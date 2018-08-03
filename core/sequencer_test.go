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
	"sort"
	"strings"
	"testing"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type SequencerTestSuite struct {
	suite.Suite
}

func (s *SequencerTestSuite) generateValidatorIDs(
	count int) []types.ValidatorID {

	validatorIDs := []types.ValidatorID{}
	for i := 0; i < count; i++ {
		validatorIDs = append(validatorIDs,
			types.ValidatorID{Hash: common.NewRandomHash()})
	}

	return validatorIDs
}

func (s *SequencerTestSuite) genRootBlock(
	vID types.ValidatorID, acks map[common.Hash]struct{}) *types.Block {

	hash := common.NewRandomHash()
	return &types.Block{
		ProposerID: vID,
		ParentHash: hash,
		Hash:       hash,
		Height:     0,
		Acks:       acks,
	}
}

func (s *SequencerTestSuite) checkNotDeliver(seq *sequencer, b *types.Block) {
	hashes, eqrly, err := seq.processBlock(b)
	s.Empty(hashes)
	s.False(eqrly)
	s.Nil(err)
}

func (s *SequencerTestSuite) checkNotInWorkingSet(
	seq *sequencer, b *types.Block) {

	s.NotContains(seq.pendings, b.Hash)
	s.NotContains(seq.acked, b.Hash)
}

func (s *SequencerTestSuite) TestBlockRelation() {
	// This test case would verify if 'acking' and 'acked'
	// accumulated correctly.
	//
	// The DAG used below is:
	//  A <- B <- C

	vID := types.ValidatorID{Hash: common.NewRandomHash()}

	hash := common.NewRandomHash()
	blockA := &types.Block{
		ProposerID: vID,
		ParentHash: hash,
		Hash:       hash,
		Height:     0,
		Acks:       map[common.Hash]struct{}{},
	}
	blockB := &types.Block{
		ProposerID: vID,
		ParentHash: blockA.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			blockA.Hash: struct{}{},
		},
	}
	blockC := &types.Block{
		ProposerID: vID,
		ParentHash: blockB.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			blockB.Hash: struct{}{},
		},
	}

	seq := newSequencer(1, 3, 5)
	s.checkNotDeliver(seq, blockA)
	s.checkNotDeliver(seq, blockB)
	s.checkNotDeliver(seq, blockC)

	// Check 'acked'.
	ackedA := seq.acked[blockA.Hash]
	s.Require().NotNil(ackedA)
	s.Len(ackedA, 2)
	s.Contains(ackedA, blockB.Hash)
	s.Contains(ackedA, blockC.Hash)

	ackedB := seq.acked[blockB.Hash]
	s.Require().NotNil(ackedB)
	s.Len(ackedB, 1)
	s.Contains(ackedB, blockC.Hash)

	s.Nil(seq.acked[blockC.Hash])
}

func (s *SequencerTestSuite) TestCreateAckingHeightVectorFromHeightVector() {
	validators := s.generateValidatorIDs(5)
	global := ackingStatusVector{
		validators[0]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 5},
		validators[1]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 5},
		validators[2]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 5},
		validators[3]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 5},
	}

	// For 'not existed' record in local but exist in global,
	// should be infinity.
	ahv := ackingStatusVector{
		validators[0]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 2},
	}.getAckingHeightVector(global, 0)
	s.Len(ahv, 4)
	s.Equal(ahv[validators[0]], uint64(0))
	s.Equal(ahv[validators[1]], infinity)
	s.Equal(ahv[validators[2]], infinity)
	s.Equal(ahv[validators[3]], infinity)

	// For local min exceeds global's min+k-1, should be infinity
	hv := ackingStatusVector{
		validators[0]: &struct{ minHeight, count uint64 }{
			minHeight: 3, count: 1},
	}
	ahv = hv.getAckingHeightVector(global, 2)
	s.Equal(ahv[validators[0]], infinity)
	ahv = hv.getAckingHeightVector(global, 3)
	s.Equal(ahv[validators[0]], uint64(3))

	ahv = ackingStatusVector{
		validators[0]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 3},
		validators[1]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 3},
	}.getAckingHeightVector(global, 5)
	s.Len(ahv, 0)
}

