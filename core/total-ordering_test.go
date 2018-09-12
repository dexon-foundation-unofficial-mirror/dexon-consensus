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

type TotalOrderingTestSuite struct {
	suite.Suite
}

func (s *TotalOrderingTestSuite) genGenesisBlock(
	vIDs types.ValidatorIDs,
	chainID uint32,
	acks common.Hashes) *types.Block {

	return &types.Block{
		ProposerID: vIDs[chainID],
		ParentHash: common.Hash{},
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  0,
			ChainID: chainID,
		},
		Acks: common.NewSortedHashes(acks),
	}
}

func (s *TotalOrderingTestSuite) checkNotDeliver(to *totalOrdering, b *types.Block) {
	blocks, eqrly, err := to.processBlock(b)
	s.Empty(blocks)
	s.False(eqrly)
	s.Nil(err)
}

func (s *TotalOrderingTestSuite) checkHashSequence(blocks []*types.Block, hashes common.Hashes) {
	sort.Sort(hashes)
	for i, h := range hashes {
		s.Equal(blocks[i].Hash, h)
	}
}

func (s *TotalOrderingTestSuite) checkNotInWorkingSet(
	to *totalOrdering, b *types.Block) {

	s.NotContains(to.pendings, b.Hash)
	s.NotContains(to.acked, b.Hash)
}

func (s *TotalOrderingTestSuite) TestBlockRelation() {
	// This test case would verify if 'acking' and 'acked'
	// accumulated correctly.
	//
	// The DAG used below is:
	//  A <- B <- C
	validators := test.GenerateRandomValidatorIDs(5)
	vID := validators[0]
	blockA := s.genGenesisBlock(validators, 0, common.Hashes{})
	blockB := &types.Block{
		ProposerID: vID,
		ParentHash: blockA.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{blockA.Hash}),
	}
	blockC := &types.Block{
		ProposerID: vID,
		ParentHash: blockB.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  2,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{blockB.Hash}),
	}

	to := newTotalOrdering(1, 3, uint32(len(validators)))
	s.checkNotDeliver(to, blockA)
	s.checkNotDeliver(to, blockB)
	s.checkNotDeliver(to, blockC)

	// Check 'acked'.
	ackedA := to.acked[blockA.Hash]
	s.Require().NotNil(ackedA)
	s.Len(ackedA, 2)
	s.Contains(ackedA, blockB.Hash)
	s.Contains(ackedA, blockC.Hash)

	ackedB := to.acked[blockB.Hash]
	s.Require().NotNil(ackedB)
	s.Len(ackedB, 1)
	s.Contains(ackedB, blockC.Hash)

	s.Nil(to.acked[blockC.Hash])
}

func (s *TotalOrderingTestSuite) TestCreateAckingHeightVectorFromHeightVector() {
	var (
		cache   = newTotalOrderingObjectCache(5)
		dirties = []int{0, 1, 2, 3, 4}
	)
	// Prepare global acking status.
	global := &totalOrderingCandidateInfo{
		ackedStatus: []*totalOrderingHeightRecord{
			&totalOrderingHeightRecord{minHeight: 0, count: 5},
			&totalOrderingHeightRecord{minHeight: 0, count: 5},
			&totalOrderingHeightRecord{minHeight: 0, count: 5},
			&totalOrderingHeightRecord{minHeight: 0, count: 5},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
		}}

	// For 'not existed' record in local but exist in global,
	// should be infinity.
	candidate := &totalOrderingCandidateInfo{
		ackedStatus: []*totalOrderingHeightRecord{
			&totalOrderingHeightRecord{minHeight: 0, count: 2},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
		}}
	candidate.updateAckingHeightVector(global, 0, dirties, cache)
	s.Equal(candidate.cachedHeightVector[0], uint64(0))
	s.Equal(candidate.cachedHeightVector[1], infinity)
	s.Equal(candidate.cachedHeightVector[2], infinity)
	s.Equal(candidate.cachedHeightVector[3], infinity)

	// For local min exceeds global's min+k-1, should be infinity
	candidate = &totalOrderingCandidateInfo{
		ackedStatus: []*totalOrderingHeightRecord{
			&totalOrderingHeightRecord{minHeight: 3, count: 1},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
		}}
	candidate.updateAckingHeightVector(global, 2, dirties, cache)
	s.Equal(candidate.cachedHeightVector[0], infinity)
	candidate.updateAckingHeightVector(global, 3, dirties, cache)
	s.Equal(candidate.cachedHeightVector[0], uint64(3))

	candidate = &totalOrderingCandidateInfo{
		ackedStatus: []*totalOrderingHeightRecord{
			&totalOrderingHeightRecord{minHeight: 0, count: 3},
			&totalOrderingHeightRecord{minHeight: 0, count: 3},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
		}}
	candidate.updateAckingHeightVector(global, 5, dirties, cache)
}

