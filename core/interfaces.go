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

	"github.com/shopspring/decimal"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Application describes the application interface that interacts with DEXON
// consensus core.
type Application interface {
	// PreparePayload is called when consensus core is preparing a block.
	PreparePayloads(position types.Position) [][]byte

	// VerifyPayloads verifies if the payloads are valid.
	VerifyPayloads(payloads [][]byte) bool

	// BlockConfirmed is called when a block is confirmed and added to lattice.
	BlockConfirmed(block *types.Block)

	// StronglyAcked is called when a block is strongly acked.
	StronglyAcked(blockHash common.Hash)

	// TotalOrderingDeliver is called when the total ordering algorithm deliver
	// a set of block.
	TotalOrderingDeliver(blockHashes common.Hashes, early bool)

	// DeliverBlock is called when a block is add to the compaction chain.
	DeliverBlock(blockHash common.Hash, timestamp time.Time)

	// NotaryAckDeliver is called when a notary ack is created.
	NotaryAckDeliver(notaryAck *types.NotaryAck)
}

// Network describs the network interface that interacts with DEXON consensus
// core.
type Network interface {
	// BroadcastVote broadcasts vote to all nodes in DEXON network.
	BroadcastVote(vote *types.Vote)

	// BroadcastBlock broadcasts block to all nodes in DEXON network.
	BroadcastBlock(block *types.Block)

	// BroadcastNotaryAck broadcasts notaryAck to all nodes in DEXON network.
	BroadcastNotaryAck(notaryAck *types.NotaryAck)

	// ReceiveChan returns a channel to receive messages from DEXON network.
	ReceiveChan() <-chan interface{}
}

// Governance interface specifies interface to control the governance contract.
// Note that there are a lot more methods in the governance contract, that this
// interface only define those that are required to run the consensus algorithm.
type Governance interface {
	// Get the current validator set and it's corresponding stake.
	GetValidatorSet() map[types.ValidatorID]decimal.Decimal

	// Get K.
	GetTotalOrderingK() int

	// Get PhiRatio.
	GetPhiRatio() float32

	// Get block proposing interval (in milliseconds).
	GetBlockProposingInterval() int

	// Get Number of Chains.
	GetChainNumber() uint32

	// Get Genesis CRS.
	GetGenesisCRS() string

	// Get configuration change events after an epoch.
	GetConfigurationChangeEvent(epoch int) []types.ConfigurationChangeEvent
}