func (s *SequencerTestSuite) TestCreateAckingNodeSetFromHeightVector() {
	validators := s.generateValidatorIDs(5)
	global := ackingStatusVector{
		validators[0]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 5},
		validators[1]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 5},
		validators[2]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 5},
		validators[3]: &struct{ minHeight, count uint64 }{
			minHeight: 0, count: 5},
	}

	local := ackingStatusVector{
		validators[0]: &struct{ minHeight, count uint64 }{
			minHeight: 1, count: 2},
	}
	s.Len(local.getAckingNodeSet(global, 1), 1)
	s.Len(local.getAckingNodeSet(global, 2), 1)
	s.Len(local.getAckingNodeSet(global, 3), 0)
}

func (s *SequencerTestSuite) TestGrade() {
	validators := s.generateValidatorIDs(5)
	seq := newSequencer(1, 3, 5) // K doesn't matter when calculating preceding.

	ans := map[types.ValidatorID]struct{}{
		validators[0]: struct{}{},
		validators[1]: struct{}{},
		validators[2]: struct{}{},
		validators[3]: struct{}{},
	}

	ahv1 := map[types.ValidatorID]uint64{
		validators[0]: 1,
		validators[1]: infinity,
		validators[2]: infinity,
		validators[3]: infinity,
	}
	ahv2 := map[types.ValidatorID]uint64{
		validators[0]: 1,
		validators[1]: 1,
		validators[2]: 1,
		validators[3]: 1,
	}
	ahv3 := map[types.ValidatorID]uint64{
		validators[0]: 1,
		validators[1]: 1,
		validators[2]: infinity,
		validators[3]: infinity,
	}
	s.Equal(seq.grade(ahv2, ahv1, ans), 1)
	s.Equal(seq.grade(ahv1, ahv2, ans), 0)
	s.Equal(seq.grade(ahv2, ahv3, ans), -1)
	s.Equal(seq.grade(ahv3, ahv2, ans), 0)
}

func (s *SequencerTestSuite) TestCycleDetection() {
	// Make sure we don't get hang by cycle from
	// block's acks.
	validators := s.generateValidatorIDs(5)

	// create blocks with cycles in acking relation.
	cycledHash := common.NewRandomHash()
	hash := common.NewRandomHash()
	b00 := &types.Block{
		ProposerID: validators[0],
		ParentHash: hash,
		Hash:       hash,
		Height:     0,
		Acks: map[common.Hash]struct{}{
			cycledHash: struct{}{},
		},
	}
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b00.Hash: struct{}{},
		},
	}
	b02 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b01.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b01.Hash: struct{}{},
		},
	}
	b03 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b02.Hash,
		Hash:       cycledHash,
		Height:     3,
		Acks: map[common.Hash]struct{}{
			b02.Hash: struct{}{},
		},
	}

	// Create a block acks self.
	hash = common.NewRandomHash()
	b10 := &types.Block{
		ProposerID: validators[1],
		ParentHash: hash,
		Hash:       hash,
		Height:     0,
		Acks: map[common.Hash]struct{}{
			hash: struct{}{},
		},
	}

	// Make sure we won't hang when cycle exists.
	seq := newSequencer(1, 3, 5)
	s.checkNotDeliver(seq, b00)
	s.checkNotDeliver(seq, b01)
	s.checkNotDeliver(seq, b02)

	// Should not hang in this line.
	s.checkNotDeliver(seq, b03)
	// Should not hang in this line
	s.checkNotDeliver(seq, b10)
}

func (s *SequencerTestSuite) TestNotValidDAGDetection() {
	validators := s.generateValidatorIDs(4)
	seq := newSequencer(1, 3, 5)

	hash := common.NewRandomHash()
	b00 := &types.Block{
		ProposerID: validators[0],
		ParentHash: hash,
		Hash:       hash,
		Height:     0,
		Acks:       map[common.Hash]struct{}{},
	}
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
	}

	// When submit to block with lower height to sequencer,
	// caller should receive an error.
	s.checkNotDeliver(seq, b01)
	_, _, err := seq.processBlock(b00)
	s.Equal(err, ErrNotValidDAG)
}

