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
	ErrIncorrectBlockRandomnessResult = fmt.Errorf(
		"incorrect block randomness result")
)

// consensusBAReceiver implements agreementReceiver.
type consensusBAReceiver struct {
	// TODO(mission): consensus would be replaced by lattice and network.
	consensus        *Consensus
	agreementModule  *agreement
	chainID          uint32
	changeNotaryTime time.Time
	round            uint64
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

func (recv *consensusBAReceiver) ProposeBlock() common.Hash {
	block := recv.consensus.proposeBlock(recv.chainID, recv.round)
	recv.consensus.baModules[recv.chainID].addCandidateBlock(block)
	if err := recv.consensus.preProcessBlock(block); err != nil {
		log.Println(err)
		return common.Hash{}
	}
	recv.consensus.network.BroadcastBlock(block)
	return block.Hash
}

func (recv *consensusBAReceiver) ConfirmBlock(
	hash common.Hash, votes map[types.NodeID]*types.Vote) {
	block, exist := recv.consensus.baModules[recv.chainID].
		findCandidateBlock(hash)
	if !exist {
		log.Println(ErrUnknownBlockConfirmed, hash)
		return
	}
	recv.consensus.ccModule.registerBlock(block)
	voteList := make([]types.Vote, 0, len(votes))
	for _, vote := range votes {
		voteList = append(voteList, *vote)
	}
	recv.consensus.network.BroadcastAgreementResult(&types.AgreementResult{
		BlockHash: hash,
		Position:  block.Position,
		Votes:     voteList,
	})
	if err := recv.consensus.processBlock(block); err != nil {
		log.Println(err)
		return
	}
	if block.Timestamp.After(recv.changeNotaryTime) {
		recv.round++
		recv.restartNotary <- true
	} else {
		recv.restartNotary <- false
	}
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
	recv.gov.AddDKGComplaint(complaint.Round, complaint)
}

// ProposeDKGMasterPublicKey propose a DKGMasterPublicKey.
func (recv *consensusDKGReceiver) ProposeDKGMasterPublicKey(
	mpk *types.DKGMasterPublicKey) {
	if err := recv.authModule.SignDKGMasterPublicKey(mpk); err != nil {
		log.Println(err)
		return
	}
	recv.gov.AddDKGMasterPublicKey(mpk.Round, mpk)
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

// ProposeDKGFinalize propose a DKGFinalize message.
func (recv *consensusDKGReceiver) ProposeDKGFinalize(final *types.DKGFinalize) {
	if err := recv.authModule.SignDKGFinalize(final); err != nil {
		log.Println(err)
		return
	}
	recv.gov.AddDKGFinalize(final.Round, final)
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
	dMoment      time.Time
	nodeSetCache *NodeSetCache
	round        uint64
	lock         sync.RWMutex
	ctx          context.Context
	ctxCancel    context.CancelFunc
	event        *common.Event
}

// NewConsensus construct an Consensus instance.
func NewConsensus(
	dMoment time.Time,
	app Application,
	gov Governance,
	db blockdb.BlockDatabase,
	network Network,
	prv crypto.PrivateKey) *Consensus {

	// TODO(w): load latest blockHeight from DB, and use config at that height.
	var (
		round uint64
	)
	config := gov.Configuration(round)
	nodeSetCache := NewNodeSetCache(gov)
	crs := gov.CRS(round)
	// Setup acking by information returned from Governace.
	nodes, err := nodeSetCache.GetNodeSet(round)
	if err != nil {
		panic(err)
	}
	// Setup auth module.
	authModule := NewAuthenticator(prv)
	// Check if the application implement Debug interface.
	debugApp, _ := app.(Debug)
	// Setup nonblocking module.
	nbModule := newNonBlocking(app, debugApp)
	// Init lattice.
	lattice := NewLattice(dMoment, config, authModule, nbModule, nbModule, db)
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
	// Construct Consensus instance.
	con := &Consensus{
		ID:            ID,
		currentConfig: config,
		ccModule:      newCompactionChain(),
		lattice:       lattice,
		nbModule:      nbModule,
		gov:           gov,
		db:            db,
		network:       network,
		tickerObj:     newTicker(gov, round, TickerBA),
		dkgReady:      sync.NewCond(&sync.Mutex{}),
		cfgModule:     cfgModule,
		dMoment:       dMoment,
		nodeSetCache:  nodeSetCache,
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
		recv.changeNotaryTime = dMoment
		con.baModules[chainID] = agreementModule
		con.receivers[chainID] = recv
	}
	return con
}

// Run starts running DEXON Consensus.
func (con *Consensus) Run() {
	// Setup context.
	con.ctx, con.ctxCancel = context.WithCancel(context.Background())
	go con.processMsg(con.network.ReceiveChan())
	con.cfgModule.registerDKG(con.round, int(con.currentConfig.DKGSetSize)/3+1)
	con.event.RegisterTime(con.dMoment.Add(con.currentConfig.RoundInterval/4),
		func(time.Time) {
			con.runDKGTSIG(con.round)
		})
	round1 := uint64(1)
	con.lattice.AppendConfig(round1, con.gov.Configuration(round1))
	con.initialRound(con.dMoment)
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
		select {
		case newNotary := <-recv.restartNotary:
			if newNotary {
				recv.changeNotaryTime =
					recv.changeNotaryTime.Add(con.currentConfig.RoundInterval)
				nodes, err := con.nodeSetCache.GetNodeSet(recv.round)
				if err != nil {
					panic(err)
				}
				nIDs = nodes.GetSubSet(
					int(con.gov.Configuration(recv.round).NotarySetSize),
					types.NewNotarySetTarget(con.gov.CRS(recv.round), chainID))
			}
			nextPos := con.lattice.NextPosition(chainID)
			nextPos.Round = recv.round
			agreement.restart(nIDs, nextPos)
		default:
		}
		err := agreement.nextState()
		if err != nil {
			log.Printf("[%s] %s\n", con.ID.String(), err)
			break BALoop
		}
		for i := 0; i < agreement.clocks(); i++ {
			// Priority select for agreement.done().
			select {
			case <-agreement.done():
				continue BALoop
			default:
			}
			select {
			case <-agreement.done():
				continue BALoop
			case <-tick:
			}
		}
	}
}

// runDKGTSIG starts running DKG+TSIG protocol.
func (con *Consensus) runDKGTSIG(round uint64) {
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
			con.gov.ProposeCRS(crs)
		}
	}
}