func (s *TotalOrderingTestSuite) TestCreateAckingNodeSetFromHeightVector() {
	global := &totalOrderingCandidateInfo{
		ackedStatus: []*totalOrderingHeightRecord{
			&totalOrderingHeightRecord{minHeight: 0, count: 5},
			&totalOrderingHeightRecord{minHeight: 0, count: 5},
			&totalOrderingHeightRecord{minHeight: 0, count: 5},
			&totalOrderingHeightRecord{minHeight: 0, count: 5},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
		}}

	local := &totalOrderingCandidateInfo{
		ackedStatus: []*totalOrderingHeightRecord{
			&totalOrderingHeightRecord{minHeight: 1, count: 2},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
			&totalOrderingHeightRecord{minHeight: 0, count: 0},
		}}
	s.Equal(local.getAckingNodeSetLength(global, 1), uint64(1))
	s.Equal(local.getAckingNodeSetLength(global, 2), uint64(1))
	s.Equal(local.getAckingNodeSetLength(global, 3), uint64(0))
}

func (s *TotalOrderingTestSuite) TestGrade() {
	// This test case just fake some internal structure used
	// when performing total ordering.
	var (
		validators      = test.GenerateRandomValidatorIDs(5)
		cache           = newTotalOrderingObjectCache(5)
		dirtyValidators = []int{0, 1, 2, 3, 4}
	)
	ansLength := uint64(len(map[types.ValidatorID]struct{}{
		validators[0]: struct{}{},
		validators[1]: struct{}{},
		validators[2]: struct{}{},
		validators[3]: struct{}{},
	}))
	candidate1 := newTotalOrderingCandidateInfo(common.Hash{}, cache)
	candidate1.cachedHeightVector = []uint64{
		1, infinity, infinity, infinity, infinity}
	candidate2 := newTotalOrderingCandidateInfo(common.Hash{}, cache)
	candidate2.cachedHeightVector = []uint64{
		1, 1, 1, 1, infinity}
	candidate3 := newTotalOrderingCandidateInfo(common.Hash{}, cache)
	candidate3.cachedHeightVector = []uint64{
		1, 1, infinity, infinity, infinity}

	candidate2.updateWinRecord(
		0, candidate1, dirtyValidators, cache)
	s.Equal(candidate2.winRecords[0].grade(5, 3, ansLength), 1)
	candidate1.updateWinRecord(
		1, candidate2, dirtyValidators, cache)
	s.Equal(candidate1.winRecords[1].grade(5, 3, ansLength), 0)
	candidate2.updateWinRecord(
		2, candidate3, dirtyValidators, cache)
	s.Equal(candidate2.winRecords[2].grade(5, 3, ansLength), -1)
	candidate3.updateWinRecord(
		1, candidate2, dirtyValidators, cache)
	s.Equal(candidate3.winRecords[1].grade(5, 3, ansLength), 0)
}

func (s *TotalOrderingTestSuite) TestCycleDetection() {
	// Make sure we don't get hang by cycle from
	// block's acks.
	validators := test.GenerateRandomValidatorIDs(5)

	// create blocks with cycles in acking relation.
	cycledHash := common.NewRandomHash()
	b00 := s.genGenesisBlock(validators, 0, common.Hashes{cycledHash})
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{b00.Hash}),
	}
	b02 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b01.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  2,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{b01.Hash}),
	}
	b03 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b02.Hash,
		Hash:       cycledHash,
		Position: types.Position{
			Height:  3,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{b02.Hash}),
	}

	// Create a block acks self.
	b10 := s.genGenesisBlock(validators, 1, common.Hashes{})
	b10.Acks = append(b10.Acks, b10.Hash)

	// Make sure we won't hang when cycle exists.
	to := newTotalOrdering(1, 3, uint32(len(validators)))
	s.checkNotDeliver(to, b00)
	s.checkNotDeliver(to, b01)
	s.checkNotDeliver(to, b02)

	// Should not hang in this line.
	s.checkNotDeliver(to, b03)
	// Should not hang in this line
	s.checkNotDeliver(to, b10)
}

