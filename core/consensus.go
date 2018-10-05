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
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Errors for consensus core.
var (
	ErrProposerNotInNodeSet = fmt.Errorf(
		"proposer is not in node set")
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
	ErrIncorrectAgreementResultPosition = fmt.Errorf(
		"incorrect agreement result position")
	ErrNotEnoughVotes = fmt.Errorf(
		"not enought votes")
	ErrIncorrectVoteProposer = fmt.Errorf(
		"incorrect vote proposer")
)

// consensusBAReceiver implements agreementReceiver.
type consensusBAReceiver struct {
	// TODO(mission): consensus would be replaced by lattice and network.
	consensus        *Consensus
	agreementModule  *agreement
	chainID          uint32
	changeNotaryTime time.Time
	restartNotary    chan bool
}

func (recv *consensusBAReceiver) ProposeVote(vote *types.Vote) {
	if err := recv.agreementModule.prepareVote(vote); err != nil {
		log.Println(err)
		return
	}
	go func() {
		if err := recv.agreementModule.processVote(vote); err != nil {
			log.Println(err)
			return
		}
		recv.consensus.network.BroadcastVote(vote)
	}()
}

func (recv *consensusBAReceiver) ProposeBlock() {
	block := recv.consensus.proposeBlock(recv.chainID)
	recv.consensus.baModules[recv.chainID].addCandidateBlock(block)
	if err := recv.consensus.preProcessBlock(block); err != nil {
		log.Println(err)
		return
	}
	recv.consensus.network.BroadcastBlock(block)
}

func (recv *consensusBAReceiver) ConfirmBlock(hash common.Hash) {
	block, exist := recv.consensus.baModules[recv.chainID].findCandidateBlock(hash)
	if !exist {
		log.Println(ErrUnknownBlockConfirmed, hash)
		return
	}
	if err := recv.consensus.processBlock(block); err != nil {
		log.Println(err)
		return
	}
	recv.restartNotary <- block.Timestamp.After(recv.changeNotaryTime)
}

// consensusDKGReceiver implements dkgReceiver.
type consensusDKGReceiver struct {
	ID           types.NodeID
	gov          Governance
	authModule   *Authenticator
	nodeSetCache *NodeSetCache
	network      Network
}

// ProposeDKGComplaint proposes a DKGComplaint.
func (recv *consensusDKGReceiver) ProposeDKGComplaint(
	complaint *types.DKGComplaint) {
	if err := recv.authModule.SignDKGComplaint(complaint); err != nil {
		log.Println(err)
		return
	}
	recv.gov.AddDKGComplaint(complaint)
}

// ProposeDKGMasterPublicKey propose a DKGMasterPublicKey.
func (recv *consensusDKGReceiver) ProposeDKGMasterPublicKey(
	mpk *types.DKGMasterPublicKey) {
	if err := recv.authModule.SignDKGMasterPublicKey(mpk); err != nil {
		log.Println(err)
		return
	}
	recv.gov.AddDKGMasterPublicKey(mpk)
}

// ProposeDKGPrivateShare propose a DKGPrivateShare.
func (recv *consensusDKGReceiver) ProposeDKGPrivateShare(
	prv *types.DKGPrivateShare) {
	if err := recv.authModule.SignDKGPrivateShare(prv); err != nil {
		log.Println(err)
		return
	}
	receiverPubKey, exists := recv.nodeSetCache.GetPublicKey(prv.ReceiverID)
	if !exists {
		log.Println("public key for receiver not found")
		return
	}
	recv.network.SendDKGPrivateShare(receiverPubKey, prv)
}

// ProposeDKGAntiNackComplaint propose a DKGPrivateShare as an anti complaint.
func (recv *consensusDKGReceiver) ProposeDKGAntiNackComplaint(
	prv *types.DKGPrivateShare) {
	if prv.ProposerID == recv.ID {
		if err := recv.authModule.SignDKGPrivateShare(prv); err != nil {
			log.Println(err)
			return
		}
	}
	recv.network.BroadcastDKGPrivateShare(prv)
}

// Consensus implements DEXON Consensus algorithm.
type Consensus struct {
	// Node Info.
	ID            types.NodeID
	authModule    *Authenticator
	currentConfig *types.Config

	// Modules.
	nbModule *nonBlocking

	// BA.
	baModules []*agreement
	receivers []*consensusBAReceiver

	// DKG.
	dkgRunning int32
	dkgReady   *sync.Cond
	cfgModule  *configurationChain

	// Dexon consensus v1's modules.
	lattice  *Lattice
	ccModule *compactionChain

	// Interfaces.
	db        blockdb.BlockDatabase
	gov       Governance
	network   Network
	tickerObj Ticker

	// Misc.
	nodeSetCache *NodeSetCache
	round        uint64
	lock         sync.RWMutex
	ctx          context.Context
	ctxCancel    context.CancelFunc
	event        *common.Event
}

