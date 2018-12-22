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
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
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
	ErrIncorrectVoteBlockHash = fmt.Errorf(
		"incorrect vote block hash")
	ErrIncorrectVoteType = fmt.Errorf(
		"incorrect vote type")
	ErrIncorrectVotePosition = fmt.Errorf(
		"incorrect vote position")
	ErrIncorrectVoteProposer = fmt.Errorf(
		"incorrect vote proposer")
	ErrCRSNotReady = fmt.Errorf(
		"CRS not ready")
	ErrConfigurationNotReady = fmt.Errorf(
		"Configuration not ready")
)

// consensusBAReceiver implements agreementReceiver.
type consensusBAReceiver struct {
	// TODO(mission): consensus would be replaced by lattice and network.
	consensus        *Consensus
	agreementModule  *agreement
	chainID          uint32
	changeNotaryTime time.Time
	round            uint64
	isNotary         bool
	restartNotary    chan types.Position
}

func (recv *consensusBAReceiver) ProposeVote(vote *types.Vote) {
	if !recv.isNotary {
		return
	}
	if err := recv.agreementModule.prepareVote(vote); err != nil {
		recv.consensus.logger.Error("Failed to prepare vote", "error", err)
		return
	}
	go func() {
		if err := recv.agreementModule.processVote(vote); err != nil {
			recv.consensus.logger.Error("Failed to process self vote",
				"error", err,
				"vote", vote)
			return
		}
		recv.consensus.logger.Debug("Calling Network.BroadcastVote",
			"vote", vote)
		recv.consensus.network.BroadcastVote(vote)
	}()
}

func (recv *consensusBAReceiver) ProposeBlock() common.Hash {
	if !recv.isNotary {
		return common.Hash{}
	}
	block := recv.consensus.proposeBlock(recv.chainID, recv.round)
	if block == nil {
		recv.consensus.logger.Error("unable to propose block")
		return nullBlockHash
	}
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
	isEmptyBlockConfirmed := hash == common.Hash{}
	if isEmptyBlockConfirmed {
		aID := recv.agreementModule.agreementID()
		recv.consensus.logger.Info("Empty block is confirmed",
			"position", &aID)
		var err error
		block, err = recv.consensus.proposeEmptyBlock(recv.round, recv.chainID)
		if err != nil {
			recv.consensus.logger.Error("Propose empty block failed", "error", err)
			return
		}
	} else {
		var exist bool
		block, exist = recv.agreementModule.findCandidateBlockNoLock(hash)
		if !exist {
			recv.consensus.logger.Error("Unknown block confirmed",
				"hash", hash.String()[:6],
				"chainID", recv.chainID)
			ch := make(chan *types.Block)
			func() {
				recv.consensus.lock.Lock()
				defer recv.consensus.lock.Unlock()
				recv.consensus.baConfirmedBlock[hash] = ch
			}()
			recv.consensus.network.PullBlocks(common.Hashes{hash})
			go func() {
				block = <-ch
				recv.consensus.logger.Info("Receive unknown block",
					"hash", hash.String()[:6],
					"position", &block.Position,
					"chainID", recv.chainID)
				recv.agreementModule.addCandidateBlock(block)
				recv.agreementModule.lock.Lock()
				defer recv.agreementModule.lock.Unlock()
				recv.ConfirmBlock(block.Hash, votes)
			}()
			return
		}
	}
	recv.consensus.ccModule.registerBlock(block)
	if block.Position.Height != 0 &&
		!recv.consensus.lattice.Exist(block.ParentHash) {
		go func(hash common.Hash) {
			parentHash := hash
			for {
				recv.consensus.logger.Warn("Parent block not confirmed",
					"parent-hash", parentHash.String()[:6],
					"cur-position", &block.Position)
				ch := make(chan *types.Block)
				if !func() bool {
					recv.consensus.lock.Lock()
					defer recv.consensus.lock.Unlock()
					if _, exist := recv.consensus.baConfirmedBlock[parentHash]; exist {
						return false
					}
					recv.consensus.baConfirmedBlock[parentHash] = ch
					return true
				}() {
					return
				}
				var block *types.Block
			PullBlockLoop:
				for {
					recv.consensus.logger.Debug("Calling Network.PullBlock for parent",
						"hash", parentHash)
					recv.consensus.network.PullBlocks(common.Hashes{parentHash})
					select {
					case block = <-ch:
						break PullBlockLoop
					case <-time.After(1 * time.Second):
					}
				}
				recv.consensus.logger.Info("Receive parent block",
					"parent-hash", block.ParentHash.String()[:6],
					"cur-position", &block.Position,
					"chainID", recv.chainID)
				recv.consensus.ccModule.registerBlock(block)
				if err := recv.consensus.processBlock(block); err != nil {
					recv.consensus.logger.Error("Failed to process block",
						"block", block,
						"error", err)
					return
				}
				parentHash = block.ParentHash
				if block.Position.Height == 0 ||
					recv.consensus.lattice.Exist(parentHash) {
					return
				}
			}
		}(block.ParentHash)
	}
	if recv.isNotary {
		voteList := make([]types.Vote, 0, len(votes))
		for _, vote := range votes {
			if vote.BlockHash != hash {
				continue
			}
			voteList = append(voteList, *vote)
		}
		result := &types.AgreementResult{
			BlockHash:    block.Hash,
			Position:     block.Position,
			Votes:        voteList,
			IsEmptyBlock: isEmptyBlockConfirmed,
		}
		recv.consensus.logger.Debug("Propose AgreementResult",
			"result", result)
		recv.consensus.network.BroadcastAgreementResult(result)
	}
	if err := recv.consensus.processBlock(block); err != nil {
		recv.consensus.logger.Error("Failed to process block",
			"block", block,
			"error", err)
		return
	}
	// Clean the restartNotary channel so BA will not stuck by deadlock.
CleanChannelLoop:
	for {
		select {
		case <-recv.restartNotary:
		default:
			break CleanChannelLoop
		}
	}
	newPos := block.Position
	if block.Timestamp.After(recv.changeNotaryTime) {
		recv.round++
		newPos.Round++
	}
	recv.restartNotary <- newPos
}