func (s *TotalOrderingTestSuite) TestNotValidDAGDetection() {
	validators := test.GenerateRandomValidatorIDs(5)
	to := newTotalOrdering(1, 3, uint32(len(validators)))

	b00 := s.genGenesisBlock(validators, 0, common.Hashes{})
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Position: types.Position{
			Height:  1,
			ChainID: 0,
		},
		Hash: common.NewRandomHash(),
	}

	// When submit to block with lower height to totalOrdering,
	// caller should receive an error.
	s.checkNotDeliver(to, b01)
	_, _, err := to.processBlock(b00)
	s.Equal(err, ErrNotValidDAG)
}

func (s *TotalOrderingTestSuite) TestEarlyDeliver() {
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
	validators := test.GenerateRandomValidatorIDs(5)
	to := newTotalOrdering(2, 3, uint32(len(validators)))
	genNextBlock := func(b *types.Block) *types.Block {
		return &types.Block{
			ProposerID: b.ProposerID,
			ParentHash: b.Hash,
			Hash:       common.NewRandomHash(),
			Position: types.Position{
				Height:  b.Position.Height + 1,
				ChainID: b.Position.ChainID,
			},
			Acks: common.NewSortedHashes(common.Hashes{b.Hash}),
		}
	}

	b00 := s.genGenesisBlock(validators, 0, common.Hashes{})
	b01 := genNextBlock(b00)
	b02 := genNextBlock(b01)

	b10 := s.genGenesisBlock(validators, 1, common.Hashes{b00.Hash})
	b11 := genNextBlock(b10)
	b12 := genNextBlock(b11)

	b20 := s.genGenesisBlock(validators, 2, common.Hashes{b00.Hash})
	b21 := genNextBlock(b20)
	b22 := genNextBlock(b21)

	b30 := s.genGenesisBlock(validators, 3, common.Hashes{b00.Hash})
	b31 := genNextBlock(b30)
	b32 := genNextBlock(b31)

	// It's a valid block sequence to deliver
	// to total ordering algorithm: DAG.
	s.checkNotDeliver(to, b00)
	s.checkNotDeliver(to, b01)
	s.checkNotDeliver(to, b02)

	candidate := to.candidates[0]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight,
		b00.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(3))

	s.checkNotDeliver(to, b10)
	s.checkNotDeliver(to, b11)
	s.checkNotDeliver(to, b12)
	s.checkNotDeliver(to, b20)
	s.checkNotDeliver(to, b21)
	s.checkNotDeliver(to, b22)
	s.checkNotDeliver(to, b30)
	s.checkNotDeliver(to, b31)

	// Check the internal state before delivering.
	s.Len(to.candidateChainMapping, 1) // b00 is the only candidate.

	candidate = to.candidates[0]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight, b00.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(3))
	s.Equal(candidate.ackedStatus[1].minHeight, b10.Position.Height)
	s.Equal(candidate.ackedStatus[1].count, uint64(3))
	s.Equal(candidate.ackedStatus[2].minHeight, b20.Position.Height)
	s.Equal(candidate.ackedStatus[2].count, uint64(3))
	s.Equal(candidate.ackedStatus[3].minHeight, b30.Position.Height)
	s.Equal(candidate.ackedStatus[3].count, uint64(2))

	blocks, early, err := to.processBlock(b32)
	s.Require().Len(blocks, 1)
	s.True(early)
	s.Nil(err)
	s.checkHashSequence(blocks, common.Hashes{b00.Hash})

	// Check the internal state after delivered.
	s.Len(to.candidateChainMapping, 4) // b01, b10, b20, b30 are candidates.

	// Check b01.
	candidate = to.candidates[0]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight, b01.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(2))

	// Check b10.
	candidate = to.candidates[1]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[1].minHeight, b10.Position.Height)
	s.Equal(candidate.ackedStatus[1].count, uint64(3))

	// Check b20.
	candidate = to.candidates[2]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[2].minHeight, b20.Position.Height)
	s.Equal(candidate.ackedStatus[2].count, uint64(3))

	// Check b30.
	candidate = to.candidates[3]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[3].minHeight, b30.Position.Height)
	s.Equal(candidate.ackedStatus[3].count, uint64(3))

	// Make sure b00 doesn't exist in current working set:
	s.checkNotInWorkingSet(to, b00)
}