// NewConsensus construct an Consensus instance.
func NewConsensus(
	app Application,
	gov Governance,
	db blockdb.BlockDatabase,
	network Network,
	prv crypto.PrivateKey) *Consensus {

	// TODO(w): load latest blockHeight from DB, and use config at that height.
	var round uint64
	config := gov.Configuration(round)
	// TODO(w): notarySet is different for each chain, need to write a
	// GetNotarySetForChain(nodeSet, chainID, crs) function to get the
	// correct notary set for a given chain.
	nodeSetCache := NewNodeSetCache(gov)
	crs := gov.CRS(round)
	// Setup acking by information returned from Governace.
	nodes, err := nodeSetCache.GetNodeSet(0)
	if err != nil {
		panic(err)
	}
	// Setup context.
	ctx, ctxCancel := context.WithCancel(context.Background())
	// Setup auth module.
	authModule := NewAuthenticator(prv)
	// Check if the application implement Debug interface.
	debugApp, _ := app.(Debug)
	// Setup nonblocking module.
	nbModule := newNonBlocking(app, debugApp)
	// Init lattice.
	lattice := NewLattice(round, config, authModule, nbModule, nbModule, db)
	// Init configuration chain.
	ID := types.NewNodeID(prv.PublicKey())
	cfgModule := newConfigurationChain(
		ID,
		&consensusDKGReceiver{
			ID:           ID,
			gov:          gov,
			authModule:   authModule,
			nodeSetCache: nodeSetCache,
			network:      network,
		},
		gov)
	// Register DKG for the initial round. This is a temporary function call for
	// simulation.
	cfgModule.registerDKG(0, config.NumDKGSet/3)
	// Construct Consensus instance.
	con := &Consensus{
		ID:            ID,
		currentConfig: config,
		ccModule:      newCompactionChain(db),
		lattice:       lattice,
		nbModule:      nbModule,
		gov:           gov,
		db:            db,
		network:       network,
		tickerObj:     newTicker(gov, 0, TickerBA),
		dkgReady:      sync.NewCond(&sync.Mutex{}),
		cfgModule:     cfgModule,
		nodeSetCache:  nodeSetCache,
		ctx:           ctx,
		ctxCancel:     ctxCancel,
		authModule:    authModule,
		event:         common.NewEvent(),
	}

	con.baModules = make([]*agreement, config.NumChains)
	con.receivers = make([]*consensusBAReceiver, config.NumChains)
	for i := uint32(0); i < config.NumChains; i++ {
		chainID := i
		recv := &consensusBAReceiver{
			consensus:     con,
			chainID:       chainID,
			restartNotary: make(chan bool, 1),
		}
		agreementModule := newAgreement(
			con.ID,
			recv,
			nodes.IDs,
			newLeaderSelector(crs),
			con.authModule,
		)
		// Hacky way to make agreement module self contained.
		recv.agreementModule = agreementModule
		con.baModules[chainID] = agreementModule
		con.receivers[chainID] = recv
	}
	return con
}

// Run starts running DEXON Consensus.
func (con *Consensus) Run() {
	go con.processMsg(con.network.ReceiveChan())
	startTime := time.Now().UTC().Add(con.currentConfig.RoundInterval / 2)
	con.initialRound(startTime)
	func() {
		con.dkgReady.L.Lock()
		defer con.dkgReady.L.Unlock()
		for con.dkgRunning != 2 {
			con.dkgReady.Wait()
		}
	}()
	time.Sleep(startTime.Sub(time.Now().UTC()))
	ticks := make([]chan struct{}, 0, con.currentConfig.NumChains)
	for i := uint32(0); i < con.currentConfig.NumChains; i++ {
		tick := make(chan struct{})
		ticks = append(ticks, tick)
		go con.runBA(i, tick)
	}

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
	agreement := con.baModules[chainID]
	recv := con.receivers[chainID]
	recv.changeNotaryTime = time.Now().UTC()
	recv.restartNotary <- true
	nIDs := make(map[types.NodeID]struct{})
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
		case newNotary := <-recv.restartNotary:
			if newNotary {
				recv.changeNotaryTime.Add(con.currentConfig.RoundInterval)
				nodes, err := con.nodeSetCache.GetNodeSet(con.round)
				if err != nil {
					panic(err)
				}
				nIDs = nodes.GetSubSet(con.gov.Configuration(con.round).NumNotarySet,
					types.NewNotarySetTarget(con.gov.CRS(con.round), chainID))
			}
			agreement.restart(nIDs, con.lattice.NextPosition(chainID))
		default:
		}
		err := agreement.nextState()
		if err != nil {
			log.Printf("[%s] %s\n", con.ID.String(), err)
			break BALoop
		}
	}
}