func (recv *consensusBAReceiver) PullBlocks(hashes common.Hashes) {
	if !recv.isNotary {
		return
	}
	recv.consensus.logger.Debug("Calling Network.PullBlocks", "hashes", hashes)
	recv.consensus.network.PullBlocks(hashes)
}

// consensusDKGReceiver implements dkgReceiver.
type consensusDKGReceiver struct {
	ID           types.NodeID
	gov          Governance
	signer       *utils.Signer
	nodeSetCache *utils.NodeSetCache
	cfgModule    *configurationChain
	network      Network
	logger       common.Logger
}

// ProposeDKGComplaint proposes a DKGComplaint.
func (recv *consensusDKGReceiver) ProposeDKGComplaint(
	complaint *typesDKG.Complaint) {
	if err := recv.signer.SignDKGComplaint(complaint); err != nil {
		recv.logger.Error("Failed to sign DKG complaint", "error", err)
		return
	}
	recv.logger.Debug("Calling Governace.AddDKGComplaint",
		"complaint", complaint)
	recv.gov.AddDKGComplaint(complaint.Round, complaint)
}

// ProposeDKGMasterPublicKey propose a DKGMasterPublicKey.
func (recv *consensusDKGReceiver) ProposeDKGMasterPublicKey(
	mpk *typesDKG.MasterPublicKey) {
	if err := recv.signer.SignDKGMasterPublicKey(mpk); err != nil {
		recv.logger.Error("Failed to sign DKG master public key", "error", err)
		return
	}
	recv.logger.Debug("Calling Governance.AddDKGMasterPublicKey", "key", mpk)
	recv.gov.AddDKGMasterPublicKey(mpk.Round, mpk)
}