func (s *TotalOrderingTestSuite) TestBasicCaseForK2() {
	// It's a handcrafted test case.
	validators := test.GenerateRandomValidatorIDs(5)
	to := newTotalOrdering(2, 3, uint32(len(validators)))
	// Setup blocks.
	b00 := s.genGenesisBlock(validators, 0, common.Hashes{})
	b10 := s.genGenesisBlock(validators, 1, common.Hashes{})
	b20 := s.genGenesisBlock(validators, 2, common.Hashes{b10.Hash})
	b30 := s.genGenesisBlock(validators, 3, common.Hashes{b20.Hash})
	b40 := s.genGenesisBlock(validators, 4, common.Hashes{})
	b11 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b10.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 1,
		},
		Acks: common.NewSortedHashes(common.Hashes{b10.Hash, b00.Hash}),
	}
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{b00.Hash, b11.Hash}),
	}
	b21 := &types.Block{
		ProposerID: validators[2],
		ParentHash: b20.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 2,
		},
		Acks: common.NewSortedHashes(common.Hashes{b20.Hash, b01.Hash}),
	}
	b31 := &types.Block{
		ProposerID: validators[3],
		ParentHash: b30.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 3,
		},
		Acks: common.NewSortedHashes(common.Hashes{b30.Hash, b21.Hash}),
	}
	b02 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b01.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  2,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{b01.Hash, b21.Hash}),
	}
	b12 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b11.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  2,
			ChainID: 1,
		},
		Acks: common.NewSortedHashes(common.Hashes{b11.Hash, b21.Hash}),
	}
	b32 := &types.Block{
		ProposerID: validators[3],
		ParentHash: b31.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  2,
			ChainID: 3,
		},
		Acks: common.NewSortedHashes(common.Hashes{b31.Hash}),
	}
	b22 := &types.Block{
		ProposerID: validators[2],
		ParentHash: b21.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  2,
			ChainID: 2,
		},
		Acks: common.NewSortedHashes(common.Hashes{b21.Hash, b32.Hash}),
	}
	b23 := &types.Block{
		ProposerID: validators[2],
		ParentHash: b22.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  3,
			ChainID: 2,
		},
		Acks: common.NewSortedHashes(common.Hashes{b22.Hash}),
	}
	b03 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b02.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  3,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{b02.Hash, b22.Hash}),
	}
	b13 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b12.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  3,
			ChainID: 1,
		},
		Acks: common.NewSortedHashes(common.Hashes{b12.Hash, b22.Hash}),
	}
	b14 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b13.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  4,
			ChainID: 1,
		},
		Acks: common.NewSortedHashes(common.Hashes{b13.Hash}),
	}
	b41 := &types.Block{
		ProposerID: validators[4],
		ParentHash: b40.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 4,
		},
		Acks: common.NewSortedHashes(common.Hashes{b40.Hash}),
	}
	b42 := &types.Block{
		ProposerID: validators[4],
		ParentHash: b41.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  2,
			ChainID: 4,
		},
		Acks: common.NewSortedHashes(common.Hashes{b41.Hash}),
	}

	s.checkNotDeliver(to, b00)
	s.checkNotDeliver(to, b10)
	s.checkNotDeliver(to, b11)
	s.checkNotDeliver(to, b01)
	s.checkNotDeliver(to, b20)
	s.checkNotDeliver(to, b30)
	s.checkNotDeliver(to, b21)
	s.checkNotDeliver(to, b31)
	s.checkNotDeliver(to, b32)
	s.checkNotDeliver(to, b22)
	s.checkNotDeliver(to, b12)

	// Make sure 'acked' for current precedings is correct.
	acked := to.acked[b00.Hash]
	s.Require().NotNil(acked)
	s.Len(acked, 7)
	s.Contains(acked, b01.Hash)
	s.Contains(acked, b11.Hash)
	s.Contains(acked, b12.Hash)
	s.Contains(acked, b21.Hash)
	s.Contains(acked, b22.Hash)
	s.Contains(acked, b31.Hash)
	s.Contains(acked, b32.Hash)

	acked = to.acked[b10.Hash]
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
	s.Require().Len(to.candidateChainMapping, 2)

	// Check b00's height vector.
	candidate := to.candidates[0]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight, b00.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(2))
	s.Equal(candidate.ackedStatus[1].minHeight, b11.Position.Height)
	s.Equal(candidate.ackedStatus[1].count, uint64(2))
	s.Equal(candidate.ackedStatus[2].minHeight, b21.Position.Height)
	s.Equal(candidate.ackedStatus[2].count, uint64(2))
	s.Equal(candidate.ackedStatus[3].minHeight, b31.Position.Height)
	s.Equal(candidate.ackedStatus[3].count, uint64(2))
	s.Equal(candidate.ackedStatus[4].count, uint64(0))

	// Check b10's height vector.
	candidate = to.candidates[1]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight, b01.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(1))
	s.Equal(candidate.ackedStatus[1].minHeight, b10.Position.Height)
	s.Equal(candidate.ackedStatus[1].count, uint64(3))
	s.Equal(candidate.ackedStatus[2].minHeight, b20.Position.Height)
	s.Equal(candidate.ackedStatus[2].count, uint64(3))
	s.Equal(candidate.ackedStatus[3].minHeight, b30.Position.Height)
	s.Equal(candidate.ackedStatus[3].count, uint64(3))
	s.Equal(candidate.ackedStatus[4].count, uint64(0))

	// Check the first deliver.
	blocks, early, err := to.processBlock(b02)
	s.True(early)
	s.Nil(err)
	s.checkHashSequence(blocks, common.Hashes{b00.Hash, b10.Hash})

	// Make sure b00, b10 are removed from current working set.
	s.checkNotInWorkingSet(to, b00)
	s.checkNotInWorkingSet(to, b10)

	// Check if candidates of next round are picked correctly.
	s.Len(to.candidateChainMapping, 2)

	// Check b01's height vector.
	candidate = to.candidates[1]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight, b01.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(2))
	s.Equal(candidate.ackedStatus[1].minHeight, b11.Position.Height)
	s.Equal(candidate.ackedStatus[1].count, uint64(2))
	s.Equal(candidate.ackedStatus[2].minHeight, b21.Position.Height)
	s.Equal(candidate.ackedStatus[2].count, uint64(2))
	s.Equal(candidate.ackedStatus[3].minHeight, b11.Position.Height)
	s.Equal(candidate.ackedStatus[3].count, uint64(2))
	s.Equal(candidate.ackedStatus[4].count, uint64(0))

	// Check b20's height vector.
	candidate = to.candidates[2]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight, b02.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(1))
	s.Equal(candidate.ackedStatus[1].minHeight, b12.Position.Height)
	s.Equal(candidate.ackedStatus[1].count, uint64(1))
	s.Equal(candidate.ackedStatus[2].minHeight, b20.Position.Height)
	s.Equal(candidate.ackedStatus[2].count, uint64(3))
	s.Equal(candidate.ackedStatus[3].minHeight, b30.Position.Height)
	s.Equal(candidate.ackedStatus[3].count, uint64(3))
	s.Equal(candidate.ackedStatus[4].count, uint64(0))

	s.checkNotDeliver(to, b13)

	// Check the second deliver.
	blocks, early, err = to.processBlock(b03)
	s.True(early)
	s.Nil(err)
	s.checkHashSequence(blocks, common.Hashes{b11.Hash, b20.Hash})

	// Make sure b11, b20 are removed from current working set.
	s.checkNotInWorkingSet(to, b11)
	s.checkNotInWorkingSet(to, b20)

	// Add b40, b41, b42 to pending set.
	s.checkNotDeliver(to, b40)
	s.checkNotDeliver(to, b41)
	s.checkNotDeliver(to, b42)
	s.checkNotDeliver(to, b14)

	// Make sure b01, b30, b40 are candidate in next round.
	s.Len(to.candidateChainMapping, 3)
	candidate = to.candidates[0]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight, b01.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(3))
	s.Equal(candidate.ackedStatus[1].minHeight, b12.Position.Height)
	s.Equal(candidate.ackedStatus[1].count, uint64(3))
	s.Equal(candidate.ackedStatus[2].minHeight, b21.Position.Height)
	s.Equal(candidate.ackedStatus[2].count, uint64(2))
	s.Equal(candidate.ackedStatus[3].minHeight, b31.Position.Height)
	s.Equal(candidate.ackedStatus[3].count, uint64(2))
	s.Equal(candidate.ackedStatus[4].count, uint64(0))

	candidate = to.candidates[3]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].minHeight, b03.Position.Height)
	s.Equal(candidate.ackedStatus[0].count, uint64(1))
	s.Equal(candidate.ackedStatus[1].minHeight, b13.Position.Height)
	s.Equal(candidate.ackedStatus[1].count, uint64(2))
	s.Equal(candidate.ackedStatus[2].minHeight, b22.Position.Height)
	s.Equal(candidate.ackedStatus[2].count, uint64(1))
	s.Equal(candidate.ackedStatus[3].minHeight, b30.Position.Height)
	s.Equal(candidate.ackedStatus[3].count, uint64(3))
	s.Equal(candidate.ackedStatus[4].count, uint64(0))

	candidate = to.candidates[4]
	s.Require().NotNil(candidate)
	s.Equal(candidate.ackedStatus[0].count, uint64(0))
	s.Equal(candidate.ackedStatus[1].count, uint64(0))
	s.Equal(candidate.ackedStatus[2].count, uint64(0))
	s.Equal(candidate.ackedStatus[3].count, uint64(0))
	s.Equal(candidate.ackedStatus[4].minHeight, b40.Position.Height)
	s.Equal(candidate.ackedStatus[4].count, uint64(3))

	// Make 'Acking Node Set' contains blocks from all chains,
	// this should trigger not-early deliver.
	blocks, early, err = to.processBlock(b23)
	s.False(early)
	s.Nil(err)
	s.checkHashSequence(blocks, common.Hashes{b01.Hash, b30.Hash})

	// Make sure b01, b30 not in working set
	s.checkNotInWorkingSet(to, b01)
	s.checkNotInWorkingSet(to, b30)

	// Make sure b21, b40 are candidates of next round.
	s.Contains(to.candidateChainMapping, b21.Hash)
	s.Contains(to.candidateChainMapping, b40.Hash)
}

