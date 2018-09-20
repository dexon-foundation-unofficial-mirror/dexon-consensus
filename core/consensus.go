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
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
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
	ErrProposerNotInNotarySet = fmt.Errorf(
		"proposer is not in notary set")
	ErrIncorrectHash = fmt.Errorf(
		"hash of block is incorrect")
	ErrIncorrectSignature = fmt.Errorf(
		"signature of block is incorrect")
	ErrGenesisBlockNotEmpty = fmt.Errorf(
		"genesis block should be empty")
	ErrUnknownBlockProposed = fmt.Errorf(
		"unknown block is proposed")
	ErrUnknownBlockConfirmed = fmt.Errorf(
		"unknown block is confirmed")
	ErrIncorrectBlockPosition = fmt.Errorf(
		"position of block is incorrect")
)

// consensusReceiver implements agreementReceiver.
type consensusReceiver struct {
	consensus *Consensus
	chainID   uint32
	restart   chan struct{}
}

func (recv *consensusReceiver) ProposeVote(vote *types.Vote) {
	if err := recv.consensus.prepareVote(recv.chainID, vote); err != nil {
		log.Println(err)
		return
	}
	go func() {
		if err := recv.consensus.ProcessVote(vote); err != nil {
			log.Println(err)
			return
		}
		recv.consensus.network.BroadcastVote(vote)
	}()
}

func (recv *consensusReceiver) ProposeBlock(hash common.Hash) {
	block, exist := recv.consensus.baModules[recv.chainID].findCandidateBlock(hash)
	if !exist {
		log.Println(ErrUnknownBlockProposed)
		log.Println(hash)
		return
	}
	if err := recv.consensus.PreProcessBlock(block); err != nil {
		log.Println(err)
		return
	}
	recv.consensus.network.BroadcastBlock(block)
}

func (recv *consensusReceiver) ConfirmBlock(hash common.Hash) {
	block, exist := recv.consensus.baModules[recv.chainID].findCandidateBlock(hash)
	if !exist {
		log.Println(ErrUnknownBlockConfirmed, hash)
		return
	}
	if err := recv.consensus.ProcessBlock(block); err != nil {
		log.Println(err)
		return
	}
	recv.restart <- struct{}{}
}

// Consensus implements DEXON Consensus algorithm.
type Consensus struct {
	ID        types.NodeID
	app       Application
	gov       Governance
	config    *types.Config
	baModules []*agreement
	receivers []*consensusReceiver
	rbModule  *reliableBroadcast
	toModule  *totalOrdering
	ctModule  *consensusTimestamp
	ccModule  *compactionChain
	db        blockdb.BlockDatabase
	network   Network
	tickerObj Ticker
	prvKey    crypto.PrivateKey
	sigToPub  SigToPubFn
	lock      sync.RWMutex
	ctx       context.Context
	ctxCancel context.CancelFunc
}

// NewConsensus construct an Consensus instance.
func NewConsensus(
	app Application,
	gov Governance,
	db blockdb.BlockDatabase,
	network Network,
	prv crypto.PrivateKey,
	sigToPub SigToPubFn) *Consensus {

	config := gov.GetConfiguration(0)
	notarySet := gov.GetNotarySet()

	// Setup acking by information returned from Governace.
	rb := newReliableBroadcast()
	rb.setChainNum(config.NumChains)
	for nID := range notarySet {
		rb.addNode(nID)
	}
	// Setup context.
	ctx, ctxCancel := context.WithCancel(context.Background())

	// Setup sequencer by information returned from Governace.
	var nodes types.NodeIDs
	for nID := range notarySet {
		nodes = append(nodes, nID)
	}
	to := newTotalOrdering(
		uint64(config.K),
		uint64(float32(len(notarySet)-1)*config.PhiRatio+1),
		config.NumChains)

	con := &Consensus{
		ID:        types.NewNodeID(prv.PublicKey()),
		rbModule:  rb,
		toModule:  to,
		ctModule:  newConsensusTimestamp(),
		ccModule:  newCompactionChain(db, sigToPub),
		app:       newNonBlockingApplication(app),
		gov:       gov,
		config:    config,
		db:        db,
		network:   network,
		tickerObj: newTicker(gov),
		prvKey:    prv,
		sigToPub:  sigToPub,
		ctx:       ctx,
		ctxCancel: ctxCancel,
	}

	con.baModules = make([]*agreement, con.config.NumChains)
	con.receivers = make([]*consensusReceiver, con.config.NumChains)
	for i := uint32(0); i < con.config.NumChains; i++ {
		chainID := i
		con.receivers[chainID] = &consensusReceiver{
			consensus: con,
			chainID:   chainID,
			restart:   make(chan struct{}, 1),
		}
		blockProposer := func() *types.Block {
			block := con.proposeBlock(chainID)
			con.baModules[chainID].addCandidateBlock(block)
			return block
		}
		con.baModules[chainID] = newAgreement(
			con.ID,
			con.receivers[chainID],
			nodes,
			newGenesisLeaderSelector(con.config.GenesisCRS, con.sigToPub),
			con.sigToPub,
			blockProposer,
		)
	}
	return con
}

