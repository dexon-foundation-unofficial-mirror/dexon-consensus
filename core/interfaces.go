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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Application describes the application interface that interacts with DEXON
// consensus core.
type Application interface {
	// PrepareBlock is called when consensus core is preparing a block.
	PrepareBlock(position types.Position) (payload []byte, witnessData []byte)

	// VerifyBlock verifies if the block is valid.
	VerifyBlock(block *types.Block) bool

	// BlockDelivered is called when a block is add to the compaction chain.
	BlockDelivered(block types.Block)
}

// Debug describes the application interface that requires
// more detailed consensus execution.
type Debug interface {
	// BlockConfirmed is called when a block is confirmed and added to lattice.
	BlockConfirmed(blockHash common.Hash)

	// StronglyAcked is called when a block is strongly acked.
	StronglyAcked(blockHash common.Hash)

	// TotalOrderingDelivered is called when the total ordering algorithm deliver
	// a set of block.
	TotalOrderingDelivered(common.Hashes, bool)
}

// Network describs the network interface that interacts with DEXON consensus
// core.
type Network interface {
	// BroadcastVote broadcasts vote to all nodes in DEXON network.
	BroadcastVote(vote *types.Vote)

	// BroadcastBlock broadcasts block to all nodes in DEXON network.
	BroadcastBlock(block *types.Block)

	// SendDKGPrivateShare sends PrivateShare to a DKG participant.
	SendDKGPrivateShare(pub crypto.PublicKey, prvShare *types.DKGPrivateShare)

	// BroadcastDKGPrivateShare broadcasts PrivateShare to all DKG participants.
	BroadcastDKGPrivateShare(prvShare *types.DKGPrivateShare)

	// BroadcastDKGPartialSignature broadcasts partialSignature to all
	// DKG participants.
	BroadcastDKGPartialSignature(psig *types.DKGPartialSignature)

	// ReceiveChan returns a channel to receive messages from DEXON network.
	ReceiveChan() <-chan interface{}
}

// Governance interface specifies interface to control the governance contract.
// Note that there are a lot more methods in the governance contract, that this
// interface only define those that are required to run the consensus algorithm.
type Governance interface {
	// GetConfiguration returns the configuration at a given round.
	// Return the genesis configuration if round == 0.
	GetConfiguration(round uint64) *types.Config

	// GetCRS returns the CRS for a given round.
	// Return the genesis CRS if round == 0.
	GetCRS(round uint64) common.Hash

	// Propose a CRS of round.
	ProposeCRS(round uint64, crs []byte)

	// GetNodeSet returns the node set at a given round.
	// Return the genesis node set if round == 0.
	GetNodeSet(round uint64) []crypto.PublicKey

	// Porpose a ThresholdSignature of round.
	ProposeThresholdSignature(round uint64, signature crypto.Signature)

	// Get a ThresholdSignature of round.
	GetThresholdSignature(round uint64) (crypto.Signature, bool)

	//// DKG-related methods.

	// AddDKGComplaint adds a DKGComplaint.
	AddDKGComplaint(complaint *types.DKGComplaint)

	// GetDKGComplaints gets all the DKGComplaints of round.
	DKGComplaints(round uint64) []*types.DKGComplaint

	// AddDKGMasterPublicKey adds a DKGMasterPublicKey.
	AddDKGMasterPublicKey(masterPublicKey *types.DKGMasterPublicKey)

	// DKGMasterPublicKeys gets all the DKGMasterPublicKey of round.
	DKGMasterPublicKeys(round uint64) []*types.DKGMasterPublicKey
}

// Ticker define the capability to tick by interval.
type Ticker interface {
	// Tick would return a channel, which would be triggered until next tick.
	Tick() <-chan time.Time

	// Stop the ticker.
	Stop()
}
