// Copyright 2018 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package core

import (
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

// Application describes the application interface that interacts with DEXON
// consensus core.
type Application interface {
	// PreparePayload is called when consensus core is preparing a block.
	PreparePayload(position types.Position) ([]byte, error)

	// PrepareWitness will return the witness data no lower than consensusHeight.
	PrepareWitness(consensusHeight uint64) (types.Witness, error)

	// VerifyBlock verifies if the block is valid.
	VerifyBlock(block *types.Block) types.BlockVerifyStatus

	// BlockConfirmed is called when a block is confirmed and added to lattice.
	BlockConfirmed(block types.Block)

	// BlockDelivered is called when a block is added to the compaction chain.
	BlockDelivered(blockHash common.Hash,
		blockPosition types.Position, result types.FinalizationResult)
}

// Debug describes the application interface that requires
// more detailed consensus execution.
type Debug interface {
	// BlockReceived is called when the block received in agreement.
	BlockReceived(common.Hash)
	// TotalOrderingDelivered is called when the total ordering algorithm deliver
	// a set of block.
	TotalOrderingDelivered(common.Hashes, uint32)
	// BlockReady is called when the block's randomness is ready.
	BlockReady(common.Hash)
}

// Network describs the network interface that interacts with DEXON consensus
// core.
type Network interface {
	// PullBlocks tries to pull blocks from the DEXON network.
	PullBlocks(hashes common.Hashes)

	// PullVotes tries to pull votes from the DEXON network.
	PullVotes(position types.Position)

	// BroadcastVote broadcasts vote to all nodes in DEXON network.
	BroadcastVote(vote *types.Vote)

	// BroadcastBlock broadcasts block to all nodes in DEXON network.
	BroadcastBlock(block *types.Block)

	// BroadcastAgreementResult broadcasts agreement result to DKG set.
	BroadcastAgreementResult(randRequest *types.AgreementResult)

	// BroadcastRandomnessResult broadcasts rand request to Notary set.
	BroadcastRandomnessResult(randResult *types.BlockRandomnessResult)

	// SendDKGPrivateShare sends PrivateShare to a DKG participant.
	SendDKGPrivateShare(pub crypto.PublicKey, prvShare *typesDKG.PrivateShare)

	// BroadcastDKGPrivateShare broadcasts PrivateShare to all DKG participants.
	BroadcastDKGPrivateShare(prvShare *typesDKG.PrivateShare)

	// BroadcastDKGPartialSignature broadcasts partialSignature to all
	// DKG participants.
	BroadcastDKGPartialSignature(psig *typesDKG.PartialSignature)

	// ReceiveChan returns a channel to receive messages from DEXON network.
	ReceiveChan() <-chan interface{}
}

// Governance interface specifies interface to control the governance contract.
// Note that there are a lot more methods in the governance contract, that this
// interface only define those that are required to run the consensus algorithm.
type Governance interface {
	// Configuration returns the configuration at a given round.
	// Return the genesis configuration if round == 0.
	Configuration(round uint64) *types.Config

	// CRS returns the CRS for a given round.
	// Return the genesis CRS if round == 0.
	CRS(round uint64) common.Hash

	// Propose a CRS of round.
	ProposeCRS(round uint64, signedCRS []byte)

	// NodeSet returns the node set at a given round.
	// Return the genesis node set if round == 0.
	NodeSet(round uint64) []crypto.PublicKey

	// NotifyRoundHeight notifies governance contract the consensus height of
	// the first block of the given round.
	NotifyRoundHeight(targetRound, consensusHeight uint64)

	//// DKG-related methods.

	// AddDKGComplaint adds a DKGComplaint.
	AddDKGComplaint(round uint64, complaint *typesDKG.Complaint)

	// DKGComplaints gets all the DKGComplaints of round.
	DKGComplaints(round uint64) []*typesDKG.Complaint

	// AddDKGMasterPublicKey adds a DKGMasterPublicKey.
	AddDKGMasterPublicKey(round uint64, masterPublicKey *typesDKG.MasterPublicKey)

	// DKGMasterPublicKeys gets all the DKGMasterPublicKey of round.
	DKGMasterPublicKeys(round uint64) []*typesDKG.MasterPublicKey

	// AddDKGFinalize adds a DKG finalize message.
	AddDKGFinalize(round uint64, final *typesDKG.Finalize)

	// IsDKGFinal checks if DKG is final.
	IsDKGFinal(round uint64) bool
}

// Ticker define the capability to tick by interval.
type Ticker interface {
	// Tick would return a channel, which would be triggered until next tick.
	Tick() <-chan time.Time

	// Stop the ticker.
	Stop()
}
