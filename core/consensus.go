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
	"sync/atomic"
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
	ErrUnknownBlockProposed = fmt.Errorf(
		"unknown block is proposed")
	ErrIncorrectAgreementResultPosition = fmt.Errorf(
		"incorrect agreement result position")
	ErrNotEnoughVotes = fmt.Errorf(
		"not enought votes")
	ErrCRSNotReady = fmt.Errorf(
		"CRS not ready")
	ErrConfigurationNotReady = fmt.Errorf(
		"Configuration not ready")
)

// consensusBAReceiver implements agreementReceiver.
type consensusBAReceiver struct {
	// TODO(mission): consensus would be replaced by blockChain and network.
	consensus               *Consensus
	agreementModule         *agreement
	changeNotaryHeightValue *atomic.Value
	roundValue              *atomic.Value
	isNotary                bool
	restartNotary           chan types.Position
}

func (recv *consensusBAReceiver) round() uint64 {
	return recv.roundValue.Load().(uint64)
}

func (recv *consensusBAReceiver) changeNotaryHeight() uint64 {
	return recv.changeNotaryHeightValue.Load().(uint64)
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
	block, err := recv.consensus.proposeBlock(recv.agreementModule.agreementID())
	if err != nil || block == nil {
		recv.consensus.logger.Error("unable to propose block", "error", err)
		return types.NullBlockHash
	}
	go func() {
		if err := recv.consensus.preProcessBlock(block); err != nil {
			recv.consensus.logger.Error("Failed to pre-process block", "error", err)
			return
		}
		recv.consensus.logger.Debug("Calling Network.BroadcastBlock",
			"block", block)
		recv.consensus.network.BroadcastBlock(block)
	}()
	return block.Hash
}

