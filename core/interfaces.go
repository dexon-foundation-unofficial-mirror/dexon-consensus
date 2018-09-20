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
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Application describes the application interface that interacts with DEXON
// consensus core.
type Application interface {
	// PreparePayload is called when consensus core is preparing a block.
	PreparePayload(position types.Position) []byte

	// VerifyPayloads verifies if the payloads are valid.
	VerifyPayloads(payloads []byte) bool

	// BlockConfirmed is called when a block is confirmed and added to lattice.
	BlockConfirmed(block *types.Block)

	// StronglyAcked is called when a block is strongly acked.
	StronglyAcked(blockHash common.Hash)

	// TotalOrderingDeliver is called when the total ordering algorithm deliver
	// a set of block.
	TotalOrderingDeliver(blockHashes common.Hashes, early bool)

	// DeliverBlock is called when a block is add to the compaction chain.
	DeliverBlock(blockHash common.Hash, timestamp time.Time)

	// BlockProcessedChan returns a channel to receive the block hashes that have
	// finished processing by the application.
	BlockProcessedChan() <-chan types.WitnessResult

	// WitnessAckDeliver is called when a witness ack is created.
	WitnessAckDeliver(witnessAck *types.WitnessAck)
}

// Network describs the network interface that interacts with DEXON consensus
// core.
type Network interface {
	// BroadcastVote broadcasts vote to all nodes in DEXON network.
	BroadcastVote(vote *types.Vote)

	// BroadcastBlock broadcasts block to all nodes in DEXON network.
	BroadcastBlock(block *types.Block)

	// BroadcastWitnessAck broadcasts witnessAck to all nodes in DEXON network.
	BroadcastWitnessAck(witnessAck *types.WitnessAck)

	// SendDKGPrivateShare sends PrivateShare to a DKG participant.
	SendDKGPrivateShare(recv types.NodeID, prvShare *types.DKGPrivateShare)

	// ReceiveChan returns a channel to receive messages from DEXON network.
	ReceiveChan() <-chan interface{}
}

// Governance interface specifies interface to control the governance contract.
// Note that there are a lot more methods in the governance contract, that this
// interface only define those that are required to run the consensus algorithm.
type Governance interface {
	// GetConfiguration returns the configuration at a given block height.
	GetConfiguration(blockHeight uint64) *types.Config

	// Get the current notary set.
	GetNotarySet() map[types.NodeID]struct{}

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
