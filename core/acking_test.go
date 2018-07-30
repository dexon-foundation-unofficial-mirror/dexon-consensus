// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package core

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type AckingTest struct {
	suite.Suite
}

func (s *AckingTest) SetupSuite() {

}

func (s *AckingTest) SetupTest() {

}

// genTestCase1 generates test case 1,
//  3
//  |
//  2
//  | \
//  1  |     1
//  |  |     |
//  0  0  0  0 (block height)
//  0  1  2  3 (validator)
func genTestCase1(s *AckingTest, a *Acking) []types.ValidatorID {
	// Create new Acking instance with 4 validators
	var b *types.Block
	var h common.Hash
	vids := []types.ValidatorID{}
	for i := 0; i < 4; i++ {
		vid := types.ValidatorID{Hash: common.NewRandomHash()}
		a.AddValidator(vid)
		vids = append(vids, vid)
	}
	// Add genesis blocks.
	for i := 0; i < 4; i++ {
		h = common.NewRandomHash()
		b = &types.Block{
			ProposerID: vids[i],
			ParentHash: h,
			Hash:       h,
			Height:     0,
			Acks:       map[common.Hash]struct{}{},
		}
		a.ProcessBlock(b)
	}

	// Add block 0-1 which acks 0-0.
	h = a.lattice[vids[0]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	a.ProcessBlock(b)
	s.NotNil(a.lattice[vids[0]].blocks[1])

	// Add block 0-2 which acks 0-1 and 1-0.
	h = a.lattice[vids[0]].blocks[1].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
			a.lattice[vids[1]].blocks[0].Hash: struct{}{},
		},
	}
	a.ProcessBlock(b)
	s.NotNil(a.lattice[vids[0]].blocks[2])

	// Add block 0-3 which acks 0-2.
	h = a.lattice[vids[0]].blocks[2].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Hash:       common.NewRandomHash(),
		Height:     3,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	a.ProcessBlock(b)
	s.NotNil(a.lattice[vids[0]].blocks[3])

	// Add block 3-1 which acks 3-0.
	h = a.lattice[vids[3]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[3],
		ParentHash: h,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	a.ProcessBlock(b)
	s.NotNil(a.lattice[vids[3]].blocks[0])

	return vids
}

func (s *AckingTest) TestAddValidator() {
	a := NewAcking()
	s.Equal(len(a.lattice), 0)
	genTestCase1(s, a)
	s.Equal(len(a.lattice), 4)
}

func (s *AckingTest) TestSanityCheck() {
	var b *types.Block
	var h common.Hash
	var vids []types.ValidatorID
	var err error
	a := NewAcking()
	vids = genTestCase1(s, a)

	// Non-genesis block with no ack, should get error.
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: common.NewRandomHash(),
		Height:     10,
		Acks:       make(map[common.Hash]struct{}),
	}
	err = a.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrNotAckParent.Error(), err.Error())

	// Non-genesis block which does not ack its parent.
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			a.lattice[vids[2]].blocks[0].Hash: struct{}{},
		},
	}
	err = a.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrNotAckParent.Error(), err.Error())

	// Non-genesis block which acks its parent but the height is invalid.
	h = a.lattice[vids[1]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: h,
		Height:     2,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	err = a.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrInvalidBlockHeight.Error(), err.Error())

	// Invalid proposer ID.
	h = a.lattice[vids[1]].blocks[0].Hash
	b = &types.Block{
		ProposerID: types.ValidatorID{Hash: common.NewRandomHash()},
		ParentHash: h,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	err = a.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrInvalidProposerID.Error(), err.Error())

	// Fork block.
	h = a.lattice[vids[0]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	err = a.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrForkBlock.Error(), err.Error())

	// Replicated ack.
	h = a.lattice[vids[0]].blocks[3].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Height:     4,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
			a.lattice[vids[1]].blocks[0].Hash: struct{}{},
		},
	}
	err = a.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrDoubleAck.Error(), err.Error())

	// Normal block.
	h = a.lattice[vids[1]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: h,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
			common.NewRandomHash(): struct{}{},
		},
	}
	err = a.sanityCheck(b)
	s.Nil(err)
}

func (s *AckingTest) TestAreAllAcksInLattice() {
	var b *types.Block
	var vids []types.ValidatorID
	a := NewAcking()
	vids = genTestCase1(s, a)

	// Empty ack should get true, although won't pass sanity check.
	b = &types.Block{
		Acks: map[common.Hash]struct{}{},
	}
	s.True(a.areAllAcksInLattice(b))

	// Acks blocks in lattice
	b = &types.Block{
		Acks: map[common.Hash]struct{}{
			a.lattice[vids[0]].blocks[0].Hash: struct{}{},
			a.lattice[vids[0]].blocks[1].Hash: struct{}{},
		},
	}
	s.True(a.areAllAcksInLattice(b))

	// Acks random block hash.
	b = &types.Block{
		Acks: map[common.Hash]struct{}{
			common.NewRandomHash(): struct{}{},
		},
	}
	s.False(a.areAllAcksInLattice(b))
}