func (recv *consensusBAReceiver) ConfirmBlock(
	hash common.Hash, votes map[types.NodeID]*types.Vote) {
	var (
		block *types.Block
		aID   = recv.agreementModule.agreementID()
	)
	isEmptyBlockConfirmed := hash == common.Hash{}
	if isEmptyBlockConfirmed {
		recv.consensus.logger.Info("Empty block is confirmed", "position", aID)
		var err error
		block, err = recv.consensus.bcModule.addEmptyBlock(aID)
		if err != nil {
			recv.consensus.logger.Error("Add position for empty failed",
				"error", err)
			return
		}
		if block == nil {
			// The empty block's parent is not found locally, thus we can't
			// propose it at this moment.
			//
			// We can only rely on block pulling upon receiving
			// types.AgreementResult from the next position.
			recv.consensus.logger.Warn(
				"An empty block is confirmed without its parent",
				"position", aID)
			return
		}
	} else {
		var exist bool
		block, exist = recv.agreementModule.findBlockNoLock(hash)
		if !exist {
			recv.consensus.logger.Error("Unknown block confirmed",
				"hash", hash.String()[:6])
			ch := make(chan *types.Block)
			func() {
				recv.consensus.lock.Lock()
				defer recv.consensus.lock.Unlock()
				recv.consensus.baConfirmedBlock[hash] = ch
			}()
			go func() {
				hashes := common.Hashes{hash}
			PullBlockLoop:
				for {
					recv.consensus.logger.Debug("Calling Network.PullBlock for BA block",
						"hash", hash)
					recv.consensus.network.PullBlocks(hashes)
					select {
					case block = <-ch:
						break PullBlockLoop
					case <-time.After(1 * time.Second):
					}
				}
				recv.consensus.logger.Info("Receive unknown block",
					"hash", hash.String()[:6],
					"position", block.Position)
				recv.agreementModule.addCandidateBlock(block)
				recv.agreementModule.lock.Lock()
				defer recv.agreementModule.lock.Unlock()
				recv.ConfirmBlock(block.Hash, votes)
			}()
			return
		}
	}
	if block.Position.Height != 0 &&
		!recv.consensus.bcModule.confirmed(block.Position.Height-1) {
		go func(hash common.Hash) {
			parentHash := hash
			for {
				recv.consensus.logger.Warn("Parent block not confirmed",
					"parent-hash", parentHash.String()[:6],
					"cur-position", block.Position)
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
					"cur-position", block.Position)
				recv.consensus.processBlockChan <- block
				parentHash = block.ParentHash
				if block.Position.Height == 0 ||
					recv.consensus.bcModule.confirmed(
						block.Position.Height-1) {
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
	recv.consensus.processBlockChan <- block
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
	if block.Position.Height+1 == recv.changeNotaryHeight() {
		newPos.Round++
		recv.roundValue.Store(newPos.Round)
	}
	currentRound := recv.round()
	changeNotaryHeight := recv.changeNotaryHeight()
	if block.Position.Height > changeNotaryHeight &&
		block.Position.Round <= currentRound {
		panic(fmt.Errorf(
			"round not switch when confirmig: %s, %d, should switch at %d",
			block, currentRound, changeNotaryHeight))
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

func (recv *consensusBAReceiver) ReportForkVote(v1, v2 *types.Vote) {
	recv.consensus.gov.ReportForkVote(v1, v2)
}

func (recv *consensusBAReceiver) ReportForkBlock(b1, b2 *types.Block) {
	recv.consensus.gov.ReportForkBlock(b1, b2)
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

	// Interfaces.
	db       db.Database
	app      Application
	debugApp Debug
	gov      Governance
	network  Network

	// Misc.
	bcModule                 *blockChain
	dMoment                  time.Time
	nodeSetCache             *utils.NodeSetCache
	lock                     sync.RWMutex
	ctx                      context.Context
	ctxCancel                context.CancelFunc
	event                    *common.Event
	roundEvent               *utils.RoundEvent
	logger                   common.Logger
	resetRandomnessTicker    chan struct{}
	resetDeliveryGuardTicker chan struct{}
	msgChan                  chan interface{}
	waitGroup                sync.WaitGroup
	processBlockChan         chan *types.Block

	// Context of Dummy receiver during switching from syncer.
	dummyCancel    context.CancelFunc
	dummyFinished  <-chan struct{}
	dummyMsgBuffer []interface{}
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
	return newConsensusForRound(
		nil, 0, dMoment, app, gov, db, network, prv, logger, true)
}

// NewConsensusForSimulation creates an instance of Consensus for simulation,
// the only difference with NewConsensus is nonblocking of app.
func NewConsensusForSimulation(
	dMoment time.Time,
	app Application,
	gov Governance,
	db db.Database,
	network Network,
	prv crypto.PrivateKey,
	logger common.Logger) *Consensus {
	return newConsensusForRound(
		nil, 0, dMoment, app, gov, db, network, prv, logger, false)
}

// NewConsensusFromSyncer constructs an Consensus instance from information
// provided from syncer.
//
// You need to provide the initial block for this newly created Consensus
// instance to bootstrap with. A proper choice is the last finalized block you
// delivered to syncer.
//
// NOTE: those confirmed blocks should be organized by chainID and sorted by
//       their positions, in ascending order.
func NewConsensusFromSyncer(
	initBlock *types.Block,
	initRoundBeginHeight uint64,
	startWithEmpty bool,
	dMoment time.Time,
	app Application,
	gov Governance,
	db db.Database,
	networkModule Network,
	prv crypto.PrivateKey,
	confirmedBlocks []*types.Block,
	randomnessResults []*types.BlockRandomnessResult,
	cachedMessages []interface{},
	logger common.Logger) (*Consensus, error) {
	// Setup Consensus instance.
	con := newConsensusForRound(initBlock, initRoundBeginHeight, dMoment, app,
		gov, db, networkModule, prv, logger, true)
	// Launch a dummy receiver before we start receiving from network module.
	con.dummyMsgBuffer = cachedMessages
	con.dummyCancel, con.dummyFinished = utils.LaunchDummyReceiver(
		con.ctx, networkModule.ReceiveChan(), func(msg interface{}) {
			con.dummyMsgBuffer = append(con.dummyMsgBuffer, msg)
		})
	// Dump all BA-confirmed blocks to the consensus instance, make sure these
	// added blocks forming a DAG.
	refBlock := initBlock
	for _, b := range confirmedBlocks {
		// Only when its parent block is already added to lattice, we can
		// then add this block. If not, our pulling mechanism would stop at
		// the block we added, and lost its parent block forever.
		if b.Position.Height != refBlock.Position.Height+1 {
			break
		}
		if err := con.processBlock(b); err != nil {
			return nil, err
		}
		refBlock = b
	}
	// Dump all randomness result to the consensus instance.
	for _, r := range randomnessResults {
		if err := con.ProcessBlockRandomnessResult(r, false); err != nil {
			con.logger.Error("failed to process randomness result when syncing",
				"result", r)
			continue
		}
	}
	if startWithEmpty {
		pos := initBlock.Position
		pos.Height++
		block, err := con.bcModule.addEmptyBlock(pos)
		if err != nil {
			panic(err)
		}
		con.processBlockChan <- block
		if pos.Round >= DKGDelayRound {
			rand := &types.AgreementResult{
				BlockHash:    block.Hash,
				Position:     block.Position,
				IsEmptyBlock: true,
			}
			go con.prepareRandomnessResult(rand)
		}
	}
	return con, nil
}

// newConsensusForRound creates a Consensus instance.
// TODO(mission): remove dMoment, it's no longer one part of consensus.
func newConsensusForRound(
	initBlock *types.Block,
	initRoundBeginHeight uint64,
	dMoment time.Time,
	app Application,
	gov Governance,
	db db.Database,
	network Network,
	prv crypto.PrivateKey,
	logger common.Logger,
	usingNonBlocking bool) *Consensus {
	// TODO(w): load latest blockHeight from DB, and use config at that height.
	nodeSetCache := utils.NewNodeSetCache(gov)
	// Setup signer module.
	signer := utils.NewSigner(prv)
	// Check if the application implement Debug interface.
	var debugApp Debug
	if a, ok := app.(Debug); ok {
		debugApp = a
	}
	// Get configuration for bootstrap round.
	initRound := uint64(0)
	initBlockHeight := uint64(0)
	if initBlock != nil {
		initRound = initBlock.Position.Round
		initBlockHeight = initBlock.Position.Height
	}
	initConfig := utils.GetConfigWithPanic(gov, initRound, logger)
	initCRS := utils.GetCRSWithPanic(gov, initRound, logger)
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
	cfgModule := newConfigurationChain(ID, recv, gov, nodeSetCache, db, logger)
	dkg, err := recoverDKGProtocol(ID, recv, initRound, utils.GetDKGThreshold(initConfig), db)
	if err != nil {
		panic(err)
	}
	cfgModule.dkg = dkg
	recv.cfgModule = cfgModule
	appModule := app
	if usingNonBlocking {
		appModule = newNonBlocking(app, debugApp)
	}
	bcModule := newBlockChain(ID, dMoment, initBlock, appModule,
		NewTSigVerifierCache(gov, 7), signer, logger)
	// Construct Consensus instance.
	con := &Consensus{
		ID:                       ID,
		app:                      appModule,
		debugApp:                 debugApp,
		gov:                      gov,
		db:                       db,
		network:                  network,
		baConfirmedBlock:         make(map[common.Hash]chan<- *types.Block),
		dkgReady:                 sync.NewCond(&sync.Mutex{}),
		cfgModule:                cfgModule,
		bcModule:                 bcModule,
		dMoment:                  dMoment,
		nodeSetCache:             nodeSetCache,
		signer:                   signer,
		event:                    common.NewEvent(),
		logger:                   logger,
		resetRandomnessTicker:    make(chan struct{}),
		resetDeliveryGuardTicker: make(chan struct{}),
		msgChan:                  make(chan interface{}, 1024),
		processBlockChan:         make(chan *types.Block, 1024),
	}
	con.ctx, con.ctxCancel = context.WithCancel(context.Background())
	if con.roundEvent, err = utils.NewRoundEvent(con.ctx, gov, logger, initRound,
		initRoundBeginHeight, initBlockHeight, ConfigRoundShift); err != nil {
		panic(err)
	}
	baConfig := agreementMgrConfig{}
	baConfig.from(initRound, initConfig, initCRS)
	baConfig.SetRoundBeginHeight(initRoundBeginHeight)
	con.baMgr, err = newAgreementMgr(con, initRound, baConfig)
	if err != nil {
		panic(err)
	}
	if err = con.prepare(initRoundBeginHeight, initBlock); err != nil {
		panic(err)
	}
	return con
}

// prepare the Consensus instance to be ready for blocks after 'initBlock'.
// 'initBlock' could be either:
//  - nil
//  - the last finalized block
func (con *Consensus) prepare(
	initRoundBeginHeight uint64, initBlock *types.Block) (err error) {
	// Trigger the round validation method for the next round of the first
	// round.
	// The block past from full node should be delivered already or known by
	// full node. We don't have to notify it.
	initRound := uint64(0)
	if initBlock != nil {
		initRound = initBlock.Position.Round
	}
	if initRound == 0 {
		if DKGDelayRound == 0 {
			panic("not implemented yet")
		}
	}
	// Register round event handler to update BA and BC modules.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		// Always updates newer configs to the later modules first in the flow.
		if err := con.bcModule.notifyRoundEvents(evts); err != nil {
			panic(err)
		}
		// The init config is provided to baModule when construction.
		if evts[len(evts)-1].BeginHeight != initRoundBeginHeight {
			if err := con.baMgr.notifyRoundEvents(evts); err != nil {
				panic(err)
			}
		}
	})
	// Register round event handler to propose new CRS.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		// We don't have to propose new CRS during DKG reset, the reset of DKG
		// would be done by the DKG set in previous round.
		e := evts[len(evts)-1]
		if e.Reset != 0 || e.Round < DKGDelayRound {
			return
		}
		if curDkgSet, err := con.nodeSetCache.GetDKGSet(e.Round); err != nil {
			con.logger.Error("Error getting DKG set when proposing CRS",
				"round", e.Round,
				"error", err)
		} else {
			if _, exist := curDkgSet[con.ID]; !exist {
				return
			}
			con.event.RegisterHeight(e.NextCRSProposingHeight(), func(uint64) {
				con.logger.Debug(
					"Calling Governance.CRS to check if already proposed",
					"round", e.Round+1)
				if (con.gov.CRS(e.Round+1) != common.Hash{}) {
					con.logger.Debug("CRS already proposed", "round", e.Round+1)
					return
				}
				con.runCRS(e.Round, e.CRS)
			})
		}
	})
	// Touch nodeSetCache for next round.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		e := evts[len(evts)-1]
		if e.Reset != 0 {
			return
		}
		con.event.RegisterHeight(e.NextTouchNodeSetCacheHeight(), func(uint64) {
			if err := con.nodeSetCache.Touch(e.Round + 1); err != nil {
				con.logger.Warn("Failed to update nodeSetCache",
					"round", e.Round+1,
					"error", err)
			}
		})
	})
	// checkCRS is a generator of checker to check if CRS for that round is
	// ready or not.
	checkCRS := func(round uint64) func() bool {
		return func() bool {
			nextCRS := con.gov.CRS(round)
			if (nextCRS != common.Hash{}) {
				return true
			}
			con.logger.Debug("CRS is not ready yet. Try again later...",
				"nodeID", con.ID,
				"round", round)
			return false
		}
	}
	// Trigger round validation method for next period.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		e := evts[len(evts)-1]
		// Register a routine to trigger round events.
		con.event.RegisterHeight(e.NextRoundValidationHeight(), func(
			blockHeight uint64) {
			con.roundEvent.ValidateNextRound(blockHeight)
		})
		// Register a routine to register next DKG.
		con.event.RegisterHeight(e.NextDKGRegisterHeight(), func(uint64) {
			nextRound := e.Round + 1
			if nextRound < DKGDelayRound {
				con.logger.Info("Skip runDKG for round", "round", nextRound)
				return
			}
			// Normally, gov.CRS would return non-nil. Use this for in case of
			// unexpected network fluctuation and ensure the robustness.
			if !checkWithCancel(
				con.ctx, 500*time.Millisecond, checkCRS(nextRound)) {
				con.logger.Debug("unable to prepare CRS for DKG set",
					"round", nextRound)
				return
			}
			nextDkgSet, err := con.nodeSetCache.GetDKGSet(nextRound)
			if err != nil {
				con.logger.Error("Error getting DKG set for next round",
					"round", nextRound,
					"error", err)
				return
			}
			if _, exist := nextDkgSet[con.ID]; !exist {
				con.logger.Info("Not selected as DKG set", "round", nextRound)
				return
			}
			con.logger.Info("Selected as DKG set", "round", nextRound)
			nextConfig := utils.GetConfigWithPanic(con.gov, nextRound,
				con.logger)
			con.cfgModule.registerDKG(nextRound, utils.GetDKGThreshold(
				nextConfig))
			con.event.RegisterHeight(e.NextDKGPreparationHeight(),
				func(uint64) {
					func() {
						con.dkgReady.L.Lock()
						defer con.dkgReady.L.Unlock()
						con.dkgRunning = 0
					}()
					con.runDKG(nextRound, nextConfig)
				})
		})
	})
	con.roundEvent.TriggerInitEvent()
	return
}