func (s *SequencerTestSuite) TestEarlyDeliver() {
	// The test scenario:
	//
	//  o o o o o
	//  : : : : : <- (K - 1) layers
	//  o o o o o
	//   \ v /  |
	//     o    o
	//     A    B
	//  Even when B is not received, A should
	//  be able to be delivered.
	seq := newSequencer(2, 3, 5)
	validators := s.generateValidatorIDs(5)

	genNextBlock := func(b *types.Block) *types.Block {
		return &types.Block{
			ProposerID: b.ProposerID,
			ParentHash: b.Hash,
			Hash:       common.NewRandomHash(),
			Height:     b.Height + 1,
			Acks: map[common.Hash]struct{}{
				b.Hash: struct{}{},
			},
		}
	}

	b00 := s.genRootBlock(validators[0], map[common.Hash]struct{}{})
	b01 := genNextBlock(b00)
	b02 := genNextBlock(b01)

	b10 := s.genRootBlock(validators[1], map[common.Hash]struct{}{
		b00.Hash: struct{}{},
	})
	b11 := genNextBlock(b10)
	b12 := genNextBlock(b11)

	b20 := s.genRootBlock(validators[2], map[common.Hash]struct{}{
		b00.Hash: struct{}{},
	})
	b21 := genNextBlock(b20)
	b22 := genNextBlock(b21)

	b30 := s.genRootBlock(validators[3], map[common.Hash]struct{}{
		b00.Hash: struct{}{},
	})
	b31 := genNextBlock(b30)
	b32 := genNextBlock(b31)

	// It's a valid block sequence to deliver
	// to total ordering algorithm: DAG.
	s.checkNotDeliver(seq, b00)
	s.checkNotDeliver(seq, b01)
	s.checkNotDeliver(seq, b02)

	vec := seq.candidateAckingStatusVectors[b00.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 1)
	s.Equal(vec[validators[0]].minHeight, b00.Height)
	s.Equal(vec[validators[0]].count, uint64(3))

	s.checkNotDeliver(seq, b10)
	s.checkNotDeliver(seq, b11)
	s.checkNotDeliver(seq, b12)
	s.checkNotDeliver(seq, b20)
	s.checkNotDeliver(seq, b21)
	s.checkNotDeliver(seq, b22)
	s.checkNotDeliver(seq, b30)
	s.checkNotDeliver(seq, b31)

	// Check the internal state before delivering.
	s.Len(seq.candidateAckingStatusVectors, 1) // b00 is the only candidate.

	vec = seq.candidateAckingStatusVectors[b00.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 4)
	s.Equal(vec[validators[0]].minHeight, b00.Height)
	s.Equal(vec[validators[0]].count, uint64(3))
	s.Equal(vec[validators[1]].minHeight, b10.Height)
	s.Equal(vec[validators[1]].count, uint64(3))
	s.Equal(vec[validators[2]].minHeight, b20.Height)
	s.Equal(vec[validators[2]].count, uint64(3))
	s.Equal(vec[validators[3]].minHeight, b30.Height)
	s.Equal(vec[validators[3]].count, uint64(2))

	hashes, early, err := seq.processBlock(b32)
	s.Require().Len(hashes, 1)
	s.True(early)
	s.Nil(err)
	s.Equal(hashes[0], b00.Hash)

	// Check the internal state after delivered.
	s.Len(seq.candidateAckingStatusVectors, 4) // b01, b10, b20, b30 are candidates.

	// Check b01.
	vec = seq.candidateAckingStatusVectors[b01.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 1)
	s.Equal(vec[validators[0]].minHeight, b01.Height)
	s.Equal(vec[validators[0]].count, uint64(2))

	// Check b10.
	vec = seq.candidateAckingStatusVectors[b10.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 1)
	s.Equal(vec[validators[1]].minHeight, b10.Height)
	s.Equal(vec[validators[1]].count, uint64(3))

	// Check b20.
	vec = seq.candidateAckingStatusVectors[b20.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 1)
	s.Equal(vec[validators[2]].minHeight, b20.Height)
	s.Equal(vec[validators[2]].count, uint64(3))

	// Check b30.
	vec = seq.candidateAckingStatusVectors[b30.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 1)
	s.Equal(vec[validators[3]].minHeight, b30.Height)
	s.Equal(vec[validators[3]].count, uint64(3))

	// Make sure b00 doesn't exist in current working set:
	s.checkNotInWorkingSet(seq, b00)
}

