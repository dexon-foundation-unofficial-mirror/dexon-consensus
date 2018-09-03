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
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// SigToPubFn is a function to recover public key from signature.
type SigToPubFn func(hash common.Hash, signature crypto.Signature) (
	crypto.PublicKey, error)

// ErrMissingBlockInfo would be reported if some information is missing when
// calling PrepareBlock. It implements error interface.
type ErrMissingBlockInfo struct {
	MissingField string
}

func (e *ErrMissingBlockInfo) Error() string {
	return "missing " + e.MissingField + " in block"
}

// Errors for consensus core.
var (
	ErrProposerNotValidator = fmt.Errorf(
		"proposer is not a validator")
	ErrIncorrectHash = fmt.Errorf(
		"hash of block is incorrect")
	ErrIncorrectSignature = fmt.Errorf(
		"signature of block is incorrect")
	ErrGenesisBlockNotEmpty = fmt.Errorf(
		"genesis block should be empty")
)

// Consensus implements DEXON Consensus algorithm.
type Consensus struct {
	ID       types.ValidatorID
	app      Application
	gov      Governance
	rbModule *reliableBroadcast
	toModule *totalOrdering
	ctModule *consensusTimestamp
	ccModule *compactionChain
	db       blockdb.BlockDatabase
	network  Network
	tick     *time.Ticker
	prvKey   crypto.PrivateKey
	sigToPub SigToPubFn
	lock     sync.RWMutex
	stopChan chan struct{}
}

// NewConsensus construct an Consensus instance.
func NewConsensus(
	app Application,
	gov Governance,
	db blockdb.BlockDatabase,
	network Network,
	tick *time.Ticker,
	prv crypto.PrivateKey,
	sigToPub SigToPubFn) *Consensus {
	validatorSet := gov.GetValidatorSet()

	// Setup acking by information returned from Governace.
	rb := newReliableBroadcast()
	rb.setChainNum(len(validatorSet))
	for vID := range validatorSet {
		rb.addValidator(vID)
	}

	// Setup sequencer by information returned from Governace.
	var validators types.ValidatorIDs
	for vID := range validatorSet {
		validators = append(validators, vID)
	}
	to := newTotalOrdering(
		uint64(gov.GetTotalOrderingK()),
		uint64(float32(len(validatorSet)-1)*gov.GetPhiRatio()+1),
		validators)

	return &Consensus{
		ID:       types.NewValidatorID(prv.PublicKey()),
		rbModule: rb,
		toModule: to,
		ctModule: newConsensusTimestamp(),
		ccModule: newCompactionChain(db, sigToPub),
		app:      newNonBlockingApplication(app),
		gov:      gov,
		db:       db,
		network:  network,
		tick:     tick,
		prvKey:   prv,
		sigToPub: sigToPub,
		stopChan: make(chan struct{}),
	}
}

// Run starts running Consensus core.
func (con *Consensus) Run() {
	go con.processMsg(con.network.ReceiveChan())

	chainID := uint32(0)
	hashes := make(common.Hashes, 0, len(con.gov.GetValidatorSet()))
	for vID := range con.gov.GetValidatorSet() {
		hashes = append(hashes, vID.Hash)
	}
	sort.Sort(hashes)
	for i, hash := range hashes {
		if hash == con.ID.Hash {
			chainID = uint32(i)
			break
		}
	}

	genesisBlock := &types.Block{
		ProposerID: con.ID,
		Position: types.Position{
			ChainID: chainID,
		},
	}
	if err := con.PrepareGenesisBlock(genesisBlock, time.Now().UTC()); err != nil {
		fmt.Println(err)
	}
	if err := con.ProcessBlock(genesisBlock); err != nil {
		fmt.Println(err)
	}
	con.network.BroadcastBlock(genesisBlock)

ProposingBlockLoop:
	for {
		select {
		case <-con.tick.C:
		case <-con.stopChan:
			break ProposingBlockLoop
		}
		block := &types.Block{
			ProposerID: con.ID,
			Position: types.Position{
				ChainID: chainID,
			},
		}
		if err := con.PrepareBlock(block, time.Now().UTC()); err != nil {
			fmt.Println(err)
		}
		if err := con.ProcessBlock(block); err != nil {
			fmt.Println(err)
		}
		con.network.BroadcastBlock(block)
	}
}

// Stop the Consensus core.
func (con *Consensus) Stop() {
	con.stopChan <- struct{}{}
	con.stopChan <- struct{}{}
}

func (con *Consensus) processMsg(msgChan <-chan interface{}) {
	for {
		var msg interface{}
		select {
		case msg = <-msgChan:
		case <-con.stopChan:
			return
		}

		switch val := msg.(type) {
		case *types.Block:
			if err := con.ProcessBlock(val); err != nil {
				fmt.Println(err)
			}
			types.RecycleBlock(val)
		case *types.NotaryAck:
			if err := con.ProcessNotaryAck(val); err != nil {
				fmt.Println(err)
			}
		case *types.Vote:
			if err := con.ProcessVote(val); err != nil {
				fmt.Println(err)
			}
		}
	}
}

// ProcessVote is the entry point to submit ont vote to a Consensus instance.
func (con *Consensus) ProcessVote(vote *types.Vote) (err error) {
	return
}