// ProposeDKGPrivateShare propose a DKGPrivateShare.
func (recv *consensusDKGReceiver) ProposeDKGPrivateShare(
	prv *typesDKG.PrivateShare) {
	if err := recv.signer.SignDKGPrivateShare(prv); err != nil {
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
	prv *typesDKG.PrivateShare) {
	if prv.ProposerID == recv.ID {
		if err := recv.signer.SignDKGPrivateShare(prv); err != nil {
			recv.logger.Error("Failed sign DKG private share", "error", err)
			return
		}
	}
	recv.logger.Debug("Calling Network.BroadcastDKGPrivateShare", "share", prv)
	recv.network.BroadcastDKGPrivateShare(prv)
}

// ProposeDKGMPKReady propose a DKGMPKReady message.
func (recv *consensusDKGReceiver) ProposeDKGMPKReady(ready *typesDKG.MPKReady) {
	if err := recv.signer.SignDKGMPKReady(ready); err != nil {
		recv.logger.Error("Failed to sign DKG ready", "error", err)
		return
	}
	recv.logger.Debug("Calling Governance.AddDKGMPKReady", "ready", ready)
	recv.gov.AddDKGMPKReady(ready.Round, ready)
}

// ProposeDKGFinalize propose a DKGFinalize message.
func (recv *consensusDKGReceiver) ProposeDKGFinalize(final *typesDKG.Finalize) {
	if err := recv.signer.SignDKGFinalize(final); err != nil {
		recv.logger.Error("Failed to sign DKG finalize", "error", err)
		return
	}
	recv.logger.Debug("Calling Governance.AddDKGFinalize", "final", final)
	recv.gov.AddDKGFinalize(final.Round, final)
}

// Consensus implements DEXON Consensus algorithm.
type Consensus struct {
	// Node Info.
	ID     types.NodeID
	signer *utils.Signer

	// BA.
	baMgr            *agreementMgr
	baConfirmedBlock map[common.Hash]chan<- *types.Block

	// DKG.
	dkgRunning int32
	dkgReady   *sync.Cond
	cfgModule  *configurationChain

	// Dexon consensus v1's modules.
	lattice  *Lattice
	ccModule *compactionChain
	toSyncer *totalOrderingSyncer

	// Interfaces.
	db       db.Database
	app      Application
	debugApp Debug
	gov      Governance
	network  Network

	// Misc.
	dMoment                    time.Time
	nodeSetCache               *utils.NodeSetCache
	round                      uint64
	roundToNotify              uint64
	lock                       sync.RWMutex
	ctx                        context.Context
	ctxCancel                  context.CancelFunc
	event                      *common.Event
	logger                     common.Logger
	nonFinalizedBlockDelivered bool
}

// NewConsensus construct an Consensus instance.
func NewConsensus(
	dMoment time.Time,
	app Application,
	gov Governance,
	db db.Database,
	network Network,
	prv crypto.PrivateKey,
	logger common.Logger) *Consensus {

	// TODO(w): load latest blockHeight from DB, and use config at that height.
	nodeSetCache := utils.NewNodeSetCache(gov)
	// Setup signer module.
	signer := utils.NewSigner(prv)
	// Check if the application implement Debug interface.
	var debugApp Debug
	if a, ok := app.(Debug); ok {
		debugApp = a
	}
	// Get configuration for genesis round.
	var round uint64
	config := utils.GetConfigWithPanic(gov, round, logger)
	// Init lattice.
	lattice := NewLattice(
		dMoment, round, config, signer, app, debugApp, db, logger)
	// Init configuration chain.
	ID := types.NewNodeID(prv.PublicKey())
	recv := &consensusDKGReceiver{
		ID:           ID,
		gov:          gov,
		signer:       signer,
		nodeSetCache: nodeSetCache,
		network:      network,
		logger:       logger,
	}
	cfgModule := newConfigurationChain(
		ID,
		recv,
		gov,
		nodeSetCache,
		db,
		logger)
	recv.cfgModule = cfgModule
	// Construct Consensus instance.
	con := &Consensus{
		ID:               ID,
		ccModule:         newCompactionChain(gov),
		lattice:          lattice,
		app:              newNonBlocking(app, debugApp),
		debugApp:         debugApp,
		gov:              gov,
		db:               db,
		network:          network,
		baConfirmedBlock: make(map[common.Hash]chan<- *types.Block),
		dkgReady:         sync.NewCond(&sync.Mutex{}),
		cfgModule:        cfgModule,
		dMoment:          dMoment,
		nodeSetCache:     nodeSetCache,
		signer:           signer,
		event:            common.NewEvent(),
		logger:           logger,
	}
	con.ctx, con.ctxCancel = context.WithCancel(context.Background())
	con.baMgr = newAgreementMgr(con, round, dMoment)
	if err := con.prepare(&types.Block{}); err != nil {
		panic(err)
	}
	return con
}

// NewConsensusFromSyncer constructs an Consensus instance from information
// provided from syncer.
//
// You need to provide the initial block for this newly created Consensus
// instance to bootstrap with. A proper choice is the last finalized block you
// delivered to syncer.
func NewConsensusFromSyncer(
	initBlock *types.Block,
	initRoundBeginTime time.Time,
	app Application,
	gov Governance,
	db db.Database,
	networkModule Network,
	prv crypto.PrivateKey,
	latticeModule *Lattice,
	blocks []*types.Block,
	randomnessResults []*types.BlockRandomnessResult,
	logger common.Logger) (*Consensus, error) {
	// Setup the cache for node sets.
	nodeSetCache := utils.NewNodeSetCache(gov)
	// Setup signer module.
	signer := utils.NewSigner(prv)
	// Init configuration chain.
	ID := types.NewNodeID(prv.PublicKey())
	recv := &consensusDKGReceiver{
		ID:           ID,
		gov:          gov,
		signer:       signer,
		nodeSetCache: nodeSetCache,
		network:      networkModule,
		logger:       logger,
	}
	cfgModule := newConfigurationChain(
		ID,
		recv,
		gov,
		nodeSetCache,
		db,
		logger)
	recv.cfgModule = cfgModule
	// Check if the application implement Debug interface.
	var debugApp Debug
	if a, ok := app.(Debug); ok {
		debugApp = a
	}
	// Setup Consensus instance.
	con := &Consensus{
		ID:               ID,
		ccModule:         newCompactionChain(gov),
		lattice:          latticeModule,
		app:              newNonBlocking(app, debugApp),
		gov:              gov,
		db:               db,
		network:          networkModule,
		baConfirmedBlock: make(map[common.Hash]chan<- *types.Block),
		dkgReady:         sync.NewCond(&sync.Mutex{}),
		cfgModule:        cfgModule,
		dMoment:          initRoundBeginTime,
		nodeSetCache:     nodeSetCache,
		signer:           signer,
		event:            common.NewEvent(),
		logger:           logger,
	}
	con.ctx, con.ctxCancel = context.WithCancel(context.Background())
	con.baMgr = newAgreementMgr(con, initBlock.Position.Round, initRoundBeginTime)
	// Bootstrap the consensus instance.
	if err := con.prepare(initBlock); err != nil {
		return nil, err
	}
	// Dump all BA-confirmed blocks to the consensus instance.
	for _, b := range blocks {
		con.ccModule.registerBlock(b)
		if err := con.processBlock(b); err != nil {
			return nil, err
		}
	}
	// Dump all randomness result to the consensus instance.
	for _, r := range randomnessResults {
		if err := con.ProcessBlockRandomnessResult(r, false); err != nil {
			con.logger.Error("failed to process randomness result when syncing",
				"result", r)
			continue
		}
	}
	return con, nil
}

// prepare the Consensus instance to be ready for blocks after 'initBlock'.
// 'initBlock' could be either:
//  - an empty block
//  - the last finalized block
func (con *Consensus) prepare(initBlock *types.Block) error {
	// The block past from full node should be delivered already or known by
	// full node. We don't have to notify it.
	con.roundToNotify = initBlock.Position.Round + 1
	initRound := initBlock.Position.Round
	initConfig := utils.GetConfigWithPanic(con.gov, initRound, con.logger)
	// Setup context.
	con.ccModule.init(initBlock)
	con.logger.Debug("Calling Governance.CRS", "round", initRound)
	initCRS := con.gov.CRS(initRound)
	if (initCRS == common.Hash{}) {
		return ErrCRSNotReady
	}
	if err := con.baMgr.appendConfig(initRound, initConfig, initCRS); err != nil {
		return err
	}
	// Setup lattice module.
	initPlusOneCfg := utils.GetConfigWithPanic(con.gov, initRound+1, con.logger)
	if err := con.lattice.AppendConfig(initRound+1, initPlusOneCfg); err != nil {
		return err
	}
	// Register events.
	dkgSet, err := con.nodeSetCache.GetDKGSet(initRound)
	if err != nil {
		return err
	}
	// TODO(jimmy): registerDKG should be called after dmoment.
	if _, exist := dkgSet[con.ID]; exist {
		con.logger.Info("Selected as DKG set", "round", initRound)
		con.cfgModule.registerDKG(initRound, getDKGThreshold(initConfig))
		con.event.RegisterTime(con.dMoment.Add(initConfig.RoundInterval/4),
			func(time.Time) {
				con.runDKG(initRound, initConfig)
			})
	}
	con.initialRound(con.dMoment, initRound, initConfig)
	return nil
}

// Run starts running DEXON Consensus.
func (con *Consensus) Run() {
	// Launch BA routines.
	con.baMgr.run()
	// Launch network handler.
	con.logger.Debug("Calling Network.ReceiveChan")
	go con.processMsg(con.network.ReceiveChan())
	// Sleep until dMoment come.
	time.Sleep(con.dMoment.Sub(time.Now().UTC()))
	// Block until done.
	select {
	case <-con.ctx.Done():
	}
}

// runDKG starts running DKG protocol.
func (con *Consensus) runDKG(round uint64, config *types.Config) {
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
				config.RoundInterval.Nanoseconds()/2 {
				con.logger.Warn("Your computer cannot finish DKG on time!",
					"nodeID", con.ID.String())
			}
		}()
		if err := con.cfgModule.runDKG(round); err != nil {
			con.logger.Error("Failed to runDKG", "error", err)
		}
	}()
}