func (s *TotalOrderingTestSuite) TestBasicCaseForK0() {
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
	var (
		req        = s.Require()
		validators = test.GenerateRandomValidatorIDs(5)
		to         = newTotalOrdering(0, 3, uint32(len(validators)))
	)
	// Setup blocks.
	b00 := s.genGenesisBlock(validators, 0, common.Hashes{})
	b10 := s.genGenesisBlock(validators, 1, common.Hashes{})
	b20 := s.genGenesisBlock(validators, 2, common.Hashes{})
	b30 := s.genGenesisBlock(validators, 3, common.Hashes{b20.Hash})
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{b00.Hash, b10.Hash}),
	}
	b11 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b10.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 1,
		},
		Acks: common.NewSortedHashes(common.Hashes{b10.Hash, b20.Hash}),
	}
	b21 := &types.Block{
		ProposerID: validators[2],
		ParentHash: b20.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 2,
		},
		Acks: common.NewSortedHashes(common.Hashes{b20.Hash}),
	}
	b31 := &types.Block{
		ProposerID: validators[3],
		ParentHash: b30.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height:  1,
			ChainID: 3,
		},
		Acks: common.NewSortedHashes(common.Hashes{b21.Hash, b30.Hash}),
	}
	b40 := s.genGenesisBlock(validators, 4, common.Hashes{b31.Hash})

	s.checkNotDeliver(to, b00)
	s.checkNotDeliver(to, b10)
	s.checkNotDeliver(to, b20)
	s.checkNotDeliver(to, b30)
	s.checkNotDeliver(to, b01)
	s.checkNotDeliver(to, b11)
	s.checkNotDeliver(to, b21)
	s.checkNotDeliver(to, b31)

	// Check candidate status before delivering.
	candidate := to.candidates[0]
	req.NotNil(candidate)
	req.Equal(candidate.ackedStatus[0].minHeight, b00.Position.Height)
	req.Equal(candidate.ackedStatus[0].count, uint64(2))

	candidate = to.candidates[1]
	req.NotNil(candidate)
	req.Equal(candidate.ackedStatus[0].minHeight, b01.Position.Height)
	req.Equal(candidate.ackedStatus[0].count, uint64(1))
	req.Equal(candidate.ackedStatus[1].minHeight, b10.Position.Height)
	req.Equal(candidate.ackedStatus[1].count, uint64(2))

	candidate = to.candidates[2]
	req.NotNil(candidate)
	req.Equal(candidate.ackedStatus[1].minHeight, b11.Position.Height)
	req.Equal(candidate.ackedStatus[1].count, uint64(1))
	req.Equal(candidate.ackedStatus[2].minHeight, b20.Position.Height)
	req.Equal(candidate.ackedStatus[2].count, uint64(2))
	req.Equal(candidate.ackedStatus[3].minHeight, b30.Position.Height)
	req.Equal(candidate.ackedStatus[3].count, uint64(2))

	// This new block should trigger non-early deliver.
	blocks, early, err := to.processBlock(b40)
	req.False(early)
	req.Nil(err)
	s.checkHashSequence(blocks, common.Hashes{b20.Hash})

	// Make sure b20 is no long existing in working set.
	s.checkNotInWorkingSet(to, b20)

	// Make sure b10, b30 are candidates for next round.
	req.Contains(to.candidateChainMapping, b00.Hash)
	req.Contains(to.candidateChainMapping, b10.Hash)
	req.Contains(to.candidateChainMapping, b30.Hash)
}