// sanityCheck checks if the block is a valid block
func (con *Consensus) sanityCheck(b *types.Block) (err error) {
	// Check the hash of block.
	hash, err := hashBlock(b)
	if err != nil || hash != b.Hash {
		return ErrIncorrectHash
	}

	// Check the signer.
	pubKey, err := con.sigToPub(b.Hash, b.Signature)
	if err != nil {
		return err
	}
	if !b.ProposerID.Equal(crypto.Keccak256Hash(pubKey.Bytes())) {
		return ErrIncorrectSignature
	}
	return nil
}

// ProcessBlock is the entry point to submit one block to a Consensus instance.
func (con *Consensus) ProcessBlock(b *types.Block) (err error) {
	// TODO(jimmy-dexon): BlockConverter.Block() is called twice in this method.
	if err := con.sanityCheck(b); err != nil {
		return err
	}
	var (
		deliveredBlocks []*types.Block
		earlyDelivered  bool
	)
	// To avoid application layer modify the content of block during
	// processing, we should always operate based on the cloned one.
	b = b.Clone()

	con.lock.Lock()
	defer con.lock.Unlock()
	// Perform reliable broadcast checking.
	if err = con.rbModule.processBlock(b); err != nil {
		return err
	}
	con.app.BlockConfirmed(b.Clone())
	for _, b := range con.rbModule.extractBlocks() {
		// Notify application layer that some block is strongly acked.
		con.app.StronglyAcked(b.Hash)
		// Perform total ordering.
		deliveredBlocks, earlyDelivered, err = con.toModule.processBlock(b)
		if err != nil {
			return
		}
		if len(deliveredBlocks) == 0 {
			continue
		}
		for _, b := range deliveredBlocks {
			if err = con.db.Put(*b); err != nil {
				return
			}
		}
		// TODO(mission): handle membership events here.
		hashes := make(common.Hashes, len(deliveredBlocks))
		for idx := range deliveredBlocks {
			hashes[idx] = deliveredBlocks[idx].Hash
		}
		con.app.TotalOrderingDeliver(hashes, earlyDelivered)
		// Perform timestamp generation.
		deliveredBlocks, _, err = con.ctModule.processBlocks(
			deliveredBlocks)
		if err != nil {
			return
		}
		for _, b := range deliveredBlocks {
			if err = con.ccModule.processBlock(b); err != nil {
				return
			}
			if err = con.db.Update(*b); err != nil {
				return
			}
			con.app.DeliverBlock(b.Hash, b.Notary.Timestamp)
			// TODO(mission): Find a way to safely recycle the block.
			//                We should deliver block directly to
			//                nonBlockingApplication and let them recycle the
			//                block.
		}
		var notaryAck *types.NotaryAck
		notaryAck, err = con.ccModule.prepareNotaryAck(con.prvKey)
		if err != nil {
			return
		}
		err = con.ProcessNotaryAck(notaryAck)
		if err != nil {
			return
		}
		con.app.NotaryAckDeliver(notaryAck)
	}
	return
}

func (con *Consensus) checkPrepareBlock(
	b *types.Block, proposeTime time.Time) (err error) {
	if (b.ProposerID == types.ValidatorID{}) {
		err = &ErrMissingBlockInfo{MissingField: "ProposerID"}
		return
	}
	return
}

// PrepareBlock would setup header fields of block based on its ProposerID.
func (con *Consensus) PrepareBlock(b *types.Block,
	proposeTime time.Time) (err error) {
	if err = con.checkPrepareBlock(b, proposeTime); err != nil {
		return
	}
	con.lock.RLock()
	defer con.lock.RUnlock()

	con.rbModule.prepareBlock(b)
	b.Timestamps[b.ProposerID] = proposeTime
	b.Payloads = con.app.PreparePayloads(b.Position)
	b.Hash, err = hashBlock(b)
	if err != nil {
		return
	}
	b.Signature, err = con.prvKey.Sign(b.Hash)
	if err != nil {
		return
	}
	return
}

// PrepareGenesisBlock would setup header fields for genesis block.
func (con *Consensus) PrepareGenesisBlock(b *types.Block,
	proposeTime time.Time) (err error) {
	if err = con.checkPrepareBlock(b, proposeTime); err != nil {
		return
	}
	if len(b.Payloads) != 0 {
		err = ErrGenesisBlockNotEmpty
		return
	}
	b.Position.Height = 0
	b.ParentHash = common.Hash{}
	b.Acks = make(map[common.Hash]struct{})
	b.Timestamps = make(map[types.ValidatorID]time.Time)
	for vID := range con.gov.GetValidatorSet() {
		b.Timestamps[vID] = time.Time{}
	}
	b.Timestamps[b.ProposerID] = proposeTime
	b.Hash, err = hashBlock(b)
	if err != nil {
		return
	}
	b.Signature, err = con.prvKey.Sign(b.Hash)
	if err != nil {
		return
	}
	return
}

// ProcessNotaryAck is the entry point to submit one notary ack.
func (con *Consensus) ProcessNotaryAck(notaryAck *types.NotaryAck) (err error) {
	notaryAck = notaryAck.Clone()
	if _, exists := con.gov.GetValidatorSet()[notaryAck.ProposerID]; !exists {
		err = ErrProposerNotValidator
		return
	}
	err = con.ccModule.processNotaryAck(notaryAck)
	return
}

// NotaryAcks returns the latest NotaryAck received from all other validators.
func (con *Consensus) NotaryAcks() map[types.ValidatorID]*types.NotaryAck {
	return con.ccModule.notaryAcks()
}