func (s *AckingTest) TestStrongAck() {
	var b *types.Block
	var vids []types.ValidatorID
	a := NewAcking()
	vids = genTestCase1(s, a)

	// Check block 0-0 to 0-3 before adding 1-1 and 2-1.
	for i := uint64(0); i < 4; i++ {
		s.Equal(types.BlockStatusInit, a.lattice[vids[0]].blocks[i].Status)
	}

	// Add block 1-1 which acks 1-0 and 0-2, and block 0-0 to 0-3 are still
	// in BlockStatusInit, because they are not strongly acked.
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: a.lattice[vids[1]].blocks[0].Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			a.lattice[vids[0]].blocks[2].Hash: struct{}{},
			a.lattice[vids[1]].blocks[0].Hash: struct{}{},
		},
	}
	a.ProcessBlock(b)
	s.NotNil(a.lattice[vids[1]].blocks[1])
	for i := uint64(0); i < 4; i++ {
		s.Equal(types.BlockStatusInit, a.lattice[vids[0]].blocks[i].Status)
	}

	// Add block 2-1 which acks 0-2 and 2-0, block 0-0 to 0-2 are strongly acked but
	// 0-3 is still not.
	b = &types.Block{
		ProposerID: vids[2],
		ParentHash: a.lattice[vids[2]].blocks[0].Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			a.lattice[vids[0]].blocks[2].Hash: struct{}{},
			a.lattice[vids[2]].blocks[0].Hash: struct{}{},
		},
	}
	a.ProcessBlock(b)
	s.Equal(types.BlockStatusAcked, a.lattice[vids[0]].blocks[0].Status)
	s.Equal(types.BlockStatusAcked, a.lattice[vids[0]].blocks[1].Status)
	s.Equal(types.BlockStatusAcked, a.lattice[vids[0]].blocks[2].Status)
	s.Equal(types.BlockStatusInit, a.lattice[vids[0]].blocks[3].Status)
}

func (s *AckingTest) TestExtractBlocks() {
	var b *types.Block
	a := NewAcking()
	vids := genTestCase1(s, a)

	// Add block 1-1 which acks 1-0, 0-2, 3-0.
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: a.lattice[vids[1]].blocks[0].Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			a.lattice[vids[0]].blocks[2].Hash: struct{}{},
			a.lattice[vids[1]].blocks[0].Hash: struct{}{},
			a.lattice[vids[3]].blocks[0].Hash: struct{}{},
		},
	}
	a.ProcessBlock(b)

	// Add block 2-1 which acks 0-2, 2-0, 3-0.
	b = &types.Block{
		ProposerID: vids[2],
		ParentHash: a.lattice[vids[2]].blocks[0].Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			a.lattice[vids[0]].blocks[2].Hash: struct{}{},
			a.lattice[vids[2]].blocks[0].Hash: struct{}{},
			a.lattice[vids[3]].blocks[0].Hash: struct{}{},
		},
	}
	a.ProcessBlock(b)

	hashs := []common.Hash{
		a.lattice[vids[0]].blocks[0].Hash,
		a.lattice[vids[0]].blocks[1].Hash,
		a.lattice[vids[3]].blocks[0].Hash,
	}
	hashExtracted := map[common.Hash]*types.Block{}
	for _, b := range a.ExtractBlocks() {
		hashExtracted[b.Hash] = b
		s.Equal(types.BlockStatusOrdering, b.Status)
	}
	for _, h := range hashs {
		_, exist := hashExtracted[h]
		s.True(exist)
	}
}

func (s *AckingTest) TestRandomIntensiveAcking() {
	a := NewAcking()
	vids := []types.ValidatorID{}
	heights := map[types.ValidatorID]uint64{}
	extractedBlocks := []*types.Block{}

	// Generate validators and genesis blocks.
	for i := 0; i < 4; i++ {
		vid := types.ValidatorID{Hash: common.NewRandomHash()}
		a.AddValidator(vid)
		vids = append(vids, vid)
		h := common.NewRandomHash()
		b := &types.Block{
			Hash:       h,
			ParentHash: h,
			Acks:       map[common.Hash]struct{}{},
			Height:     0,
			ProposerID: vid,
		}
		a.ProcessBlock(b)
		heights[vid] = 1
	}

	for i := 0; i < 5000; i++ {
		vid := vids[rand.Int()%len(vids)]
		height := heights[vid]
		heights[vid]++
		parentHash := a.lattice[vid].blocks[height-1].Hash
		acks := map[common.Hash]struct{}{}
		for _, vid2 := range vids {
			if b, exist := a.lattice[vid2].blocks[a.lattice[vid].nextAck[vid2]]; exist {
				acks[b.Hash] = struct{}{}
			}
		}
		b := &types.Block{
			ProposerID: vid,
			Hash:       common.NewRandomHash(),
			ParentHash: parentHash,
			Height:     height,
			Acks:       acks,
		}
		a.ProcessBlock(b)
		extractedBlocks = append(extractedBlocks, a.ExtractBlocks()...)
	}

	extractedBlocks = append(extractedBlocks, a.ExtractBlocks()...)
	// The len of array extractedBlocks should be about 5000.
	s.True(len(extractedBlocks) > 4500)
	// The len of a.blocks should be small if deleting mechanism works.
	s.True(len(a.blocks) < 500)
}

func TestAcking(t *testing.T) {
	suite.Run(t, new(AckingTest))
}
