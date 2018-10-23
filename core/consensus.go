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
	"encoding/hex"
	"fmt"
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
		recv.consensus.logger.Error("Failed to prepare vote", "error", err)
		return
	}
	go func() {
		if err := recv.agreementModule.processVote(vote); err != nil {
			recv.consensus.logger.Error("Failed to process vote", "error", err)
			return
		}
		recv.consensus.logger.Debug("Calling Network.BroadcastVote",
			"vote", vote)
		recv.consensus.network.BroadcastVote(vote)
	}()
}

func (recv *consensusBAReceiver) ProposeBlock() common.Hash {
	block := recv.consensus.proposeBlock(recv.chainID, recv.round)
	recv.consensus.baModules[recv.chainID].addCandidateBlock(block)
	if err := recv.consensus.preProcessBlock(block); err != nil {
		recv.consensus.logger.Error("Failed to pre-process block", "error", err)
		return common.Hash{}
	}
	recv.consensus.logger.Debug("Calling Network.BroadcastBlock", "block", block)
	recv.consensus.network.BroadcastBlock(block)
	return block.Hash
}

func (recv *consensusBAReceiver) ConfirmBlock(
	hash common.Hash, votes map[types.NodeID]*types.Vote) {
	var block *types.Block
	if (hash == common.Hash{}) {
		recv.consensus.logger.Info("Empty block is confirmed",
			"position", recv.agreementModule.agreementID())
		var err error
		block, err = recv.consensus.proposeEmptyBlock(recv.chainID)
		if err != nil {
			recv.consensus.logger.Error("Propose empty block failed", "error", err)
			return
		}
	} else {
		var exist bool
		block, exist = recv.consensus.baModules[recv.chainID].
			findCandidateBlock(hash)
		if !exist {
			recv.consensus.logger.Error("Unknown block confirmed", "hash", hash)
			return
		}
	}
	recv.consensus.ccModule.registerBlock(block)
	voteList := make([]types.Vote, 0, len(votes))
	for _, vote := range votes {
		if vote.BlockHash != hash {
			continue
		}
		voteList = append(voteList, *vote)
	}
	result := &types.AgreementResult{
		BlockHash: block.Hash,
		Position:  block.Position,
		Votes:     voteList,
	}
	recv.consensus.logger.Debug("Calling Network.BroadcastAgreementResult",
		"result", result)
	recv.consensus.network.BroadcastAgreementResult(result)
	if err := recv.consensus.processBlock(block); err != nil {
		recv.consensus.logger.Error("Failed to process block", "error", err)
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
	cfgModule    *configurationChain
	network      Network
	logger       common.Logger
}

// ProposeDKGComplaint proposes a DKGComplaint.
func (recv *consensusDKGReceiver) ProposeDKGComplaint(
	complaint *types.DKGComplaint) {
	if err := recv.authModule.SignDKGComplaint(complaint); err != nil {
		recv.logger.Error("Failed to sign DKG complaint", "error", err)
		return
	}
	recv.logger.Debug("Calling Governace.AddDKGComplaint",
		"complaint", complaint)
	recv.gov.AddDKGComplaint(complaint.Round, complaint)
}

// ProposeDKGMasterPublicKey propose a DKGMasterPublicKey.
func (recv *consensusDKGReceiver) ProposeDKGMasterPublicKey(
	mpk *types.DKGMasterPublicKey) {
	if err := recv.authModule.SignDKGMasterPublicKey(mpk); err != nil {
		recv.logger.Error("Failed to sign DKG master public key", "error", err)
		return
	}
	recv.logger.Debug("Calling Governance.AddDKGMasterPublicKey", "key", mpk)
	recv.gov.AddDKGMasterPublicKey(mpk.Round, mpk)
}

// ProposeDKGPrivateShare propose a DKGPrivateShare.
func (recv *consensusDKGReceiver) ProposeDKGPrivateShare(
	prv *types.DKGPrivateShare) {
	if err := recv.authModule.SignDKGPrivateShare(prv); err != nil {
		recv.logger.Error("Failed to sign DKG private share", "error", err)
		return
	}
	receiverPubKey, exists := recv.nodeSetCache.GetPublicKey(prv.ReceiverID)
	if !exists {
		recv.logger.Error("Public key for receiver not found",
			"receiver", prv.ReceiverID.String()[:6])
		return
	}
	if prv.ReceiverID == recv.ID {
		go func() {
			if err := recv.cfgModule.processPrivateShare(prv); err != nil {
				recv.logger.Error("Failed to process self private share", "prvShare", prv)
			}
		}()
	} else {
		recv.logger.Debug("Calling Network.SendDKGPrivateShare",
			"receiver", hex.EncodeToString(receiverPubKey.Bytes()))
		recv.network.SendDKGPrivateShare(receiverPubKey, prv)
	}
}

// ProposeDKGAntiNackComplaint propose a DKGPrivateShare as an anti complaint.
func (recv *consensusDKGReceiver) ProposeDKGAntiNackComplaint(
	prv *types.DKGPrivateShare) {
	if prv.ProposerID == recv.ID {
		if err := recv.authModule.SignDKGPrivateShare(prv); err != nil {
			recv.logger.Error("Failed sign DKG private share", "error", err)
			return
		}
	}
	recv.logger.Debug("Calling Network.BroadcastDKGPrivateShare", "share", prv)
	recv.network.BroadcastDKGPrivateShare(prv)
}

// ProposeDKGFinalize propose a DKGFinalize message.
func (recv *consensusDKGReceiver) ProposeDKGFinalize(final *types.DKGFinalize) {
	if err := recv.authModule.SignDKGFinalize(final); err != nil {
		recv.logger.Error("Faield to sign DKG finalize", "error", err)
		return
	}
	recv.logger.Debug("Calling Governance.AddDKGFinalize", "final", final)
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
	dMoment       time.Time
	nodeSetCache  *NodeSetCache
	round         uint64
	roundToNotify uint64
	lock          sync.RWMutex
	ctx           context.Context
	ctxCancel     context.CancelFunc
	event         *common.Event
	logger        common.Logger
}

// NewConsensus construct an Consensus instance.
func NewConsensus(
	dMoment time.Time,
	app Application,
	gov Governance,
	db blockdb.BlockDatabase,
	network Network,
	prv crypto.PrivateKey,
	logger common.Logger) *Consensus {

	// TODO(w): load latest blockHeight from DB, and use config at that height.
	var (
		round uint64
		// round 0 and 1 are decided at beginning.
		roundToNotify = round + 2
	)
	logger.Debug("Calling Governance.Configuration", "round", round)
	config := gov.Configuration(round)
	nodeSetCache := NewNodeSetCache(gov)
	logger.Debug("Calling Governance.CRS", "round", round)
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
	lattice := NewLattice(
		dMoment, config, authModule, nbModule, nbModule, db, logger)
	// Init configuration chain.
	ID := types.NewNodeID(prv.PublicKey())
	recv := &consensusDKGReceiver{
		ID:           ID,
		gov:          gov,
		authModule:   authModule,
		nodeSetCache: nodeSetCache,
		network:      network,
		logger:       logger,
	}
	cfgModule := newConfigurationChain(
		ID,
		recv,
		gov,
		logger)
	recv.cfgModule = cfgModule
	// Construct Consensus instance.
	con := &Consensus{
		ID:            ID,
		currentConfig: config,
		ccModule:      newCompactionChain(gov),
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
		logger:        logger,
		roundToNotify: roundToNotify,
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
func (con *Consensus) Run(initBlock *types.Block) {
	// Setup context.
	con.ctx, con.ctxCancel = context.WithCancel(context.Background())
	con.ccModule.init(initBlock)
	// TODO(jimmy-dexon): change AppendConfig to add config for specific round.
	for i := uint64(0); i < initBlock.Position.Round; i++ {
		con.logger.Debug("Calling Governance.Configuration", "round", i+1)
		cfg := con.gov.Configuration(i + 1)
		if err := con.lattice.AppendConfig(i+1, cfg); err != nil {
			panic(err)
		}
	}
	con.logger.Debug("Calling Network.ReceiveChan")
	go con.processMsg(con.network.ReceiveChan())
	// Sleep until dMoment come.
	time.Sleep(con.dMoment.Sub(time.Now().UTC()))
	con.cfgModule.registerDKG(con.round, int(con.currentConfig.DKGSetSize)/3+1)
	con.event.RegisterTime(con.dMoment.Add(con.currentConfig.RoundInterval/4),
		func(time.Time) {
			con.runDKGTSIG(con.round)
		})
	round1 := uint64(1)
	con.logger.Debug("Calling Governance.Configuration", "round", round1)
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
				con.logger.Debug("Calling Governance.Configuration",
					"round", recv.round)
				con.logger.Debug("Calling Governance.CRS", "round", recv.round)
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
			con.logger.Error("Failed to proceed to next state",
				"nodeID", con.ID.String(),
				"error", err)
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
				con.logger.Warn("Your computer cannot finish DKG on time!",
					"nodeID", con.ID.String())
			}
		}()
		if err := con.cfgModule.runDKG(round); err != nil {
			panic(err)
		}
		nodes, err := con.nodeSetCache.GetNodeSet(round)
		if err != nil {
			panic(err)
		}
		con.logger.Debug("Calling Governance.Configuration", "round", round)
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
		con.logger.Debug("Calling Network.BroadcastDKGPartialSignature",
			"proposer", psig.ProposerID,
			"round", psig.Round,
			"hash", psig.Hash)
		con.network.BroadcastDKGPartialSignature(psig)
		if _, err = con.cfgModule.runBlockTSig(round, hash); err != nil {
			panic(err)
		}
	}()
}