func (con *Consensus) initialRound(startTime time.Time) {
	select {
	case <-con.ctx.Done():
		return
	default:
	}
	con.currentConfig = con.gov.Configuration(con.round)

	con.event.RegisterTime(startTime.Add(con.currentConfig.RoundInterval/2),
		func(time.Time) {
			go con.runCRS()
		})
	con.event.RegisterTime(startTime.Add(con.currentConfig.RoundInterval/2),
		func(time.Time) {
			con.cfgModule.registerDKG(
				con.round+1, int(con.currentConfig.DKGSetSize/3)+1)
		})
	con.event.RegisterTime(startTime.Add(con.currentConfig.RoundInterval*2/3),
		func(time.Time) {
			func() {
				con.dkgReady.L.Lock()
				defer con.dkgReady.L.Unlock()
				con.dkgRunning = 0
			}()
			con.runDKGTSIG(con.round + 1)
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
	for _, a := range con.baModules {
		a.stop()
	}
	con.event.Reset()
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

func (con *Consensus) proposeBlock(chainID uint32, round uint64) *types.Block {
	block := &types.Block{
		Position: types.Position{
			ChainID: chainID,
			Round:   round,
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
	if rand.Position.Round == 0 {
		return nil
	}
	if !con.ccModule.blockRegistered(rand.BlockHash) {
		return nil
	}
	if DiffUint64(con.round, rand.Position.Round) > 1 {
		return nil
	}
	if len(rand.Votes) <= int(con.currentConfig.NotarySetSize/3*2) {
		return ErrNotEnoughVotes
	}
	if rand.Position.ChainID >= con.currentConfig.NumChains {
		return ErrIncorrectAgreementResultPosition
	}
	notarySet, err := con.nodeSetCache.GetNotarySet(
		rand.Position.Round, rand.Position.ChainID)
	if err != nil {
		return err
	}
	for _, vote := range rand.Votes {
		if _, exist := notarySet[vote.ProposerID]; !exist {
			return ErrIncorrectVoteProposer
		}
		ok, err := verifyVoteSignature(&vote)
		if err != nil {
			return err
		}
		if !ok {
			return ErrIncorrectVoteSignature
		}
	}
	// Sanity check done.
	if !con.cfgModule.touchTSigHash(rand.BlockHash) {
		return nil
	}
	con.network.BroadcastAgreementResult(rand)
	dkgSet, err := con.nodeSetCache.GetDKGSet(rand.Position.Round)
	if err != nil {
		return err
	}
	if _, exist := dkgSet[con.ID]; !exist {
		return nil
	}
	psig, err := con.cfgModule.preparePartialSignature(rand.Position.Round, rand.BlockHash)
	if err != nil {
		return err
	}
	if err = con.authModule.SignDKGPartialSignature(psig); err != nil {
		return err
	}
	if err = con.cfgModule.processPartialSignature(psig); err != nil {
		return err
	}
	con.network.BroadcastDKGPartialSignature(psig)
	go func() {
		tsig, err := con.cfgModule.runTSig(rand.Position.Round, rand.BlockHash)
		if err != nil {
			if err != ErrTSigAlreadyRunning {
				log.Println(err)
			}
			return
		}
		result := &types.BlockRandomnessResult{
			BlockHash:  rand.BlockHash,
			Position:   rand.Position,
			Randomness: tsig.Signature,
		}
		if err := con.ProcessBlockRandomnessResult(result); err != nil {
			log.Println(err)
			return
		}
		con.network.BroadcastRandomnessResult(result)
	}()
	return nil
}

// ProcessBlockRandomnessResult processes the randomness result.
func (con *Consensus) ProcessBlockRandomnessResult(
	rand *types.BlockRandomnessResult) error {
	if rand.Position.Round == 0 {
		return nil
	}
	if !con.ccModule.blockRegistered(rand.BlockHash) {
		return nil
	}
	// TODO(jimmy-dexon): reuse the GPK.
	round := rand.Position.Round
	gpk, err := NewDKGGroupPublicKey(round,
		con.gov.DKGMasterPublicKeys(round),
		con.gov.DKGComplaints(round),
		int(con.gov.Configuration(round).DKGSetSize/3)+1)
	if err != nil {
		return err
	}
	if !gpk.VerifySignature(
		rand.BlockHash, crypto.Signature{Signature: rand.Randomness}) {
		return ErrIncorrectBlockRandomnessResult
	}
	con.network.BroadcastRandomnessResult(rand)
	if err := con.ccModule.processBlockRandomnessResult(rand); err != nil {
		if err != ErrBlockNotRegistered {
			return err
		}
	}
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
		go con.event.NotifyTime(b.Finalization.Timestamp)
	}
	deliveredBlocks = con.ccModule.extractBlocks()
	for _, b := range deliveredBlocks {
		if err = con.db.Put(*b); err != nil {
			return
		}
		if b.Position.Round > con.round {
			con.round++
			con.lattice.AppendConfig(con.round+1, con.gov.Configuration(con.round+1))
		}
		// TODO(mission): clone types.FinalizationResult
		con.nbModule.BlockDelivered(b.Hash, b.Finalization)
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