func (s *TotalOrderingTestSuite) baseTestRandomlyGeneratedBlocks(
	totalOrderingConstructor func(types.ValidatorIDs) *totalOrdering,
	validatorCount, blockCount int,
	ackingCountGenerator func() int,
	repeat int) {

	var (
		req               = s.Require()
		gen               = test.NewBlocksGenerator(nil, hashBlock)
		revealingSequence = make(map[string]struct{})
		orderingSequence  = make(map[string]struct{})
	)

	db, err := blockdb.NewMemBackedBlockDB()
	req.Nil(err)
	validators, err := gen.Generate(
		validatorCount, blockCount, ackingCountGenerator, db)
	req.Nil(err)
	req.Len(validators, validatorCount)
	iter, err := db.GetAll()
	req.Nil(err)
	// Setup a revealer that would reveal blocks forming
	// valid DAGs.
	revealer, err := test.NewRandomDAGRevealer(iter)
	req.Nil(err)

	// TODO (mission): make this part run concurrently.
	for i := 0; i < repeat; i++ {
		revealed := ""
		ordered := ""
		revealer.Reset()
		to := totalOrderingConstructor(validators)
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
			hashes, _, err := to.processBlock(&b)
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

func (s *TotalOrderingTestSuite) TestRandomlyGeneratedBlocks() {
	var (
		validatorCount        = 13
		blockCount            = 50
		phi            uint64 = 10
		repeat                = 8
	)

	ackingCountGenerators := []func() int{
		nil, // Acking frequency with normal distribution.
		test.MaxAckingCountGenerator(0),              // Low acking frequency.
		test.MaxAckingCountGenerator(validatorCount), // High acking frequency.
	}

	// Test based on different acking frequency.
	for _, gen := range ackingCountGenerators {
		// Test for K=0.
		constructor := func(validators types.ValidatorIDs) *totalOrdering {
			return newTotalOrdering(0, phi, uint32(len(validators)))
		}
		s.baseTestRandomlyGeneratedBlocks(
			constructor, validatorCount, blockCount, gen, repeat)
		// Test for K=1,
		constructor = func(validators types.ValidatorIDs) *totalOrdering {
			return newTotalOrdering(1, phi, uint32(len(validators)))
		}
		s.baseTestRandomlyGeneratedBlocks(
			constructor, validatorCount, blockCount, gen, repeat)
		// Test for K=2,
		constructor = func(validators types.ValidatorIDs) *totalOrdering {
			return newTotalOrdering(2, phi, uint32(len(validators)))
		}
		s.baseTestRandomlyGeneratedBlocks(
			constructor, validatorCount, blockCount, gen, repeat)
		// Test for K=3,
		constructor = func(validators types.ValidatorIDs) *totalOrdering {
			return newTotalOrdering(3, phi, uint32(len(validators)))
		}
		s.baseTestRandomlyGeneratedBlocks(
			constructor, validatorCount, blockCount, gen, repeat)
	}
}

func TestTotalOrdering(t *testing.T) {
	suite.Run(t, new(TotalOrderingTestSuite))
}