func (s *SequencerTestSuite) TestBasicCaseForK2() {
	// It's a handcrafted test case.
	seq := newSequencer(2, 3, 5)
	validators := s.generateValidatorIDs(5)

	b00 := s.genRootBlock(validators[0], map[common.Hash]struct{}{})
	b10 := s.genRootBlock(validators[1], map[common.Hash]struct{}{})
	b20 := s.genRootBlock(
		validators[2], map[common.Hash]struct{}{b10.Hash: struct{}{}})
	b30 := s.genRootBlock(
		validators[3], map[common.Hash]struct{}{b20.Hash: struct{}{}})
	b40 := s.genRootBlock(validators[4], map[common.Hash]struct{}{})
	b11 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b10.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b10.Hash: struct{}{},
			b00.Hash: struct{}{},
		},
	}
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b00.Hash: struct{}{},
			b11.Hash: struct{}{},
		},
	}
	b21 := &types.Block{
		ProposerID: validators[2],
		ParentHash: b20.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b20.Hash: struct{}{},
			b01.Hash: struct{}{},
		},
	}
	b31 := &types.Block{
		ProposerID: validators[3],
		ParentHash: b30.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b30.Hash: struct{}{},
			b21.Hash: struct{}{},
		},
	}
	b02 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b01.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b01.Hash: struct{}{},
			b21.Hash: struct{}{},
		},
	}
	b12 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b11.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b11.Hash: struct{}{},
			b21.Hash: struct{}{},
		},
	}
	b32 := &types.Block{
		ProposerID: validators[3],
		ParentHash: b31.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b31.Hash: struct{}{},
		},
	}
	b22 := &types.Block{
		ProposerID: validators[2],
		ParentHash: b21.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b21.Hash: struct{}{},
			b32.Hash: struct{}{},
		},
	}
	b23 := &types.Block{
		ProposerID: validators[2],
		ParentHash: b22.Hash,
		Hash:       common.NewRandomHash(),
		Height:     3,
		Acks: map[common.Hash]struct{}{
			b22.Hash: struct{}{},
		},
	}
	b03 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b02.Hash,
		Hash:       common.NewRandomHash(),
		Height:     3,
		Acks: map[common.Hash]struct{}{
			b02.Hash: struct{}{},
			b22.Hash: struct{}{},
		},
	}
	b13 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b12.Hash,
		Hash:       common.NewRandomHash(),
		Height:     3,
		Acks: map[common.Hash]struct{}{
			b12.Hash: struct{}{},
			b22.Hash: struct{}{},
		},
	}
	b14 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b13.Hash,
		Hash:       common.NewRandomHash(),
		Height:     4,
		Acks: map[common.Hash]struct{}{
			b13.Hash: struct{}{},
		},
	}
	b41 := &types.Block{
		ProposerID: validators[4],
		ParentHash: b40.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b40.Hash: struct{}{},
		},
	}
	b42 := &types.Block{
		ProposerID: validators[4],
		ParentHash: b41.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b41.Hash: struct{}{},
		},
	}

	s.checkNotDeliver(seq, b00)
	s.checkNotDeliver(seq, b10)
	s.checkNotDeliver(seq, b11)
	s.checkNotDeliver(seq, b01)
	s.checkNotDeliver(seq, b20)
	s.checkNotDeliver(seq, b30)
	s.checkNotDeliver(seq, b21)
	s.checkNotDeliver(seq, b31)
	s.checkNotDeliver(seq, b32)
	s.checkNotDeliver(seq, b22)
	s.checkNotDeliver(seq, b12)

	// Make sure 'acked' for current precedings is correct.
	acked := seq.acked[b00.Hash]
	s.Require().NotNil(acked)
	s.Len(acked, 7)
	s.Contains(acked, b01.Hash)
	s.Contains(acked, b11.Hash)
	s.Contains(acked, b12.Hash)
	s.Contains(acked, b21.Hash)
	s.Contains(acked, b22.Hash)
	s.Contains(acked, b31.Hash)
	s.Contains(acked, b32.Hash)

	acked = seq.acked[b10.Hash]
	s.Require().NotNil(acked)
	s.Len(acked, 9)
	s.Contains(acked, b01.Hash)
	s.Contains(acked, b11.Hash)
	s.Contains(acked, b12.Hash)
	s.Contains(acked, b20.Hash)
	s.Contains(acked, b21.Hash)
	s.Contains(acked, b22.Hash)
	s.Contains(acked, b30.Hash)
	s.Contains(acked, b31.Hash)
	s.Contains(acked, b32.Hash)

	// Make sure there are 2 candidates.
	s.Require().Len(seq.candidateAckingStatusVectors, 2)

	// Check b00's height vector.
	vec := seq.candidateAckingStatusVectors[b00.Hash]
	s.Require().NotNil(vec)
	s.NotContains(vec, validators[4])
	s.Equal(vec[validators[0]].minHeight, b00.Height)
	s.Equal(vec[validators[0]].count, uint64(2))
	s.Equal(vec[validators[1]].minHeight, b11.Height)
	s.Equal(vec[validators[1]].count, uint64(2))
	s.Equal(vec[validators[2]].minHeight, b21.Height)
	s.Equal(vec[validators[2]].count, uint64(2))
	s.Equal(vec[validators[3]].minHeight, b31.Height)
	s.Equal(vec[validators[3]].count, uint64(2))

	// Check b10's height vector.
	vec = seq.candidateAckingStatusVectors[b10.Hash]
	s.Require().NotNil(vec)
	s.NotContains(vec, validators[4])
	s.Equal(vec[validators[0]].minHeight, b01.Height)
	s.Equal(vec[validators[0]].count, uint64(1))
	s.Equal(vec[validators[1]].minHeight, b10.Height)
	s.Equal(vec[validators[1]].count, uint64(3))
	s.Equal(vec[validators[2]].minHeight, b20.Height)
	s.Equal(vec[validators[2]].count, uint64(3))
	s.Equal(vec[validators[3]].minHeight, b30.Height)
	s.Equal(vec[validators[3]].count, uint64(3))

	// Check the first deliver.
	hashes, early, err := seq.processBlock(b02)
	s.True(early)
	s.Nil(err)
	expected := common.Hashes{b00.Hash, b10.Hash}
	sort.Sort(expected)
	s.Equal(hashes, expected)

	// Make sure b00, b10 are removed from current working set.
	s.checkNotInWorkingSet(seq, b00)
	s.checkNotInWorkingSet(seq, b10)

	// Check if candidates of next round are picked correctly.
	s.Len(seq.candidateAckingStatusVectors, 2)

	// Check b01's height vector.
	vec = seq.candidateAckingStatusVectors[b11.Hash]
	s.Require().NotNil(vec)
	s.NotContains(vec, validators[4])
	s.Equal(vec[validators[0]].minHeight, b01.Height)
	s.Equal(vec[validators[0]].count, uint64(2))
	s.Equal(vec[validators[1]].minHeight, b11.Height)
	s.Equal(vec[validators[1]].count, uint64(2))
	s.Equal(vec[validators[2]].minHeight, b21.Height)
	s.Equal(vec[validators[2]].count, uint64(2))
	s.Equal(vec[validators[3]].minHeight, b11.Height)
	s.Equal(vec[validators[3]].count, uint64(2))

	// Check b20's height vector.
	vec = seq.candidateAckingStatusVectors[b20.Hash]
	s.Require().NotNil(vec)
	s.NotContains(vec, validators[4])
	s.Equal(vec[validators[0]].minHeight, b02.Height)
	s.Equal(vec[validators[0]].count, uint64(1))
	s.Equal(vec[validators[1]].minHeight, b12.Height)
	s.Equal(vec[validators[1]].count, uint64(1))
	s.Equal(vec[validators[2]].minHeight, b20.Height)
	s.Equal(vec[validators[2]].count, uint64(3))
	s.Equal(vec[validators[3]].minHeight, b30.Height)
	s.Equal(vec[validators[3]].count, uint64(3))

	s.checkNotDeliver(seq, b13)

	// Check the second deliver.
	hashes, early, err = seq.processBlock(b03)
	s.True(early)
	s.Nil(err)
	expected = common.Hashes{b11.Hash, b20.Hash}
	sort.Sort(expected)
	s.Equal(hashes, expected)

	// Make sure b11, b20 are removed from current working set.
	s.checkNotInWorkingSet(seq, b11)
	s.checkNotInWorkingSet(seq, b20)

	// Add b40, b41, b42 to pending set.
	s.checkNotDeliver(seq, b40)
	s.checkNotDeliver(seq, b41)
	s.checkNotDeliver(seq, b42)
	s.checkNotDeliver(seq, b14)

	// Make sure b01, b30, b40 are candidate in next round.
	s.Len(seq.candidateAckingStatusVectors, 3)
	vec = seq.candidateAckingStatusVectors[b01.Hash]
	s.Require().NotNil(vec)
	s.NotContains(vec, validators[4])
	s.Equal(vec[validators[0]].minHeight, b01.Height)
	s.Equal(vec[validators[0]].count, uint64(3))
	s.Equal(vec[validators[1]].minHeight, b12.Height)
	s.Equal(vec[validators[1]].count, uint64(3))
	s.Equal(vec[validators[2]].minHeight, b21.Height)
	s.Equal(vec[validators[2]].count, uint64(2))
	s.Equal(vec[validators[3]].minHeight, b31.Height)
	s.Equal(vec[validators[3]].count, uint64(2))

	vec = seq.candidateAckingStatusVectors[b30.Hash]
	s.Require().NotNil(vec)
	s.NotContains(vec, validators[4])
	s.Equal(vec[validators[0]].minHeight, b03.Height)
	s.Equal(vec[validators[0]].count, uint64(1))
	s.Equal(vec[validators[1]].minHeight, b13.Height)
	s.Equal(vec[validators[1]].count, uint64(2))
	s.Equal(vec[validators[2]].minHeight, b22.Height)
	s.Equal(vec[validators[2]].count, uint64(1))
	s.Equal(vec[validators[3]].minHeight, b30.Height)
	s.Equal(vec[validators[3]].count, uint64(3))

	vec = seq.candidateAckingStatusVectors[b40.Hash]
	s.Require().NotNil(vec)
	s.NotContains(vec, validators[0])
	s.NotContains(vec, validators[1])
	s.NotContains(vec, validators[2])
	s.NotContains(vec, validators[3])
	s.Equal(vec[validators[4]].minHeight, b40.Height)
	s.Equal(vec[validators[4]].count, uint64(3))

	// Make 'Acking Node Set' contains blocks from all validators,
	// this should trigger not-early deliver.
	hashes, early, err = seq.processBlock(b23)
	s.False(early)
	s.Nil(err)
	expected = common.Hashes{b01.Hash, b30.Hash}
	sort.Sort(expected)
	s.Equal(expected, hashes)

	// Make sure b01, b30 not in working set
	s.checkNotInWorkingSet(seq, b01)
	s.checkNotInWorkingSet(seq, b30)

	// Make sure b21, b40 are candidates of next round.
	s.Contains(seq.candidateAckingStatusVectors, b21.Hash)
	s.Contains(seq.candidateAckingStatusVectors, b40.Hash)
}