// runDKGTSIG starts running DKG+TSIG protocol.
func (con *Consensus) runDKGTSIG() {
	con.dkgReady.L.Lock()
	defer con.dkgReady.L.Unlock()
	if con.dkgRunning != 0 {
		return
	}
	con.dkgRunning = 1
	go func() {
		startTime := time.Now().UTC()
		defer func() {
			con.dkgReady.L.Lock()
			defer con.dkgReady.L.Unlock()
			con.dkgReady.Broadcast()
			con.dkgRunning = 2
			DKGTime := time.Now().Sub(startTime)
			if DKGTime.Nanoseconds() >=
				con.currentConfig.RoundInterval.Nanoseconds()/2 {
				log.Printf("[%s] WARNING!!! Your computer cannot finish DKG on time!\n",
					con.ID)
			}
		}()
		round := con.round
		if err := con.cfgModule.runDKG(round); err != nil {
			panic(err)
		}
		nodes, err := con.nodeSetCache.GetNodeSet(round)
		if err != nil {
			panic(err)
		}
		hash := HashConfigurationBlock(
			nodes.IDs,
			con.gov.Configuration(round),
			common.Hash{},
			con.cfgModule.prevHash)
		psig, err := con.cfgModule.preparePartialSignature(
			round, hash)
		if err != nil {
			panic(err)
		}
		if err = con.authModule.SignDKGPartialSignature(psig); err != nil {
			panic(err)
		}
		if err = con.cfgModule.processPartialSignature(psig); err != nil {
			panic(err)
		}
		con.network.BroadcastDKGPartialSignature(psig)
		if _, err = con.cfgModule.runBlockTSig(round, hash); err != nil {
			panic(err)
		}
	}()
}

func (con *Consensus) runCRS() {
	// Start running next round CRS.
	psig, err := con.cfgModule.preparePartialSignature(
		con.round, con.gov.CRS(con.round))
	if err != nil {
		log.Println(err)
	} else if err = con.authModule.SignDKGPartialSignature(psig); err != nil {
		log.Println(err)
	} else if err = con.cfgModule.processPartialSignature(psig); err != nil {
		log.Println(err)
	} else {
		con.network.BroadcastDKGPartialSignature(psig)
		crs, err := con.cfgModule.runCRSTSig(con.round, con.gov.CRS(con.round))
		if err != nil {
			log.Println(err)
		} else {
			con.gov.ProposeCRS(con.round+1, crs)
		}
	}
}

func (con *Consensus) initialRound(startTime time.Time) {
	con.currentConfig = con.gov.Configuration(con.round)
	func() {
		con.dkgReady.L.Lock()
		defer con.dkgReady.L.Unlock()
		con.dkgRunning = 0
	}()
	con.runDKGTSIG()

	con.event.RegisterTime(startTime.Add(con.currentConfig.RoundInterval/2),
		func(time.Time) {
			go con.runCRS()
		})
	con.event.RegisterTime(startTime.Add(con.currentConfig.RoundInterval/2),
		func(time.Time) {
			con.cfgModule.registerDKG(con.round+1, con.currentConfig.NumDKGSet/3)
		})
	con.event.RegisterTime(startTime.Add(con.currentConfig.RoundInterval),
		func(time.Time) {
			// Change round.
			con.round++
			con.initialRound(startTime.Add(con.currentConfig.RoundInterval))
		})
}

// Stop the Consensus core.
func (con *Consensus) Stop() {
	con.ctxCancel()
}