// Run starts running DEXON Consensus.
func (con *Consensus) Run() {
	// Launch BA routines.
	con.baMgr.run()
	// Launch network handler.
	con.logger.Debug("Calling Network.ReceiveChan")
	con.waitGroup.Add(1)
	go con.deliverNetworkMsg()
	con.waitGroup.Add(1)
	go con.processMsg()
	go con.processBlockLoop()
	// Sleep until dMoment come.
	time.Sleep(con.dMoment.Sub(time.Now().UTC()))
	// Take some time to bootstrap.
	time.Sleep(3 * time.Second)
	con.waitGroup.Add(1)
	go con.pullRandomness()
	// Stop dummy receiver if launched.
	if con.dummyCancel != nil {
		con.logger.Trace("Stop dummy receiver")
		con.dummyCancel()
		<-con.dummyFinished
		// Replay those cached messages.
		con.logger.Trace("Dummy receiver stoped, start dumping cached messages",
			"count", len(con.dummyMsgBuffer))
		for _, msg := range con.dummyMsgBuffer {
		loop:
			for {
				select {
				case con.msgChan <- msg:
					break loop
				case <-time.After(50 * time.Millisecond):
					con.logger.Debug(
						"internal message channel is full when syncing")
				}
			}
		}
		con.logger.Trace("Finish dumping cached messages")
	}
	con.waitGroup.Add(1)
	go con.deliveryGuard()
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
		defer func() {
			con.dkgReady.L.Lock()
			defer con.dkgReady.L.Unlock()
			con.dkgReady.Broadcast()
			con.dkgRunning = 2
		}()
		if err := con.cfgModule.runDKG(round); err != nil {
			con.logger.Error("Failed to runDKG", "error", err)
		}
	}()
}

