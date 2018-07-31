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
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

var lattice *BlockLattice
var validators []types.ValidatorID
var genesises []*types.Block

var b01, b11, b21, b31,
	b02, b12, b22, b32,
	b03, b13, b23, b33 *types.Block

// TestApp.
type TestApp struct {
	Outputs []*types.Block
	Early   bool
}

func (a *TestApp) TotalOrderingDeliver(blocks []*types.Block, early bool) {
	a.Outputs = append(a.Outputs, blocks...)
	a.Early = early
}

func (a *TestApp) DeliverBlock(blockHashes common.Hash, timestamp time.Time) {
}

func (a *TestApp) Clear() {
	a.Outputs = nil
	a.Early = false
}

type BlockLatticeTest struct {
	suite.Suite

	app *TestApp
}

func (s *BlockLatticeTest) SetupTest() {
	Debugf("--------------------------------------------" +
		"-------------------------\n")

	s.app = &TestApp{}

	db, err := blockdb.NewMemBackedBlockDB()
	s.Require().Nil(err)
	lattice = NewBlockLattice(db, s.app)

	for i := 0; i < 4; i++ {
		validators = append(validators,
			types.ValidatorID{Hash: common.NewRandomHash()})
		Debugf("V%d: %s\n", i, validators[i])
	}
	Debugf("\n")
	for i := 0; i < 4; i++ {
		hash := common.NewRandomHash()
		genesises = append(genesises, &types.Block{
			ProposerID: validators[i],
			ParentHash: hash,
			Hash:       hash,
			Height:     0,
			Acks:       map[common.Hash]struct{}{},
		})

		Debugf("G%d: %s\n", i, hash)
		lattice.AddValidator(validators[i], genesises[i])
	}

	// Make lattice validator[0]'s local view.
	lattice.SetOwner(validators[0])

	b01 = &types.Block{
		ProposerID: validators[0],
		ParentHash: genesises[0].Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			genesises[1].Hash: struct{}{},
			genesises[2].Hash: struct{}{},
			genesises[3].Hash: struct{}{},
		},
	}

	b11 = &types.Block{
		ProposerID: validators[1],
		ParentHash: genesises[1].Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b01.Hash:          struct{}{},
			genesises[2].Hash: struct{}{},
			genesises[3].Hash: struct{}{},
		},
	}
	b21 = &types.Block{
		ProposerID: validators[2],
		ParentHash: genesises[2].Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b01.Hash:          struct{}{},
			genesises[1].Hash: struct{}{},
			genesises[3].Hash: struct{}{},
		},
	}
	b31 = &types.Block{
		ProposerID: validators[3],
		ParentHash: genesises[3].Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			b01.Hash:          struct{}{},
			b11.Hash:          struct{}{},
			genesises[2].Hash: struct{}{},
		},
	}

	b02 = &types.Block{
		ProposerID: validators[0],
		ParentHash: b01.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b11.Hash: struct{}{},
			b21.Hash: struct{}{},
			b31.Hash: struct{}{},
		},
	}
	b12 = &types.Block{
		ProposerID: validators[1],
		ParentHash: b11.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b21.Hash: struct{}{},
			b31.Hash: struct{}{},
		},
	}
	b22 = &types.Block{
		ProposerID: validators[2],
		ParentHash: b21.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b02.Hash: struct{}{},
			b12.Hash: struct{}{},
			b31.Hash: struct{}{},
		},
	}
	b32 = &types.Block{
		ProposerID: validators[3],
		ParentHash: b31.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			b02.Hash: struct{}{},
			b12.Hash: struct{}{},
			b21.Hash: struct{}{},
		},
	}

	b03 = &types.Block{
		ProposerID: validators[0],
		ParentHash: b02.Hash,
		Hash:       common.NewRandomHash(),
		Height:     3,
		Acks: map[common.Hash]struct{}{
			b12.Hash: struct{}{},
			b32.Hash: struct{}{},
		},
	}
	b13 = &types.Block{
		ProposerID: validators[1],
		ParentHash: b12.Hash,
		Hash:       common.NewRandomHash(),
		Height:     3,
		Acks: map[common.Hash]struct{}{
			b02.Hash: struct{}{},
			b22.Hash: struct{}{},
			b32.Hash: struct{}{},
		},
	}
	b23 = &types.Block{
		ProposerID: validators[2],
		ParentHash: b22.Hash,
		Hash:       common.NewRandomHash(),
		Height:     3,
		Acks: map[common.Hash]struct{}{
			b02.Hash: struct{}{},
			b12.Hash: struct{}{},
			b32.Hash: struct{}{},
		},
	}
	b33 = &types.Block{
		ProposerID: validators[3],
		ParentHash: b32.Hash,
		Hash:       common.NewRandomHash(),
		Height:     3,
		Acks: map[common.Hash]struct{}{
			b02.Hash: struct{}{},
			b12.Hash: struct{}{},
			b22.Hash: struct{}{},
		},
	}
	Debugf("\n")
	Debugf("B01: %s\n", b01.Hash)
	Debugf("B11: %s\n", b11.Hash)
	Debugf("B21: %s\n", b21.Hash)
	Debugf("B31: %s\n", b31.Hash)
	Debugf("\n")
	Debugf("B02: %s\n", b02.Hash)
	Debugf("B12: %s\n", b12.Hash)
	Debugf("B22: %s\n", b22.Hash)
	Debugf("B32: %s\n", b32.Hash)
	Debugf("\n")
	Debugf("B03: %s\n", b03.Hash)
	Debugf("B13: %s\n", b13.Hash)
	Debugf("B23: %s\n", b23.Hash)
	Debugf("B33: %s\n", b33.Hash)
	Debugf("\n")
}