func (con *Consensus) runCRS() {
	// Start running next round CRS.
	con.logger.Debug("Calling Governance.CRS", "round", con.round)
	psig, err := con.cfgModule.preparePartialSignature(
		con.round, con.gov.CRS(con.round))
	if err != nil {
		con.logger.Error("Failed to prepare partial signature", "error", err)
	} else if err = con.authModule.SignDKGPartialSignature(psig); err != nil {
		con.logger.Error("Failed to sign DKG partial signature", "error", err)
	} else if err = con.cfgModule.processPartialSignature(psig); err != nil {
		con.logger.Error("Failed to process partial signature", "error", err)
	} else {
		con.logger.Debug("Calling Network.BroadcastDKGPartialSignature",
			"proposer", psig.ProposerID,
			"round", psig.Round,
			"hash", psig.Hash)
		con.network.BroadcastDKGPartialSignature(psig)
		con.logger.Debug("Calling Governance.CRS", "round", con.round)
		crs, err := con.cfgModule.runCRSTSig(con.round, con.gov.CRS(con.round))
		if err != nil {
			con.logger.Error("Failed to run CRS Tsig", "error", err)
		} else {
			con.logger.Debug("Calling Governance.ProposeCRS",
				"round", con.round+1,
				"crs", crs)
			con.gov.ProposeCRS(con.round+1, crs)
		}
	}
}