func (con *Consensus) runCRS(round uint64, hash common.Hash) {
	// Start running next round CRS.
	psig, err := con.cfgModule.preparePartialSignature(round, hash)
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

// Stop the Consensus core.
func (con *Consensus) Stop() {
	con.ctxCancel()
	con.baMgr.stop()
	con.event.Reset()
	con.waitGroup.Wait()
	if nbApp, ok := con.app.(*nonBlocking); ok {
		fmt.Println("Stopping nonBlocking App")
		nbApp.wait()
	}
}

func (con *Consensus) deliverNetworkMsg() {
	defer con.waitGroup.Done()
	recv := con.network.ReceiveChan()
	for {
		select {
		case <-con.ctx.Done():
			return
		default:
		}
		select {
		case msg := <-recv:
		innerLoop:
			for {
				select {
				case con.msgChan <- msg:
					break innerLoop
				case <-time.After(500 * time.Millisecond):
					con.logger.Debug("internal message channel is full",
						"pending", msg)
				}
			}
		case <-con.ctx.Done():
			return
		}
	}
}

func (con *Consensus) processMsg() {
	defer con.waitGroup.Done()
MessageLoop:
	for {
		select {
		case <-con.ctx.Done():
			return
		default:
		}
		var msg interface{}
		select {
		case msg = <-con.msgChan:
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
				if err := utils.VerifyBlockSignature(val); err != nil {
					con.logger.Error("VerifyBlockSignature failed",
						"block", val,
						"error", err)
					continue MessageLoop
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
					"position", val.Position,
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

// ProcessVote is the entry point to submit ont vote to a Consensus instance.
func (con *Consensus) ProcessVote(vote *types.Vote) (err error) {
	v := vote.Clone()
	err = con.baMgr.processVote(v)
	return
}

// ProcessAgreementResult processes the randomness request.
func (con *Consensus) ProcessAgreementResult(
	rand *types.AgreementResult) error {
	if !con.baMgr.touchAgreementResult(rand) {
		return nil
	}
	// Sanity Check.
	if err := VerifyAgreementResult(rand, con.nodeSetCache); err != nil {
		con.baMgr.untouchAgreementResult(rand)
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
	go con.prepareRandomnessResult(rand)
	return nil
}

func (con *Consensus) prepareRandomnessResult(rand *types.AgreementResult) {
	dkgSet, err := con.nodeSetCache.GetDKGSet(rand.Position.Round)
	if err != nil {
		con.logger.Error("Failed to get dkg set",
			"round", rand.Position.Round, "error", err)
		return
	}
	if _, exist := dkgSet[con.ID]; !exist {
		return
	}
	con.logger.Debug("PrepareRandomness", "round", rand.Position.Round, "hash", rand.BlockHash)
	psig, err := con.cfgModule.preparePartialSignature(rand.Position.Round, rand.BlockHash)
	if err != nil {
		con.logger.Error("Failed to prepare psig",
			"round", rand.Position.Round,
			"hash", rand.BlockHash.String()[:6],
			"error", err)
		return
	}
	if err = con.signer.SignDKGPartialSignature(psig); err != nil {
		con.logger.Error("Failed to sign psig",
			"hash", rand.BlockHash.String()[:6],
			"error", err)
		return
	}
	if err = con.cfgModule.processPartialSignature(psig); err != nil {
		con.logger.Error("Failed process psig",
			"hash", rand.BlockHash.String()[:6],
			"error", err)
		return
	}
	con.logger.Debug("Calling Network.BroadcastDKGPartialSignature",
		"proposer", psig.ProposerID,
		"round", psig.Round,
		"hash", psig.Hash.String()[:6])
	con.network.BroadcastDKGPartialSignature(psig)
	tsig, err := con.cfgModule.runTSig(rand.Position.Round, rand.BlockHash)
	if err != nil {
		if err != ErrTSigAlreadyRunning {
			con.logger.Error("Failed to run TSIG",
				"position", rand.Position,
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
	// ProcessBlockRandomnessResult is not thread-safe so we put the result in
	// the message channnel to be processed in the main thread.
	con.msgChan <- result
}

// ProcessBlockRandomnessResult processes the randomness result.
func (con *Consensus) ProcessBlockRandomnessResult(
	rand *types.BlockRandomnessResult, needBroadcast bool) error {
	if rand.Position.Round == 0 {
		return nil
	}
	if !con.bcModule.shouldAddRandomness(rand) {
		return nil
	}
	if err := con.bcModule.addRandomness(rand); err != nil {
		return err
	}
	if needBroadcast {
		con.logger.Debug("Calling Network.BroadcastRandomnessResult",
			"randomness", rand)
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

func (con *Consensus) pullRandomness() {
	defer con.waitGroup.Done()
	for {
		select {
		case <-con.ctx.Done():
			return
		default:
		}
		select {
		case <-con.ctx.Done():
			return
		case <-con.resetRandomnessTicker:
		case <-time.After(1500 * time.Millisecond):
			// TODO(jimmy): pulling period should be related to lambdaBA.
			hashes := con.bcModule.pendingBlocksWithoutRandomness()
			if len(hashes) > 0 {
				con.logger.Debug(
					"Calling Network.PullRandomness", "blocks", hashes)
				con.network.PullRandomness(hashes)
			}
		}
	}
}

func (con *Consensus) deliveryGuard() {
	defer con.waitGroup.Done()
	time.Sleep(con.dMoment.Sub(time.Now()))
	// Node takes time to start.
	select {
	case <-con.ctx.Done():
	case <-time.After(60 * time.Second):
	}
	for {
		select {
		case <-con.ctx.Done():
			return
		default:
		}
		select {
		case <-con.ctx.Done():
			return
		case <-con.resetDeliveryGuardTicker:
		case <-time.After(60 * time.Second):
			con.logger.Error("no blocks delivered for too long", "ID", con.ID)
			panic(fmt.Errorf("no blocks delivered for too long"))
		}
	}
}

// deliverBlock deliver a block to application layer.
func (con *Consensus) deliverBlock(b *types.Block) {
	select {
	case con.resetRandomnessTicker <- struct{}{}:
	default:
	}
	select {
	case con.resetDeliveryGuardTicker <- struct{}{}:
	default:
	}
	// TODO(mission): do we need to put block when confirmed now?
	if err := con.db.PutBlock(*b); err != nil {
		panic(err)
	}
	if err := con.db.PutCompactionChainTipInfo(
		b.Hash, b.Finalization.Height); err != nil {
		panic(err)
	}
	con.cfgModule.untouchTSigHash(b.Hash)
	con.logger.Debug("Calling Application.BlockDelivered", "block", b)
	con.app.BlockDelivered(b.Hash, b.Position, b.Finalization.Clone())
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
	deliveredBlocks := con.bcModule.extractBlocks()
	con.logger.Debug("Last blocks in compaction chain",
		"delivered", con.bcModule.lastDeliveredBlock(),
		"pending", con.bcModule.lastPendingBlock())
	for _, b := range deliveredBlocks {
		con.deliverBlock(b)
		go con.event.NotifyHeight(b.Finalization.Height)
	}
	return
}

func (con *Consensus) processBlockLoop() {
	for {
		select {
		case <-con.ctx.Done():
			return
		default:
		}
		select {
		case <-con.ctx.Done():
			return
		case block := <-con.processBlockChan:
			if err := con.processBlock(block); err != nil {
				con.logger.Error("Error processing block",
					"block", block,
					"error", err)
			}
		}
	}
}

// processBlock is the entry point to submit one block to a Consensus instance.
func (con *Consensus) processBlock(block *types.Block) (err error) {
	// Block processed by blockChain can be out-of-order. But the output from
	// blockChain (deliveredBlocks) cannot, thus we need to protect the part
	// below with writer lock.
	con.lock.Lock()
	defer con.lock.Unlock()
	if err = con.bcModule.addBlock(block); err != nil {
		return
	}
	if err = con.deliverFinalizedBlocksWithoutLock(); err != nil {
		return
	}
	return
}

// processFinalizedBlock is the entry point for handling finalized blocks.
func (con *Consensus) processFinalizedBlock(block *types.Block) error {
	return con.bcModule.processFinalizedBlock(block)
}

// PrepareBlock would setup header fields of block based on its ProposerID.
func (con *Consensus) proposeBlock(position types.Position) (
	*types.Block, error) {
	b, err := con.bcModule.proposeBlock(position, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	con.logger.Debug("Calling Governance.CRS", "round", b.Position.Round)
	crs := con.gov.CRS(b.Position.Round)
	if crs.Equal(common.Hash{}) {
		con.logger.Error("CRS for round is not ready, unable to prepare block",
			"position", &b.Position)
		return nil, ErrCRSNotReady
	}
	if err = con.signer.SignCRS(b, crs); err != nil {
		return nil, err
	}
	return b, nil
}