func (con *Consensus) runCRS(round uint64) {
	for {
		con.logger.Debug("Calling Governance.CRS to check if already proposed",
			"round", round+1)
		if (con.gov.CRS(round+1) != common.Hash{}) {
			con.logger.Info("CRS already proposed", "round", round+1)
			return
		}
		con.logger.Debug("Calling Governance.IsDKGFinal to check if ready to run CRS",
			"round", round)
		if con.cfgModule.isDKGFinal(round) {
			break
		}
		con.logger.Debug("DKG is not ready for running CRS. Retry later...",
			"round", round)
		time.Sleep(500 * time.Millisecond)
	}
	// Start running next round CRS.
	con.logger.Debug("Calling Governance.CRS", "round", round)
	psig, err := con.cfgModule.preparePartialSignature(
		round, utils.GetCRSWithPanic(con.gov, round, con.logger))
	if err != nil {
		con.logger.Error("Failed to prepare partial signature", "error", err)
	} else if err = con.signer.SignDKGPartialSignature(psig); err != nil {
		con.logger.Error("Failed to sign DKG partial signature", "error", err)
	} else if err = con.cfgModule.processPartialSignature(psig); err != nil {
		con.logger.Error("Failed to process partial signature", "error", err)
	} else {
		con.logger.Debug("Calling Network.BroadcastDKGPartialSignature",
			"proposer", psig.ProposerID,
			"round", psig.Round,
			"hash", psig.Hash)
		con.network.BroadcastDKGPartialSignature(psig)
		con.logger.Debug("Calling Governance.CRS", "round", round)
		crs, err := con.cfgModule.runCRSTSig(
			round, utils.GetCRSWithPanic(con.gov, round, con.logger))
		if err != nil {
			con.logger.Error("Failed to run CRS Tsig", "error", err)
		} else {
			con.logger.Debug("Calling Governance.ProposeCRS",
				"round", round+1,
				"crs", hex.EncodeToString(crs))
			con.gov.ProposeCRS(round+1, crs)
		}
	}
}