func (s *SequencerTestSuite) TestBasicCaseForK0() {
	// This is a relatively simple test for K=0.
	//
	//  0   1   2    3    4
	//  -------------------
	//  .   .   .    .    .
	//  .   .   .    .    .
	//  o   o   o <- o <- o   Height: 1
	//  | \ | \ |    |
	//  v   v   v    v
	//  o   o   o <- o        Height: 0
	seq := newSequencer(0, 3, 5)
	validators := s.generateValidatorIDs(5)

	b00 := s.genRootBlock(validators[0], map[common.Hash]struct{}{})
	b10 := s.genRootBlock(validators[1], map[common.Hash]struct{}{})
	b20 := s.genRootBlock(validators[2], map[common.Hash]struct{}{})
	b30 := s.genRootBlock(validators[3], map[common.Hash]struct{}{
		b20.Hash: struct{}{},
	})
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b00.Hash: struct{}{},
			b10.Hash: struct{}{},
		},
	}
	b11 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b10.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b10.Hash: struct{}{},
			b20.Hash: struct{}{},
		},
	}
	b21 := &types.Block{
		ProposerID: validators[2],
		ParentHash: b20.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b20.Hash: struct{}{},
		},
	}
	b31 := &types.Block{
		ProposerID: validators[3],
		ParentHash: b30.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b21.Hash: struct{}{},
			b30.Hash: struct{}{},
		},
	}
	b40 := s.genRootBlock(validators[4], map[common.Hash]struct{}{
		b31.Hash: struct{}{},
	})

	s.checkNotDeliver(seq, b00)
	s.checkNotDeliver(seq, b10)
	s.checkNotDeliver(seq, b20)
	s.checkNotDeliver(seq, b30)
	s.checkNotDeliver(seq, b01)
	s.checkNotDeliver(seq, b11)
	s.checkNotDeliver(seq, b21)
	s.checkNotDeliver(seq, b31)

	// Check status before delivering.
	vec := seq.candidateAckingStatusVectors[b00.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 1)
	s.Equal(vec[validators[0]].minHeight, b00.Height)
	s.Equal(vec[validators[0]].count, uint64(2))

	vec = seq.candidateAckingStatusVectors[b10.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 2)
	s.Equal(vec[validators[0]].minHeight, b01.Height)
	s.Equal(vec[validators[0]].count, uint64(1))
	s.Equal(vec[validators[1]].minHeight, b10.Height)
	s.Equal(vec[validators[1]].count, uint64(2))

	vec = seq.candidateAckingStatusVectors[b20.Hash]
	s.Require().NotNil(vec)
	s.Len(vec, 3)
	s.Equal(vec[validators[1]].minHeight, b11.Height)
	s.Equal(vec[validators[1]].count, uint64(1))
	s.Equal(vec[validators[2]].minHeight, b20.Height)
	s.Equal(vec[validators[2]].count, uint64(2))
	s.Equal(vec[validators[3]].minHeight, b30.Height)
	s.Equal(vec[validators[3]].count, uint64(2))

	// This new block should trigger non-early deliver.
	hashes, early, err := seq.processBlock(b40)
	s.False(early)
	s.Nil(err)
	expected := common.Hashes{b20.Hash}
	sort.Sort(expected)
	s.Equal(expected, hashes)

	// Make sure b20 is no long existing in working set.
	s.checkNotInWorkingSet(seq, b20)

	// Make sure b10, b30 are candidates for next round.
	s.Contains(seq.candidateAckingStatusVectors, b10.Hash)
	s.Contains(seq.candidateAckingStatusVectors, b30.Hash)
}