// Run starts running DEXON Consensus.
func (con *Consensus) Run() {
	ticks := make([]chan struct{}, 0, con.config.NumChains)
	for i := uint32(0); i < con.config.NumChains; i++ {
		tick := make(chan struct{})
		ticks = append(ticks, tick)
		go con.runBA(i, tick)
	}
	go con.processMsg(con.network.ReceiveChan(), con.PreProcessBlock)
	// Reset ticker.
	<-con.tickerObj.Tick()
	<-con.tickerObj.Tick()
	for {
		<-con.tickerObj.Tick()
		for _, tick := range ticks {
			go func(tick chan struct{}) { tick <- struct{}{} }(tick)
		}
	}
}

func (con *Consensus) runBA(chainID uint32, tick <-chan struct{}) {
	// TODO(jimmy-dexon): move this function inside agreement.
	notarySet := con.gov.GetNotarySet()
	nodes := make(types.NodeIDs, 0, len(notarySet))
	for nID := range notarySet {
		nodes = append(nodes, nID)
	}
	agreement := con.baModules[chainID]
	recv := con.receivers[chainID]
	recv.restart <- struct{}{}
	// Reset ticker
	<-tick
BALoop:
	for {
		select {
		case <-con.ctx.Done():
			break BALoop
		default:
		}
		for i := 0; i < agreement.clocks(); i++ {
			<-tick
		}
		select {
		case <-recv.restart:
			// TODO(jimmy-dexon): handling change of notary set.
			aID := types.Position{
				ShardID: 0,
				ChainID: chainID,
				Height:  con.rbModule.nextHeight(chainID),
			}
			agreement.restart(nodes, aID)
		default:
		}
		err := agreement.nextState()
		if err != nil {
			log.Printf("[%s] %s\n", con.ID.String(), err)
			break BALoop
		}
	}
}

// RunLegacy starts running Legacy DEXON Consensus.
func (con *Consensus) RunLegacy() {
	go con.processMsg(con.network.ReceiveChan(), con.ProcessBlock)

	chainID := uint32(0)
	hashes := make(common.Hashes, 0, len(con.gov.GetNotarySet()))
	for nID := range con.gov.GetNotarySet() {
		hashes = append(hashes, nID.Hash)
	}
	sort.Sort(hashes)
	for i, hash := range hashes {
		if hash == con.ID.Hash {
			chainID = uint32(i)
			break
		}
	}
	con.rbModule.setChainNum(uint32(len(hashes)))

	genesisBlock := &types.Block{
		ProposerID: con.ID,
		Position: types.Position{
			ChainID: chainID,
		},
	}
	if err := con.PrepareGenesisBlock(genesisBlock, time.Now().UTC()); err != nil {
		log.Println(err)
	}
	if err := con.ProcessBlock(genesisBlock); err != nil {
		log.Println(err)
	}
	con.network.BroadcastBlock(genesisBlock)

ProposingBlockLoop:
	for {
		select {
		case <-con.tickerObj.Tick():
		case <-con.ctx.Done():
			break ProposingBlockLoop
		}
		block := &types.Block{
			ProposerID: con.ID,
			Position: types.Position{
				ChainID: chainID,
			},
		}
		if err := con.PrepareBlock(block, time.Now().UTC()); err != nil {
			log.Println(err)
		}
		if err := con.ProcessBlock(block); err != nil {
			log.Println(err)
		}
		con.network.BroadcastBlock(block)
	}
}

// Stop the Consensus core.
func (con *Consensus) Stop() {
	con.ctxCancel()
}