func (con *Consensus) initialRound(
	startTime time.Time, round uint64, config *types.Config) {
	select {
	case <-con.ctx.Done():
		return
	default:
	}
	curDkgSet, err := con.nodeSetCache.GetDKGSet(round)
	if err != nil {
		con.logger.Error("Error getting DKG set", "round", round, "error", err)
		curDkgSet = make(map[types.NodeID]struct{})
	}
	// Initiate CRS routine.
	if _, exist := curDkgSet[con.ID]; exist {
		con.event.RegisterTime(startTime.Add(config.RoundInterval/2),
			func(time.Time) {
				go func() {
					con.runCRS(round)
				}()
			})
	}
	// Initiate BA modules.
	con.event.RegisterTime(
		startTime.Add(config.RoundInterval/2+config.LambdaDKG),
		func(time.Time) {
			go func(nextRound uint64) {
				for (con.gov.CRS(nextRound) == common.Hash{}) {
					con.logger.Info("CRS is not ready yet. Try again later...",
						"nodeID", con.ID,
						"round", nextRound)
					time.Sleep(500 * time.Millisecond)
				}
				// Notify BA for new round.
				nextConfig := utils.GetConfigWithPanic(
					con.gov, nextRound, con.logger)
				con.logger.Debug("Calling Governance.CRS",
					"round", nextRound)
				nextCRS := utils.GetCRSWithPanic(con.gov, nextRound, con.logger)
				if err := con.baMgr.appendConfig(
					nextRound, nextConfig, nextCRS); err != nil {
					panic(err)
				}
			}(round + 1)
		})
	// Initiate DKG for this round.
	con.event.RegisterTime(startTime.Add(config.RoundInterval/2+config.LambdaDKG),
		func(time.Time) {
			go func(nextRound uint64) {
				// Normally, gov.CRS would return non-nil. Use this for in case of
				// unexpected network fluctuation and ensure the robustness.
				for (con.gov.CRS(nextRound) == common.Hash{}) {
					con.logger.Info("CRS is not ready yet. Try again later...",
						"nodeID", con.ID,
						"round", nextRound)
					time.Sleep(500 * time.Millisecond)
				}
				nextDkgSet, err := con.nodeSetCache.GetDKGSet(nextRound)
				if err != nil {
					con.logger.Error("Error getting DKG set",
						"round", nextRound,
						"error", err)
					return
				}
				if _, exist := nextDkgSet[con.ID]; !exist {
					return
				}
				con.logger.Info("Selected as DKG set", "round", nextRound)
				con.cfgModule.registerDKG(nextRound, getDKGThreshold(config))
				con.event.RegisterTime(
					startTime.Add(config.RoundInterval*2/3),
					func(time.Time) {
						func() {
							con.dkgReady.L.Lock()
							defer con.dkgReady.L.Unlock()
							con.dkgRunning = 0
						}()
						nextConfig := utils.GetConfigWithPanic(
							con.gov, nextRound, con.logger)
						con.runDKG(nextRound, nextConfig)
					})
			}(round + 1)
		})
	// Prepare lattice module for next round and next "initialRound" routine.
	con.event.RegisterTime(startTime.Add(config.RoundInterval),
		func(time.Time) {
			// Change round.
			// Get configuration for next round.
			nextRound := round + 1
			nextConfig := utils.GetConfigWithPanic(con.gov, nextRound, con.logger)
			con.initialRound(
				startTime.Add(config.RoundInterval), nextRound, nextConfig)
		})
}