func (s *SequencerTestSuite) baseTestRandomlyGeneratedBlocks(
	seqConstructor func() *sequencer,
	revealer test.Revealer,
	repeat int) {

	// TODO (mission): make this part run concurrently.
	revealingSequence := map[string]struct{}{}
	orderingSequence := map[string]struct{}{}
	for i := 0; i < repeat; i++ {
		revealed := ""
		ordered := ""
		revealer.Reset()
		seq := seqConstructor()
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
			revealed += b.Hash.String() + ","

			// Perform total ordering.
			hashes, _, err := seq.processBlock(&b)
			s.Require().Nil(err)
			for _, h := range hashes {
				ordered += h.String() + ","
			}
		}
		revealingSequence[revealed] = struct{}{}
		orderingSequence[ordered] = struct{}{}
	}

	// Make sure we test at least two different
	// revealing sequence.
	s.True(len(revealingSequence) > 1)
	// Make sure all ordering are equal or prefixed
	// to another one.
	for orderFrom := range orderingSequence {
		for orderTo := range orderingSequence {
			if orderFrom == orderTo {
				continue
			}
			ok := strings.HasPrefix(orderFrom, orderTo) ||
				strings.HasPrefix(orderTo, orderFrom)
			s.True(ok)
		}
	}
}

func (s *SequencerTestSuite) TestRandomlyGeneratedBlocks() {
	var (
		validatorCount        = 19
		blockCount            = 50
		phi            uint64 = 10
		repeat                = 10
	)

	// Prepare a randomly genearated blocks.
	db, err := blockdb.NewMemBackedBlockDB("test-sequencer-random.blockdb")
	s.Require().Nil(err)
	defer func() {
		// If the test fails, keep the block database for troubleshooting.
		if s.T().Failed() {
			s.Nil(db.Close())
		}
	}()

	gen := test.NewBlocksGenerator(nil)
	s.Require().Nil(gen.Generate(validatorCount, blockCount, nil, db))
	iter, err := db.GetAll()
	s.Require().Nil(err)
	// Setup a revealer that would reveal blocks forming
	// valid DAGs.
	revealer, err := test.NewRandomDAGRevealer(iter)
	s.Require().Nil(err)

	// Test for K=0.
	constructor := func() *sequencer {
		return newSequencer(0, phi, uint64(validatorCount))
	}
	s.baseTestRandomlyGeneratedBlocks(constructor, revealer, repeat)
	// Test for K=1,
	constructor = func() *sequencer {
		return newSequencer(1, phi, uint64(validatorCount))
	}
	s.baseTestRandomlyGeneratedBlocks(constructor, revealer, repeat)
	// Test for K=2,
	constructor = func() *sequencer {
		return newSequencer(2, phi, uint64(validatorCount))
	}
	s.baseTestRandomlyGeneratedBlocks(constructor, revealer, repeat)
	// Test for K=3,
	constructor = func() *sequencer {
		return newSequencer(2, phi, uint64(validatorCount))
	}
	s.baseTestRandomlyGeneratedBlocks(constructor, revealer, repeat)
}

func TestSequencer(t *testing.T) {
	suite.Run(t, new(SequencerTestSuite))
}
