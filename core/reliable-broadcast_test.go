// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

// TODO(mission): we should check the return value from processBlock.

package core

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type ReliableBroadcastTest struct {
	suite.Suite
}

func (s *ReliableBroadcastTest) SetupSuite() {

}

func (s *ReliableBroadcastTest) SetupTest() {

}

func (s *ReliableBroadcastTest) prepareGenesisBlock(
	proposerID types.ValidatorID,
	validatorIDs []types.ValidatorID) (b *types.Block) {

	b = &types.Block{
		ProposerID: proposerID,
		ParentHash: common.Hash{},
		Height:     0,
		Acks:       make(map[common.Hash]struct{}),
		Timestamps: make(map[types.ValidatorID]time.Time),
	}
	for _, vID := range validatorIDs {
		b.Timestamps[vID] = time.Time{}
	}
	b.Timestamps[proposerID] = time.Now().UTC()
	var err error
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	return
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
func genTestCase1(s *ReliableBroadcastTest, r *reliableBroadcast) []types.ValidatorID {
	// Create new reliableBroadcast instance with 4 validators
	var b *types.Block
	var h common.Hash
	vids := []types.ValidatorID{}
	for i := 0; i < 4; i++ {
		vid := types.ValidatorID{Hash: common.NewRandomHash()}
		r.addValidator(vid)
		vids = append(vids, vid)
	}
	// Add genesis blocks.
	for _, vid := range vids {
		b = s.prepareGenesisBlock(vid, vids)
		s.Require().Nil(r.processBlock(b))
	}

	// Add block 0-1 which acks 0-0.
	h = r.lattice[vids[0]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	var err error
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	s.Require().Nil(r.processBlock(b))
	s.NotNil(r.lattice[vids[0]].blocks[1])

	// Add block 0-2 which acks 0-1 and 1-0.
	h = r.lattice[vids[0]].blocks[1].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Height:     2,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
			r.lattice[vids[1]].blocks[0].Hash: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	s.Require().Nil(r.processBlock(b))
	s.NotNil(r.lattice[vids[0]].blocks[2])

	// Add block 0-3 which acks 0-2.
	h = r.lattice[vids[0]].blocks[2].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Height:     3,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	s.Require().Nil(r.processBlock(b))
	s.NotNil(r.lattice[vids[0]].blocks[3])

	// Add block 3-1 which acks 3-0.
	h = r.lattice[vids[3]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[3],
		ParentHash: h,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	s.Require().Nil(r.processBlock(b))
	s.NotNil(r.lattice[vids[3]].blocks[0])

	return vids
}

func (s *ReliableBroadcastTest) TestAddValidator() {
	r := newReliableBroadcast()
	s.Equal(len(r.lattice), 0)
	genTestCase1(s, r)
	s.Equal(len(r.lattice), 4)
}

func (s *ReliableBroadcastTest) TestSanityCheck() {
	var b *types.Block
	var h common.Hash
	var vids []types.ValidatorID
	var err error
	r := newReliableBroadcast()
	vids = genTestCase1(s, r)

	// Non-genesis block with no ack, should get error.
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: common.NewRandomHash(),
		Height:     10,
		Acks:       make(map[common.Hash]struct{}),
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	err = r.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrNotAckParent.Error(), err.Error())

	// Non-genesis block which does not ack its parent.
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			r.lattice[vids[2]].blocks[0].Hash: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	err = r.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrNotAckParent.Error(), err.Error())

	// Non-genesis block which acks its parent but the height is invalid.
	h = r.lattice[vids[1]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: h,
		Height:     2,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	err = r.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrInvalidBlockHeight.Error(), err.Error())

	// Invalid proposer ID.
	h = r.lattice[vids[1]].blocks[0].Hash
	b = &types.Block{
		ProposerID: types.ValidatorID{Hash: common.NewRandomHash()},
		ParentHash: h,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	err = r.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrInvalidProposerID.Error(), err.Error())

	// Fork block.
	h = r.lattice[vids[0]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
		},
		Timestamps: map[types.ValidatorID]time.Time{
			vids[0]: time.Now().UTC(),
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	err = r.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrForkBlock.Error(), err.Error())

	// Replicated ack.
	h = r.lattice[vids[0]].blocks[3].Hash
	b = &types.Block{
		ProposerID: vids[0],
		ParentHash: h,
		Height:     4,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
			r.lattice[vids[1]].blocks[0].Hash: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	err = r.sanityCheck(b)
	s.NotNil(err)
	s.Equal(ErrDoubleAck.Error(), err.Error())

	// Normal block.
	h = r.lattice[vids[1]].blocks[0].Hash
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: h,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			h: struct{}{},
			common.NewRandomHash(): struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	err = r.sanityCheck(b)
	s.Nil(err)
}

func (s *ReliableBroadcastTest) TestAreAllAcksInLattice() {
	var b *types.Block
	var vids []types.ValidatorID
	r := newReliableBroadcast()
	vids = genTestCase1(s, r)

	// Empty ack should get true, although won't pass sanity check.
	b = &types.Block{
		Acks: map[common.Hash]struct{}{},
	}
	s.True(r.areAllAcksInLattice(b))

	// Acks blocks in lattice
	b = &types.Block{
		Acks: map[common.Hash]struct{}{
			r.lattice[vids[0]].blocks[0].Hash: struct{}{},
			r.lattice[vids[0]].blocks[1].Hash: struct{}{},
		},
	}
	s.True(r.areAllAcksInLattice(b))

	// Acks random block hash.
	b = &types.Block{
		Acks: map[common.Hash]struct{}{
			common.NewRandomHash(): struct{}{},
		},
	}
	s.False(r.areAllAcksInLattice(b))
}

func (s *ReliableBroadcastTest) TestStrongAck() {
	var b *types.Block
	var vids []types.ValidatorID
	r := newReliableBroadcast()
	vids = genTestCase1(s, r)

	// Check block 0-0 to 0-3 before adding 1-1 and 2-1.
	for i := uint64(0); i < 4; i++ {
		s.Equal(types.BlockStatusInit, r.lattice[vids[0]].blocks[i].Status)
	}

	// Add block 1-1 which acks 1-0 and 0-2, and block 0-0 to 0-3 are still
	// in BlockStatusInit, because they are not strongly acked.
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: r.lattice[vids[1]].blocks[0].Hash,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			r.lattice[vids[0]].blocks[2].Hash: struct{}{},
			r.lattice[vids[1]].blocks[0].Hash: struct{}{},
		},
	}
	var err error
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	s.Require().Nil(r.processBlock(b))
	s.NotNil(r.lattice[vids[1]].blocks[1])
	for i := uint64(0); i < 4; i++ {
		s.Equal(types.BlockStatusInit, r.lattice[vids[0]].blocks[i].Status)
	}

	// Add block 2-1 which acks 0-2 and 2-0, block 0-0 to 0-2 are strongly acked but
	// 0-3 is still not.
	b = &types.Block{
		ProposerID: vids[2],
		ParentHash: r.lattice[vids[2]].blocks[0].Hash,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			r.lattice[vids[0]].blocks[2].Hash: struct{}{},
			r.lattice[vids[2]].blocks[0].Hash: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	s.Require().Nil(r.processBlock(b))
	s.Equal(types.BlockStatusAcked, r.lattice[vids[0]].blocks[0].Status)
	s.Equal(types.BlockStatusAcked, r.lattice[vids[0]].blocks[1].Status)
	s.Equal(types.BlockStatusAcked, r.lattice[vids[0]].blocks[2].Status)
	s.Equal(types.BlockStatusInit, r.lattice[vids[0]].blocks[3].Status)
}

func (s *ReliableBroadcastTest) TestExtractBlocks() {
	var b *types.Block
	r := newReliableBroadcast()
	vids := genTestCase1(s, r)

	// Add block 1-1 which acks 1-0, 0-2, 3-0.
	b = &types.Block{
		ProposerID: vids[1],
		ParentHash: r.lattice[vids[1]].blocks[0].Hash,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			r.lattice[vids[0]].blocks[2].Hash: struct{}{},
			r.lattice[vids[1]].blocks[0].Hash: struct{}{},
			r.lattice[vids[3]].blocks[0].Hash: struct{}{},
		},
	}
	var err error
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	s.Require().Nil(r.processBlock(b))

	// Add block 2-1 which acks 0-2, 2-0, 3-0.
	b = &types.Block{
		ProposerID: vids[2],
		ParentHash: r.lattice[vids[2]].blocks[0].Hash,
		Height:     1,
		Acks: map[common.Hash]struct{}{
			r.lattice[vids[0]].blocks[2].Hash: struct{}{},
			r.lattice[vids[2]].blocks[0].Hash: struct{}{},
			r.lattice[vids[3]].blocks[0].Hash: struct{}{},
		},
	}
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
	s.Require().Nil(r.processBlock(b))

	hashs := []common.Hash{
		r.lattice[vids[0]].blocks[0].Hash,
		r.lattice[vids[0]].blocks[1].Hash,
		r.lattice[vids[3]].blocks[0].Hash,
	}
	hashExtracted := map[common.Hash]*types.Block{}
	for _, b := range r.extractBlocks() {
		hashExtracted[b.Hash] = b
		s.Equal(types.BlockStatusOrdering, b.Status)
	}
	for _, h := range hashs {
		_, exist := hashExtracted[h]
		s.True(exist)
	}
}