// Stop the Consensus core.
func (con *Consensus) Stop() {
	con.ctxCancel()
	con.baMgr.stop()
	con.event.Reset()
}

func (con *Consensus) processMsg(msgChan <-chan interface{}) {
MessageLoop:
	for {
		var msg interface{}
		select {
		case msg = <-msgChan:
		case <-con.ctx.Done():
			return
		}

		switch val := msg.(type) {
		case *types.Block:
			if ch, exist := func() (chan<- *types.Block, bool) {
				con.lock.RLock()
				defer con.lock.RUnlock()
				ch, e := con.baConfirmedBlock[val.Hash]
				return ch, e
			}(); exist {
				if err := con.lattice.SanityCheck(val); err != nil {
					if err == ErrRetrySanityCheckLater {
						err = nil
					} else {
						con.logger.Error("SanityCheck failed", "error", err)
						continue MessageLoop
					}
				}
				func() {
					con.lock.Lock()
					defer con.lock.Unlock()
					// In case of multiple delivered block.
					if _, exist := con.baConfirmedBlock[val.Hash]; !exist {
						return
					}
					delete(con.baConfirmedBlock, val.Hash)
					ch <- val
				}()
			} else if val.IsFinalized() {
				// For sync mode.
				if err := con.processFinalizedBlock(val); err != nil {
					con.logger.Error("Failed to process finalized block",
						"block", val,
						"error", err)
				}
			} else {
				if err := con.preProcessBlock(val); err != nil {
					con.logger.Error("Failed to pre process block",
						"block", val,
						"error", err)
				}
			}
		case *types.Vote:
			if err := con.ProcessVote(val); err != nil {
				con.logger.Error("Failed to process vote",
					"vote", val,
					"error", err)
			}
		case *types.AgreementResult:
			if err := con.ProcessAgreementResult(val); err != nil {
				con.logger.Error("Failed to process agreement result",
					"result", val,
					"error", err)
			}
		case *types.BlockRandomnessResult:
			if err := con.ProcessBlockRandomnessResult(val, true); err != nil {
				con.logger.Error("Failed to process block randomness result",
					"hash", val.BlockHash.String()[:6],
					"position", &val.Position,
					"error", err)
			}
		case *typesDKG.PrivateShare:
			if err := con.cfgModule.processPrivateShare(val); err != nil {
				con.logger.Error("Failed to process private share",
					"error", err)
			}

		case *typesDKG.PartialSignature:
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
	round uint64, chainID uint32) (*types.Block, error) {
	block := &types.Block{
		Position: types.Position{
			Round:   round,
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
	err = con.baMgr.processVote(v)
	return
}

// ProcessAgreementResult processes the randomness request.
func (con *Consensus) ProcessAgreementResult(
	rand *types.AgreementResult) error {
	// Sanity Check.
	if err := VerifyAgreementResult(rand, con.nodeSetCache); err != nil {
		return err
	}
	// Syncing BA Module.
	if err := con.baMgr.processAgreementResult(rand); err != nil {
		return err
	}
	// Calculating randomness.
	if rand.Position.Round == 0 {
		return nil
	}
	// TODO(mission): find a way to avoid spamming by older agreement results.
	// Sanity check done.
	if !con.cfgModule.touchTSigHash(rand.BlockHash) {
		return nil
	}
	con.logger.Debug("Rebroadcast AgreementResult",
		"result", rand)
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
	if err = con.signer.SignDKGPartialSignature(psig); err != nil {
		return err
	}
	if err = con.cfgModule.processPartialSignature(psig); err != nil {
		return err
	}
	con.logger.Debug("Calling Network.BroadcastDKGPartialSignature",
		"proposer", psig.ProposerID,
		"round", psig.Round,
		"hash", psig.Hash.String()[:6])
	con.network.BroadcastDKGPartialSignature(psig)
	go func() {
		tsig, err := con.cfgModule.runTSig(rand.Position.Round, rand.BlockHash)
		if err != nil {
			if err != ErrTSigAlreadyRunning {
				con.logger.Error("Failed to run TSIG",
					"position", &rand.Position,
					"hash", rand.BlockHash.String()[:6],
					"error", err)
			}
			return
		}
		result := &types.BlockRandomnessResult{
			BlockHash:  rand.BlockHash,
			Position:   rand.Position,
			Randomness: tsig.Signature,
		}
		if err := con.ProcessBlockRandomnessResult(result, true); err != nil {
			con.logger.Error("Failed to process randomness result",
				"error", err)
			return
		}
	}()
	return nil
}

// ProcessBlockRandomnessResult processes the randomness result.
func (con *Consensus) ProcessBlockRandomnessResult(
	rand *types.BlockRandomnessResult, needBroadcast bool) error {
	if rand.Position.Round == 0 {
		return nil
	}
	if err := con.ccModule.processBlockRandomnessResult(rand); err != nil {
		if err == ErrBlockNotRegistered {
			err = nil
		} else {
			return err
		}
	}
	if needBroadcast {
		con.logger.Debug("Calling Network.BroadcastRandomnessResult",
			"hash", rand.BlockHash.String()[:6],
			"position", &rand.Position,
			"randomness", hex.EncodeToString(rand.Randomness))
		con.network.BroadcastRandomnessResult(rand)
	}
	return con.deliverFinalizedBlocks()
}

// preProcessBlock performs Byzantine Agreement on the block.
func (con *Consensus) preProcessBlock(b *types.Block) (err error) {
	err = con.baMgr.processBlock(b)
	if err == nil && con.debugApp != nil {
		con.debugApp.BlockReceived(b.Hash)
	}
	return
}

// deliverBlock deliver a block to application layer.
func (con *Consensus) deliverBlock(b *types.Block) {
	if err := con.db.UpdateBlock(*b); err != nil {
		panic(err)
	}
	if err := con.db.PutCompactionChainTipInfo(
		b.Hash, b.Finalization.Height); err != nil {
		panic(err)
	}
	con.cfgModule.untouchTSigHash(b.Hash)
	con.logger.Debug("Calling Application.BlockDelivered", "block", b)
	con.app.BlockDelivered(b.Hash, b.Position, b.Finalization.Clone())
	if b.Position.Round == con.roundToNotify {
		// Get configuration for the round next to next round. Configuration
		// for that round should be ready at this moment and is required for
		// lattice module. This logic is related to:
		//  - roundShift
		//  - notifyGenesisRound
		futureRound := con.roundToNotify + 1
		futureConfig := utils.GetConfigWithPanic(con.gov, futureRound, con.logger)
		con.logger.Debug("Append Config", "round", futureRound)
		if err := con.lattice.AppendConfig(
			futureRound, futureConfig); err != nil {
			con.logger.Debug("Unable to append config",
				"round", futureRound,
				"error", err)
			panic(err)
		}
		// Only the first block delivered of that round would
		// trigger this noitification.
		con.logger.Debug("Calling Governance.NotifyRoundHeight",
			"round", con.roundToNotify,
			"height", b.Finalization.Height)
		con.gov.NotifyRoundHeight(
			con.roundToNotify, b.Finalization.Height)
		con.roundToNotify++
	}
	if con.debugApp != nil {
		con.debugApp.BlockReady(b.Hash)
	}
}

// deliverFinalizedBlocks extracts and delivers finalized blocks to application
// layer.
func (con *Consensus) deliverFinalizedBlocks() error {
	con.lock.Lock()
	defer con.lock.Unlock()
	return con.deliverFinalizedBlocksWithoutLock()
}

func (con *Consensus) deliverFinalizedBlocksWithoutLock() (err error) {
	deliveredBlocks := con.ccModule.extractBlocks()
	con.logger.Debug("Last blocks in compaction chain",
		"delivered", con.ccModule.lastDeliveredBlock(),
		"pending", con.ccModule.lastPendingBlock())
	for _, b := range deliveredBlocks {
		con.deliverBlock(b)
	}
	if err = con.lattice.PurgeBlocks(deliveredBlocks); err != nil {
		return
	}
	return
}

// processBlock is the entry point to submit one block to a Consensus instance.
func (con *Consensus) processBlock(block *types.Block) (err error) {
	if err = con.db.PutBlock(*block); err != nil && err != db.ErrBlockExists {
		return
	}
	con.lock.Lock()
	defer con.lock.Unlock()
	// Block processed by lattice can be out-of-order. But the output of lattice
	// (deliveredBlocks) cannot.
	deliveredBlocks, err := con.lattice.ProcessBlock(block)
	if err != nil {
		return
	}
	// Pass delivered blocks to compaction chain.
	for _, b := range deliveredBlocks {
		if b.IsFinalized() {
			if con.nonFinalizedBlockDelivered {
				panic(fmt.Errorf("attempting to skip finalized block: %s", b))
			}
			con.logger.Info("skip delivery of finalized block",
				"block", b,
				"finalization-height", b.Finalization.Height)
			continue
		} else {
			// Mark that some non-finalized block delivered. After this flag
			// turned on, it's not allowed to deliver finalized blocks anymore.
			con.nonFinalizedBlockDelivered = true
		}
		if err = con.ccModule.processBlock(b); err != nil {
			return
		}
		go con.event.NotifyTime(b.Finalization.Timestamp)
	}
	if err = con.deliverFinalizedBlocksWithoutLock(); err != nil {
		return
	}
	return
}

// processFinalizedBlock is the entry point for handling finalized blocks.
func (con *Consensus) processFinalizedBlock(block *types.Block) error {
	return con.ccModule.processFinalizedBlock(block)
}

// PrepareBlock would setup header fields of block based on its ProposerID.
func (con *Consensus) prepareBlock(b *types.Block,
	proposeTime time.Time) (err error) {
	if err = con.lattice.PrepareBlock(b, proposeTime); err != nil {
		return
	}
	con.logger.Debug("Calling Governance.CRS", "round", b.Position.Round)
	crs := con.gov.CRS(b.Position.Round)
	if crs.Equal(common.Hash{}) {
		con.logger.Error("CRS for round is not ready, unable to prepare block",
			"position", &b.Position)
		err = ErrCRSNotReady
		return
	}
	if err = con.signer.SignCRS(b, crs); err != nil {
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