func (s *BlockLatticeTest) TestAckAndStatusTransition() {
	// Recieve Order:
	//   B01 -> B12 -> B11 -> B21 -> B31 -> B02 -> B32 -> B22 -> B13 -> B33
	//       -> B03 -> B23

	// B01
	lattice.ProcessBlock(b01)

	// Set status check.
	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(1, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(0, len(lattice.pendingSet))

	s.Require().Equal(0, len(b01.Ackeds))

	// B12
	lattice.ProcessBlock(b12)

	// Set status check.
	s.Require().Equal(1, len(lattice.waitingSet))
	s.Require().Equal(1, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(0, len(lattice.pendingSet))

	s.Require().NotNil(lattice.waitingSet[b12.Hash])

	// B11
	lattice.ProcessBlock(b11)

	// b01 is acked.
	s.Require().Equal(1, len(b01.Ackeds))
	s.Require().NotNil(b01.Ackeds[b11.Hash])
	// b11 indirect acks.
	s.Require().Equal(0, len(b11.Ackeds))

	// Set status check.
	s.Require().Equal(1, len(lattice.waitingSet))
	s.Require().Equal(2, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(0, len(lattice.pendingSet))

	// B21
	lattice.ProcessBlock(b21)

	// Set status check.
	s.Require().Equal(1, len(lattice.waitingSet))
	s.Require().Equal(3, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(0, len(lattice.pendingSet))

	// b01 is acked.
	s.Require().Equal(2, len(b01.Ackeds))
	s.Require().NotNil(b01.Ackeds[b21.Hash])
	// b21 indirect acks.
	s.Require().Equal(0, len(b21.Ackeds))

	// B31
	lattice.ProcessBlock(b31)

	// Set status check.
	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(4, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(1, len(lattice.pendingSet))

	s.Require().NotNil(lattice.pendingSet[b01.Hash])
	s.Require().Equal(types.BlockStatusOrdering, b01.Status)

	// b01 is acked.
	s.Require().Equal(4, len(b01.Ackeds))
	s.Require().NotNil(b01.Ackeds[b31.Hash])

	// b11 is acked.
	s.Require().Equal(2, len(b11.Ackeds))
	s.Require().NotNil(b11.Ackeds[b31.Hash])
	s.Require().NotNil(b11.Ackeds[b12.Hash])

	// b31 indirect acks.
	s.Require().Equal(1, len(b31.Ackeds))
	s.Require().NotNil(b31.Ackeds[b12.Hash])

	// b21 & b31 is acked by b12 (which is previously in waiting set).
	s.Require().Equal(1, len(b21.Ackeds))
	s.Require().NotNil(b21.Ackeds[b12.Hash])
	s.Require().Equal(1, len(b31.Ackeds))
	s.Require().NotNil(b31.Ackeds[b12.Hash])

	// B02
	lattice.ProcessBlock(b02)

	// Set status check.
	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(4, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(2, len(lattice.pendingSet))

	s.Require().NotNil(lattice.pendingSet[b01.Hash])
	s.Require().NotNil(lattice.pendingSet[b11.Hash])

	// b11 is acked.
	s.Require().Equal(3, len(b11.Ackeds))
	s.Require().NotNil(b11.Ackeds[b02.Hash])
	// b21 is acked.
	s.Require().Equal(2, len(b21.Ackeds))
	s.Require().NotNil(b21.Ackeds[b02.Hash])
	s.Require().NotNil(b21.Ackeds[b12.Hash])
	// b31 is acked.
	s.Require().Equal(2, len(b31.Ackeds))
	s.Require().NotNil(b31.Ackeds[b02.Hash])
	s.Require().NotNil(b31.Ackeds[b12.Hash])

	// B32
	lattice.ProcessBlock(b32)

	// Set status check.
	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(4, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(4, len(lattice.pendingSet))

	s.Require().NotNil(lattice.pendingSet[b01.Hash])
	s.Require().NotNil(lattice.pendingSet[b11.Hash])
	s.Require().NotNil(lattice.pendingSet[b21.Hash])
	s.Require().NotNil(lattice.pendingSet[b31.Hash])

	// b02 is acked.
	s.Require().Equal(1, len(b02.Ackeds))
	s.Require().NotNil(b02.Ackeds[b32.Hash])
	// b12 is acked.
	s.Require().Equal(1, len(b12.Ackeds))
	s.Require().NotNil(b12.Ackeds[b32.Hash])

	// B22
	lattice.ProcessBlock(b22)

	// Set status check.
	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(4, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(4, len(lattice.pendingSet))

	s.Require().NotNil(lattice.pendingSet[b01.Hash])
	s.Require().NotNil(lattice.pendingSet[b11.Hash])
	s.Require().NotNil(lattice.pendingSet[b21.Hash])
	s.Require().NotNil(lattice.pendingSet[b31.Hash])
	s.Require().Equal(types.BlockStatusOrdering, b01.Status)
	s.Require().Equal(types.BlockStatusOrdering, b11.Status)
	s.Require().Equal(types.BlockStatusOrdering, b21.Status)
	s.Require().Equal(types.BlockStatusOrdering, b31.Status)

	// b02 is acked.
	s.Require().Equal(2, len(b02.Ackeds))
	s.Require().NotNil(b02.Ackeds[b22.Hash])
	// b12 is acked.
	s.Require().Equal(2, len(b12.Ackeds))
	s.Require().NotNil(b12.Ackeds[b22.Hash])
	// b31 is acked.
	s.Require().Equal(4, len(b31.Ackeds))
	s.Require().NotNil(b31.Ackeds[b22.Hash])

	// B13, B33, B03, B23
	lattice.ProcessBlock(b13)
	lattice.ProcessBlock(b33)
	lattice.ProcessBlock(b03)
	lattice.ProcessBlock(b23)

	s.Require().Equal(8, len(lattice.pendingSet))
}

func (s *BlockLatticeTest) TesttotalOrdering() {
	// Recieve Order:
	//   B01 -> B12 -> B11 -> B21 -> B31 -> B02 -> B32 -> B22 -> B13 -> B33
	//       -> B03 -> B23

	lattice.ProcessBlock(b01, true)
	lattice.ProcessBlock(b12, true)
	lattice.ProcessBlock(b11, true)
	lattice.ProcessBlock(b21, true)

	// B01 in pendingSet after b31 is recieved.
	lattice.ProcessBlock(b31, true)
	s.Require().NotNil(lattice.pendingSet[b01.Hash])

	s.Require().Equal(0, len(s.app.Outputs))
	s.Require().Equal(1, len(lattice.candidateSet))
	s.Require().Equal(1, len(lattice.pendingSet))
	s.Require().NotNil(lattice.candidateSet[b01.Hash])

	// ABS & AHV
	s.Require().Equal(1, len(lattice.AHV[b01.Hash]))

	lattice.ProcessBlock(b02, true)
	lattice.ProcessBlock(b32, true)

	// B21 in pendingSet after b32 is recieved.
	s.Require().Equal(3, len(lattice.pendingSet))
	s.Require().NotNil(lattice.pendingSet[b11.Hash])
	s.Require().NotNil(lattice.pendingSet[b21.Hash])
	s.Require().NotNil(lattice.pendingSet[b31.Hash])

	s.Require().Equal(3, len(lattice.pendingSet))
	s.Require().Equal(1, len(s.app.Outputs))
	s.Require().Equal(b01.Hash, s.app.Outputs[0].Hash)

	// ABS & AHV
	lattice.updateABSAHV()

	s.Require().Equal(2, len(lattice.ABS[b11.Hash]))
	s.Require().Equal(uint64(1), lattice.ABS[b11.Hash][validators[1]])
	s.Require().Equal(uint64(1), lattice.ABS[b11.Hash][validators[3]])
	s.Require().Equal(1, len(lattice.ABS[b21.Hash]))
	s.Require().Equal(uint64(1), lattice.ABS[b21.Hash][validators[2]])

	s.Require().Equal(uint64(1), lattice.AHV[b11.Hash][validators[1]])
	s.Require().Equal(infinity, lattice.AHV[b11.Hash][validators[2]])
	s.Require().Equal(uint64(1), lattice.AHV[b11.Hash][validators[3]])

	s.Require().Equal(infinity, lattice.AHV[b21.Hash][validators[1]])
	s.Require().Equal(uint64(1), lattice.AHV[b21.Hash][validators[2]])
	s.Require().Equal(infinity, lattice.AHV[b21.Hash][validators[3]])

	// B22
	s.app.Clear()
	lattice.ProcessBlock(b22, true)
	s.Require().Equal(0, len(s.app.Outputs))
	s.Require().Equal(3, len(lattice.pendingSet))
	s.Require().Equal(2, len(lattice.candidateSet))

	// ABS & AHV
	lattice.updateABSAHV()
	s.Require().Equal(3, len(lattice.abs()))

	// B13
	s.app.Clear()
	lattice.ProcessBlock(b13, true)
	s.Require().Equal(2, len(s.app.Outputs))
	expected := common.Hashes{b21.Hash, b11.Hash}
	sort.Sort(expected)
	got := common.Hashes{s.app.Outputs[0].Hash, s.app.Outputs[1].Hash}
	sort.Sort(got)
	s.Require().Equal(expected, got)
	s.Require().Equal(false, s.app.Early)

	s.Require().Equal(1, len(lattice.candidateSet))
	s.Require().NotNil(lattice.candidateSet[b31.Hash])

	// ABS & AHV
	lattice.updateABSAHV()
	s.Require().Equal(3, len(lattice.abs()))

	// B33
	s.app.Clear()
	lattice.ProcessBlock(b33, true)
	s.Require().Equal(0, len(s.app.Outputs))
	s.Require().Equal(1, len(lattice.candidateSet))
	s.Require().Equal(3, len(lattice.pendingSet))
	s.Require().NotNil(lattice.candidateSet[b31.Hash])

	// ABS & AHV
	lattice.updateABSAHV()
	s.Require().Equal(3, len(lattice.abs()))

	// B03
	s.app.Clear()
	lattice.ProcessBlock(b03, true)
	s.Require().Equal(0, len(s.app.Outputs))

	// B23
	s.app.Clear()
	lattice.ProcessBlock(b23, true)
	s.Require().Equal(1, len(s.app.Outputs))
	s.Require().Equal(b31.Hash, s.app.Outputs[0].Hash)

	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(4, len(lattice.pendingSet))
	s.Require().Equal(2, len(lattice.candidateSet))
}

func TestBlockLattice(t *testing.T) {
	suite.Run(t, new(BlockLatticeTest))
}