func (s *ReliableBroadcastTest) TestRandomIntensiveAcking() {
	r := newReliableBroadcast()
	vids := []types.ValidatorID{}
	heights := map[types.ValidatorID]uint64{}
	extractedBlocks := []*types.Block{}

	// Generate validators.
	for i := 0; i < 4; i++ {
		vid := types.ValidatorID{Hash: common.NewRandomHash()}
		r.addValidator(vid)
		vids = append(vids, vid)
	}
	// Generate genesis blocks.
	for _, vid := range vids {
		b := s.prepareGenesisBlock(vid, vids)
		s.Require().Nil(r.processBlock(b))
		heights[vid] = 1
	}

	for i := 0; i < 5000; i++ {
		vid := vids[rand.Int()%len(vids)]
		height := heights[vid]
		heights[vid]++
		parentHash := r.lattice[vid].blocks[height-1].Hash
		acks := map[common.Hash]struct{}{}
		for _, vid2 := range vids {
			if b, exist := r.lattice[vid2].blocks[r.lattice[vid].nextAck[vid2]]; exist {
				acks[b.Hash] = struct{}{}
			}
		}
		b := &types.Block{
			ProposerID: vid,
			ParentHash: parentHash,
			Height:     height,
			Acks:       acks,
		}
		var err error
		b.Hash, err = hashBlock(b)
		s.Require().Nil(err)
		s.Require().Nil(r.processBlock(b))
		extractedBlocks = append(extractedBlocks, r.extractBlocks()...)
	}

	extractedBlocks = append(extractedBlocks, r.extractBlocks()...)
	// The len of array extractedBlocks should be about 5000.
	s.True(len(extractedBlocks) > 4500)
	// The len of r.blocks should be small if deleting mechanism works.
	s.True(len(r.blocks) < 500)
}

