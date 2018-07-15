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
	"testing"

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

// TestNetwork.
type TestNetwork struct {
}

func (n *TestNetwork) Join(endpoint Endpoint) chan interface{} {
	return nil
}

func (n *TestNetwork) BroadcastBlock(block *types.Block) {
}

// TestApp.
type TestApp struct {
	Outputs []*types.Block
	Early   bool
}

func (a *TestApp) ValidateBlock(b *types.Block) bool {
	return true
}

func (a *TestApp) Deliver(blocks []*types.Block, early bool) {
	a.Outputs = blocks
	a.Early = early
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

	lattice = NewBlockLattice(
		blockdb.NewMemBackedBlockDB(),
		&TestNetwork{},
		s.app)

	for i := 0; i < 4; i++ {
		validators = append(validators, types.ValidatorID(common.NewRandomHash()))
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
			b22.Hash: struct{}{},
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

func (s *BlockLatticeTest) TestAckAndStateTransition() {
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

	s.Require().Equal(0, len(b01.IndirectAcks))
	s.Require().Equal(0, len(b01.AckedBy))

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
	s.Require().Equal(1, len(b01.AckedBy))
	s.Require().NotNil(b01.AckedBy[b11.Hash])
	// b11 indirect acks.
	s.Require().Equal(1, len(b11.IndirectAcks))
	s.Require().Equal(0, len(b11.AckedBy))
	s.Require().NotNil(b11.IndirectAcks[genesises[0].Hash])

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
	s.Require().Equal(2, len(b01.AckedBy))
	s.Require().NotNil(b01.AckedBy[b21.Hash])
	// b21 indirect acks.
	s.Require().Equal(1, len(b21.IndirectAcks))
	s.Require().Equal(0, len(b21.AckedBy))

	// B31
	lattice.ProcessBlock(b31)

	// Set status check.
	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(4, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(1, len(lattice.pendingSet))

	s.Require().NotNil(lattice.pendingSet[b01.Hash])
	s.Require().Equal(types.BlockStatusToTo, b01.State)

	// b01 is acked.
	s.Require().Equal(3, len(b01.AckedBy))
	s.Require().NotNil(b01.AckedBy[b31.Hash])

	// b11 is acked.
	s.Require().Equal(1, len(b11.AckedBy))
	s.Require().NotNil(b11.AckedBy[b31.Hash])

	// b31 indirect acks.
	s.Require().Equal(2, len(b31.IndirectAcks))
	s.Require().Equal(1, len(b31.AckedBy))
	s.Require().NotNil(b31.AckedBy[b12.Hash])

	// b21 & b31 is acked by b12 (which is previously in waiting set).
	s.Require().Equal(1, len(b21.AckedBy))
	s.Require().NotNil(b21.AckedBy[b12.Hash])
	s.Require().Equal(1, len(b31.AckedBy))
	s.Require().NotNil(b31.AckedBy[b12.Hash])

	// B02
	lattice.ProcessBlock(b02)

	// Set status check.
	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(4, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(1, len(lattice.pendingSet))

	// b11 is acked.
	s.Require().Equal(2, len(b11.AckedBy))
	s.Require().NotNil(b11.AckedBy[b02.Hash])
	// b21 is acked.
	s.Require().Equal(2, len(b21.AckedBy))
	s.Require().NotNil(b21.AckedBy[b02.Hash])
	s.Require().NotNil(b21.AckedBy[b12.Hash])
	// b31 is acked.
	s.Require().Equal(2, len(b31.AckedBy))
	s.Require().NotNil(b31.AckedBy[b02.Hash])
	s.Require().NotNil(b31.AckedBy[b12.Hash])

	// B32
	lattice.ProcessBlock(b32)

	// Set status check.
	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(4, len(lattice.ackCandidateSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(2, len(lattice.pendingSet))

	s.Require().NotNil(lattice.pendingSet[b01.Hash])
	s.Require().NotNil(lattice.pendingSet[b21.Hash])

	// b02 is acked.
	s.Require().Equal(1, len(b02.AckedBy))
	s.Require().NotNil(b02.AckedBy[b32.Hash])
	// b12 is acked.
	s.Require().Equal(1, len(b12.AckedBy))
	s.Require().NotNil(b12.AckedBy[b32.Hash])

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
	s.Require().Equal(types.BlockStatusToTo, b01.State)
	s.Require().Equal(types.BlockStatusToTo, b11.State)
	s.Require().Equal(types.BlockStatusToTo, b21.State)
	s.Require().Equal(types.BlockStatusToTo, b31.State)

	// b02 is acked.
	s.Require().Equal(2, len(b02.AckedBy))
	s.Require().NotNil(b02.AckedBy[b22.Hash])
	// b12 is acked.
	s.Require().Equal(2, len(b12.AckedBy))
	s.Require().NotNil(b12.AckedBy[b22.Hash])
	// b31 is acked.
	s.Require().Equal(3, len(b31.AckedBy))
	s.Require().NotNil(b31.AckedBy[b22.Hash])

	// b22 indirect acks.
	s.Require().NotNil(b22.IndirectAcks[b11.Hash])
	s.Require().NotNil(b22.IndirectAcks[b01.Hash])

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

	lattice.ProcessBlock(b01)
	lattice.ProcessBlock(b12)
	lattice.ProcessBlock(b11)
	lattice.ProcessBlock(b21)

	// B01 in pendingSet after b31 is recieved.
	lattice.ProcessBlock(b31)
	s.Require().NotNil(lattice.pendingSet[b01.Hash])

	// Run total ordering for b01.
	lattice.totalOrdering(b01)
	s.Require().Equal(0, len(s.app.Outputs))
	s.Require().Equal(1, len(lattice.candidateSet))
	s.Require().Equal(1, len(lattice.pendingSet))
	s.Require().NotNil(lattice.candidateSet[b01.Hash])

	// ABS & AHV
	s.Require().Equal(1, len(lattice.AHV[b01.Hash]))

	lattice.ProcessBlock(b02)
	lattice.ProcessBlock(b32)

	// B21 in pendingSet after b32 is recieved.
	s.Require().Equal(2, len(lattice.pendingSet))
	s.Require().NotNil(lattice.pendingSet[b01.Hash])
	s.Require().NotNil(lattice.pendingSet[b21.Hash])

	// Run total ordering for b21.
	lattice.totalOrdering(b21)
	s.Require().Equal(2, len(lattice.pendingSet))
	s.Require().Equal(0, len(s.app.Outputs))

	// ABS & AHV
	s.Require().Equal(uint64(1), lattice.ABS[b01.Hash][b21.ProposerID])
	s.Require().Equal(uint64(1), lattice.AHV[b01.Hash][b21.ProposerID])

	lattice.ProcessBlock(b22)
	s.Require().Equal(4, len(lattice.pendingSet))

	// Run total ordering for b31.
	lattice.totalOrdering(b31)
	s.Require().Equal(1, len(s.app.Outputs))
	s.Require().Equal(b01.Hash, s.app.Outputs[0].Hash)
	s.Require().Equal(false, s.app.Early)
	lattice.updateABSAHV()
	s.Require().Equal(3, len(lattice.abs()))
	s.Require().Equal(2, len(lattice.candidateSet))
	s.app.Clear()

	lattice.totalOrdering(b22)
	s.Require().Equal(0, len(s.app.Outputs))
	s.Require().Equal(2, len(lattice.candidateSet))
	s.Require().Equal(3, len(lattice.abs()))

	lattice.ProcessBlock(b13, true)
	s.Require().Equal(2, len(s.app.Outputs))
	s.Require().Equal(b21.Hash, s.app.Outputs[0].Hash)
	s.Require().Equal(b11.Hash, s.app.Outputs[1].Hash)
	s.Require().Equal(false, s.app.Early)
	s.app.Clear()

	lattice.ProcessBlock(b33, true)
	s.Require().Equal(0, len(s.app.Outputs))

	lattice.ProcessBlock(b03, true)
	s.Require().Equal(1, len(s.app.Outputs))
	s.Require().Equal(b31.Hash, s.app.Outputs[0].Hash)
	s.Require().Equal(false, s.app.Early)
	s.app.Clear()

	lattice.ProcessBlock(b23, true)
	s.Require().Equal(2, len(s.app.Outputs))
	s.Require().Equal(b02.Hash, s.app.Outputs[0].Hash)
	s.Require().Equal(b12.Hash, s.app.Outputs[1].Hash)

	s.Require().Equal(0, len(lattice.waitingSet))
	s.Require().Equal(0, len(lattice.stronglyAckedSet))
	s.Require().Equal(2, len(lattice.pendingSet))
	lattice.updateABSAHV()
	s.Require().Equal(2, len(lattice.abs()))
}

func TestBlockLattice(t *testing.T) {
	suite.Run(t, new(BlockLatticeTest))
}