func (con *Consensus) processMsg(
	msgChan <-chan interface{},
	blockProcesser func(*types.Block) error) {
	for {
		var msg interface{}
		select {
		case msg = <-msgChan:
		case <-con.ctx.Done():
			return
		}

		switch val := msg.(type) {
		case *types.Block:
			if err := blockProcesser(val); err != nil {
				log.Println(err)
			}
		case *types.WitnessAck:
			if err := con.ProcessWitnessAck(val); err != nil {
				log.Println(err)
			}
		case *types.Vote:
			if err := con.ProcessVote(val); err != nil {
				log.Println(err)
			}
		}
	}
}

func (con *Consensus) proposeBlock(chainID uint32) *types.Block {
	block := &types.Block{
		ProposerID: con.ID,
		Position: types.Position{
			ChainID: chainID,
			Height:  con.rbModule.nextHeight(chainID),
		},
	}
	if err := con.PrepareBlock(block, time.Now().UTC()); err != nil {
		log.Println(err)
		return nil
	}
	if err := con.baModules[chainID].prepareBlock(block, con.prvKey); err != nil {
		log.Println(err)
		return nil
	}
	return block
}

// ProcessVote is the entry point to submit ont vote to a Consensus instance.
func (con *Consensus) ProcessVote(vote *types.Vote) (err error) {
	v := vote.Clone()
	err = con.baModules[v.Position.ChainID].processVote(v)
	return err
}

// prepareVote prepares a vote.
func (con *Consensus) prepareVote(chainID uint32, vote *types.Vote) error {
	return con.baModules[chainID].prepareVote(vote, con.prvKey)
}

// sanityCheck checks if the block is a valid block
func (con *Consensus) sanityCheck(b *types.Block) (err error) {
	// Check block.Position.
	if b.Position.ShardID != 0 || b.Position.ChainID >= con.rbModule.chainNum() {
		return ErrIncorrectBlockPosition
	}
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

// PreProcessBlock performs Byzantine Agreement on the block.
func (con *Consensus) PreProcessBlock(b *types.Block) (err error) {
	if err := con.sanityCheck(b); err != nil {
		return err
	}
	if err := con.baModules[b.Position.ChainID].processBlock(b); err != nil {
		return err
	}
	return
}

// ProcessBlock is the entry point to submit one block to a Consensus instance.
func (con *Consensus) ProcessBlock(block *types.Block) (err error) {
	if err := con.sanityCheck(block); err != nil {
		return err
	}
	var (
		deliveredBlocks []*types.Block
		earlyDelivered  bool
	)
	// To avoid application layer modify the content of block during
	// processing, we should always operate based on the cloned one.
	b := block.Clone()

	con.lock.Lock()
	defer con.lock.Unlock()
	// Perform reliable broadcast checking.
	if err = con.rbModule.processBlock(b); err != nil {
		return err
	}
	con.app.BlockConfirmed(block)
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
		err = con.ctModule.processBlocks(deliveredBlocks)
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
			con.app.DeliverBlock(b.Hash, b.Witness.Timestamp)
			// TODO(mission): Find a way to safely recycle the block.
			//                We should deliver block directly to
			//                nonBlockingApplication and let them recycle the
			//                block.
		}
		var witnessAck *types.WitnessAck
		witnessAck, err = con.ccModule.prepareWitnessAck(con.prvKey)
		if err != nil {
			return
		}
		err = con.ProcessWitnessAck(witnessAck)
		if err != nil {
			return
		}
		con.app.WitnessAckDeliver(witnessAck)
	}
	return
}

func (con *Consensus) checkPrepareBlock(
	b *types.Block, proposeTime time.Time) (err error) {
	if (b.ProposerID == types.NodeID{}) {
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
	b.Timestamp = proposeTime
	b.Payload = con.app.PreparePayload(b.Position)
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
	if len(b.Payload) != 0 {
		err = ErrGenesisBlockNotEmpty
		return
	}
	b.Position.Height = 0
	b.ParentHash = common.Hash{}
	b.Timestamp = proposeTime
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

// ProcessWitnessAck is the entry point to submit one witness ack.
func (con *Consensus) ProcessWitnessAck(witnessAck *types.WitnessAck) (err error) {
	witnessAck = witnessAck.Clone()
	if _, exists := con.gov.GetNotarySet()[witnessAck.ProposerID]; !exists {
		err = ErrProposerNotInNotarySet
		return
	}
	err = con.ccModule.processWitnessAck(witnessAck)
	return
}

// WitnessAcks returns the latest WitnessAck received from all other nodes.
func (con *Consensus) WitnessAcks() map[types.NodeID]*types.WitnessAck {
	return con.ccModule.witnessAcks()
}