func (s *ReliableBroadcastTest) TestRandomlyGeneratedBlocks() {
	var (
		validatorCount = 19
		blockCount     = 50
		repeat         = 20
	)

	// Prepare a randomly generated blocks.
	db, err := blockdb.NewMemBackedBlockDB("test-reliable-broadcast-random.blockdb")
	s.Require().Nil(err)
	defer func() {
		// If the test fails, keep the block database for troubleshooting.
		if s.T().Failed() {
			s.Nil(db.Close())
		}
	}()
	gen := test.NewBlocksGenerator(nil, hashBlock)
	s.Require().Nil(gen.Generate(validatorCount, blockCount, nil, db))
	iter, err := db.GetAll()
	s.Require().Nil(err)
	// Setup a revealer that would reveal blocks randomly.
	revealer, err := test.NewRandomRevealer(iter)
	s.Require().Nil(err)

	stronglyAckedHashesAsString := map[string]struct{}{}
	for i := 0; i < repeat; i++ {
		validators := map[types.ValidatorID]struct{}{}
		rb := newReliableBroadcast()
		stronglyAckedHashes := common.Hashes{}
		revealer.Reset()

		for {
			// Reveal next block.
			b, err := revealer.Next()
			if err != nil {
				if err == blockdb.ErrIterationFinished {
					err = nil
					break
				}
			}
			s.Require().Nil(err)

			// It's a hack to add validator to reliableBroadcast module.
			if _, added := validators[b.ProposerID]; !added {
				rb.addValidator(b.ProposerID)
				validators[b.ProposerID] = struct{}{}
			}
			// Perform reliable broadcast process.
			s.Require().Nil(rb.processBlock(&b))
			for _, b := range rb.extractBlocks() {
				stronglyAckedHashes = append(stronglyAckedHashes, b.Hash)
			}
		}
		// To make it easier to check, sort hashes of
		// strongly acked blocks, and concatenate them into
		// a string.
		sort.Sort(stronglyAckedHashes)
		asString := ""
		for _, h := range stronglyAckedHashes {
			asString += h.String() + ","
		}
		stronglyAckedHashesAsString[asString] = struct{}{}
	}
	// Make sure concatenated hashes of strongly acked blocks are identical.
	s.Require().Len(stronglyAckedHashesAsString, 1)
	for h := range stronglyAckedHashesAsString {
		// Make sure at least some blocks are strongly acked.
		s.True(len(h) > 0)
	}
}