func (con *Consensus) processMsg(msgChan <-chan interface{}) {
	for {
		var msg interface{}
		select {
		case msg = <-msgChan:
		case <-con.ctx.Done():
			return
		}

		switch val := msg.(type) {
		case *types.Block:
			if err := con.preProcessBlock(val); err != nil {
				log.Println(err)
			}
		case *types.Vote:
			if err := con.ProcessVote(val); err != nil {
				log.Println(err)
			}
		case *types.AgreementResult:
			if err := con.ProcessAgreementResult(val); err != nil {
				log.Println(err)
			}
		case *types.BlockRandomnessResult:
			if err := con.ProcessBlockRandomnessResult(val); err != nil {
				log.Println(err)
			}
		case *types.DKGPrivateShare:
			if err := con.cfgModule.processPrivateShare(val); err != nil {
				log.Println(err)
			}

		case *types.DKGPartialSignature:
			if err := con.cfgModule.processPartialSignature(val); err != nil {
				log.Println(err)
			}
		}
	}
}

func (con *Consensus) proposeBlock(chainID uint32) *types.Block {
	block := &types.Block{
		Position: types.Position{
			ChainID: chainID,
		},
	}
	if err := con.prepareBlock(block, time.Now().UTC()); err != nil {
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

// ProcessAgreementResult processes the randomness request.
func (con *Consensus) ProcessAgreementResult(
	rand *types.AgreementResult) error {
	if con.round != rand.Round {
		return nil
	}
	dkgSet, err := con.nodeSetCache.GetDKGSet(rand.Round)
	if err != nil {
		return err
	}
	if _, exist := dkgSet[con.ID]; !exist {
		return nil
	}
	if len(rand.Votes) <= con.currentConfig.NumNotarySet/3*2 {
		return ErrNotEnoughVotes
	}
	if rand.Position.ChainID >= con.currentConfig.NumChains {
		return ErrIncorrectAgreementResultPosition
	}
	notarySet, err := con.nodeSetCache.GetNotarySet(
		rand.Round, rand.Position.ChainID)
	if err != nil {
		return err
	}
	for _, vote := range rand.Votes {
		if _, exist := notarySet[vote.ProposerID]; !exist {
			return ErrIncorrectVoteProposer
		}
		ok, err := verifyVoteSignature(&vote)
		if err != nil {
			return nil
		}
		if !ok {
			return ErrIncorrectVoteSignature
		}
	}
	return nil
}

// ProcessBlockRandomnessResult processes the randomness result.
func (con *Consensus) ProcessBlockRandomnessResult(
	rand *types.BlockRandomnessResult) error {
	return nil
}

// preProcessBlock performs Byzantine Agreement on the block.
func (con *Consensus) preProcessBlock(b *types.Block) (err error) {
	if err = con.lattice.SanityCheck(b); err != nil {
		return
	}
	if err = con.baModules[b.Position.ChainID].processBlock(b); err != nil {
		return err
	}
	return
}

// processBlock is the entry point to submit one block to a Consensus instance.
func (con *Consensus) processBlock(block *types.Block) (err error) {
	verifiedBlocks, deliveredBlocks, err := con.lattice.ProcessBlock(block)
	if err != nil {
		return
	}
	// Pass verified blocks (pass sanity check) back to BA module.
	for _, b := range verifiedBlocks {
		if err :=
			con.baModules[b.Position.ChainID].processBlock(b); err != nil {
			return err
		}
	}
	// Pass delivered blocks to compaction chain.
	for _, b := range deliveredBlocks {
		if err = con.ccModule.processBlock(b); err != nil {
			return
		}
		if err = con.db.Put(*b); err != nil {
			return
		}
		go con.event.NotifyTime(b.ConsensusTimestamp)
		con.nbModule.BlockDelivered(*b)
		// TODO(mission): Find a way to safely recycle the block.
		//                We should deliver block directly to
		//                nonBlocking and let them recycle the
		//                block.
	}
	if err = con.lattice.PurgeBlocks(deliveredBlocks); err != nil {
		return
	}
	return
}

// PrepareBlock would setup header fields of block based on its ProposerID.
func (con *Consensus) prepareBlock(b *types.Block,
	proposeTime time.Time) (err error) {
	if err = con.lattice.PrepareBlock(b, proposeTime); err != nil {
		return
	}
	// TODO(mission): decide CRS by block's round, which could be determined by
	//                block's info (ex. position, timestamp).
	if err = con.authModule.SignCRS(b, con.gov.CRS(0)); err != nil {
		return
	}
	return
}

// PrepareGenesisBlock would setup header fields for genesis block.
func (con *Consensus) PrepareGenesisBlock(b *types.Block,
	proposeTime time.Time) (err error) {
	if err = con.prepareBlock(b, proposeTime); err != nil {
		return
	}
	if len(b.Payload) != 0 {
		err = ErrGenesisBlockNotEmpty
		return
	}
	return
}