func (con *Consensus) initialRound(startTime time.Time) {
	select {
	case <-con.ctx.Done():
		return
	default:
	}
	con.logger.Debug("Calling Governance.Configuration", "round", con.round)
	con.currentConfig = con.gov.Configuration(con.round)

	con.event.RegisterTime(startTime.Add(con.currentConfig.RoundInterval/2),
		func(time.Time) {
			go func() {
				con.runCRS()
				ticker := newTicker(con.gov, con.round, TickerDKG)
				<-ticker.Tick()
				// Normally, gov.CRS would return non-nil. Use this for in case of
				// unexpected network fluctuation and ensure the robustness.
				for (con.gov.CRS(con.round+1) == common.Hash{}) {
					con.logger.Info("CRS is not ready yet. Try again later...",
						"nodeID", con.ID)
					time.Sleep(500 * time.Millisecond)
				}
				con.cfgModule.registerDKG(
					con.round+1, int(con.currentConfig.DKGSetSize/3)+1)
			}()
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
			con.logger.Debug("Calling Governance.Configuration",
				"round", con.round+1)
			con.lattice.AppendConfig(con.round+1, con.gov.Configuration(con.round+1))
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
			// For sync mode.
			if val.IsFinalized() {
				if err := con.processFinalizedBlock(val); err != nil {
					con.logger.Error("Failed to process finalized block",
						"error", err)
				}
			} else {
				if err := con.preProcessBlock(val); err != nil {
					con.logger.Error("Failed to pre process block",
						"error", err)
				}
			}
		case *types.Vote:
			if err := con.ProcessVote(val); err != nil {
				con.logger.Error("Failed to process vote",
					"error", err)
			}
		case *types.AgreementResult:
			if err := con.ProcessAgreementResult(val); err != nil {
				con.logger.Error("Failed to process agreement result",
					"error", err)
			}
		case *types.BlockRandomnessResult:
			if err := con.ProcessBlockRandomnessResult(val); err != nil {
				con.logger.Error("Failed to process block randomness result",
					"error", err)
			}
		case *types.DKGPrivateShare:
			if err := con.cfgModule.processPrivateShare(val); err != nil {
				con.logger.Error("Failed to process private share",
					"error", err)
			}

		case *types.DKGPartialSignature:
			if err := con.cfgModule.processPartialSignature(val); err != nil {
				con.logger.Error("Failed to process partial signature",
					"error", err)
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
		con.logger.Error("Failed to prepare block", "error", err)
		return nil
	}
	return block
}

func (con *Consensus) proposeEmptyBlock(
	chainID uint32) (*types.Block, error) {
	block := &types.Block{
		Position: types.Position{
			ChainID: chainID,
		},
	}
	if err := con.lattice.PrepareEmptyBlock(block); err != nil {
		return nil, err
	}
	return block, nil
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
	con.logger.Debug("Calling Network.BroadcastAgreementResult", "result", rand)
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
	con.logger.Debug("Calling Network.BroadcastDKGPartialSignature",
		"proposer", psig.ProposerID,
		"round", psig.Round,
		"hash", psig.Hash)
	con.network.BroadcastDKGPartialSignature(psig)
	go func() {
		tsig, err := con.cfgModule.runTSig(rand.Position.Round, rand.BlockHash)
		if err != nil {
			if err != ErrTSigAlreadyRunning {
				con.logger.Error("Faield to run TSIG", "error", err)
			}
			return
		}
		result := &types.BlockRandomnessResult{
			BlockHash:  rand.BlockHash,
			Position:   rand.Position,
			Randomness: tsig.Signature,
		}
		if err := con.ProcessBlockRandomnessResult(result); err != nil {
			con.logger.Error("Failed to process randomness result",
				"error", err)
			return
		}
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
	round := rand.Position.Round
	v, ok, err := con.ccModule.tsigVerifier.UpdateAndGet(round)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if !v.VerifySignature(
		rand.BlockHash, crypto.Signature{Signature: rand.Randomness}) {
		return ErrIncorrectBlockRandomnessResult
	}
	con.logger.Debug("Calling Network.BroadcastRandomnessResult",
		"hash", rand.BlockHash,
		"position", rand.Position,
		"randomness", hex.EncodeToString(rand.Randomness))
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
	if err = con.lattice.SanityCheck(b, true); err != nil {
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
		// TODO(mission): clone types.FinalizationResult
		con.nbModule.BlockDelivered(b.Hash, b.Finalization)
	}
	if err = con.lattice.PurgeBlocks(deliveredBlocks); err != nil {
		return
	}
	return
}

// processFinalizedBlock is the entry point for syncing blocks.
func (con *Consensus) processFinalizedBlock(block *types.Block) (err error) {
	if err = con.lattice.SanityCheck(block, false); err != nil {
		return
	}
	con.ccModule.processFinalizedBlock(block)
	for {
		confirmed := con.ccModule.extractFinalizedBlocks()
		if len(confirmed) == 0 {
			break
		}
		if err = con.lattice.ctModule.processBlocks(confirmed); err != nil {
			return
		}
		for _, b := range confirmed {
			if err = con.db.Put(*b); err != nil {
				if err != blockdb.ErrBlockExists {
					return
				}
				err = nil
			}
			con.nbModule.BlockDelivered(b.Hash, b.Finalization)
			if b.Position.Round+2 == con.roundToNotify {
				// Only the first block delivered of that round would
				// trigger this noitification.
				con.gov.NotifyRoundHeight(
					con.roundToNotify, b.Finalization.Height)
				con.roundToNotify++
			}
		}
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
	con.logger.Debug("Calling Governance.CRS", "round", 0)
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