func (s *ReliableBroadcastTest) TestPrepareBlock() {
	var (
		req         = s.Require()
		rb          = newReliableBroadcast()
		minInterval = 50 * time.Millisecond
		validators  []types.ValidatorID
	)
	// Prepare validator IDs.
	for i := 0; i < 4; i++ {
		vID := types.ValidatorID{Hash: common.NewRandomHash()}
		validators = append(validators, vID)
		rb.addValidator(vID)
	}
	// Setup genesis blocks.
	b00 := s.prepareGenesisBlock(validators[0], validators)
	time.Sleep(minInterval)
	b10 := s.prepareGenesisBlock(validators[1], validators)
	time.Sleep(minInterval)
	b20 := s.prepareGenesisBlock(validators[2], validators)
	time.Sleep(minInterval)
	b30 := s.prepareGenesisBlock(validators[3], validators)
	// Submit these blocks to reliableBroadcast instance.
	s.Require().Nil(rb.processBlock(b00))
	s.Require().Nil(rb.processBlock(b10))
	s.Require().Nil(rb.processBlock(b20))
	s.Require().Nil(rb.processBlock(b30))
	// We should be able to collect all 4 genesis blocks by calling
	// prepareBlock.
	b11 := &types.Block{
		ProposerID: validators[1],
	}
	rb.prepareBlock(b11)
	var err error
	b11.Hash, err = hashBlock(b11)
	s.Require().Nil(err)
	req.Contains(b11.Acks, b00.Hash)
	req.Contains(b11.Acks, b10.Hash)
	req.Contains(b11.Acks, b20.Hash)
	req.Contains(b11.Acks, b30.Hash)
	req.Equal(b11.Timestamps[validators[0]],
		b00.Timestamps[b00.ProposerID].Add(time.Millisecond))
	req.Equal(b11.Timestamps[validators[1]],
		b10.Timestamps[b10.ProposerID].Add(time.Millisecond))
	req.Equal(b11.Timestamps[validators[2]],
		b20.Timestamps[b20.ProposerID].Add(time.Millisecond))
	req.Equal(b11.Timestamps[validators[3]],
		b30.Timestamps[b30.ProposerID].Add(time.Millisecond))
	req.Equal(b11.ParentHash, b10.Hash)
	req.Equal(b11.Height, uint64(1))
	s.Require().Nil(rb.processBlock(b11))
	// Propose/Process a block based on collected info.
	b12 := &types.Block{
		ProposerID: validators[1],
	}
	rb.prepareBlock(b12)
	b12.Hash, err = hashBlock(b12)
	s.Require().Nil(err)
	// This time we only need to ack b11.
	req.Len(b12.Acks, 1)
	req.Contains(b12.Acks, b11.Hash)
	req.Equal(b12.ParentHash, b11.Hash)
	req.Equal(b12.Height, uint64(2))
	// When calling with other validator ID, we should be able to
	// get 4 blocks to ack.
	b01 := &types.Block{
		ProposerID: validators[0],
	}
	rb.prepareBlock(b01)
	b01.Hash, err = hashBlock(b01)
	s.Require().Nil(err)
	req.Len(b01.Acks, 4)
	req.Contains(b01.Acks, b00.Hash)
	req.Contains(b01.Acks, b11.Hash)
	req.Contains(b01.Acks, b20.Hash)
	req.Contains(b01.Acks, b30.Hash)
	req.Equal(b01.ParentHash, b00.Hash)
	req.Equal(b01.Height, uint64(1))
}

func TestReliableBroadcast(t *testing.T) {
	suite.Run(t, new(ReliableBroadcastTest))
}
