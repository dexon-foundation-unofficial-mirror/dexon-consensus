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
	cryptoDKG "github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
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
	ErrIncorrectBlockRandomness = fmt.Errorf(
		"randomness of block is incorrect")
	ErrCannotVerifyBlockRandomness = fmt.Errorf(
		"cannot verify block randomness")
)

type selfAgreementResult types.AgreementResult

// consensusBAReceiver implements agreementReceiver.
type consensusBAReceiver struct {
	consensus         *Consensus
	agreementModule   *agreement
	emptyBlockHashMap *sync.Map
	isNotary          bool
	restartNotary     chan types.Position
	npks              *typesDKG.NodePublicKeys
	psigSigner        *dkgShareSecret
}

func (recv *consensusBAReceiver) emptyBlockHash(pos types.Position) (
	common.Hash, error) {
	hashVal, ok := recv.emptyBlockHashMap.Load(pos)
	if ok {
		return hashVal.(common.Hash), nil
	}
	emptyBlock, err := recv.consensus.bcModule.prepareBlock(
		pos, time.Time{}, true)
	if err != nil {
		return common.Hash{}, err
	}
	hash, err := utils.HashBlock(emptyBlock)
	if err != nil {
		return common.Hash{}, err
	}
	recv.emptyBlockHashMap.Store(pos, hash)
	return hash, nil
}

func (recv *consensusBAReceiver) VerifyPartialSignature(vote *types.Vote) (
	bool, bool) {
	if vote.Position.Round >= DKGDelayRound && vote.BlockHash != types.SkipBlockHash {
		if vote.Type == types.VoteCom || vote.Type == types.VoteFastCom {
			if recv.npks == nil {
				recv.consensus.logger.Debug(
					"Unable to verify psig, npks is nil",
					"vote", vote)
				return false, false
			}
			if vote.Position.Round != recv.npks.Round {
				recv.consensus.logger.Debug(
					"Unable to verify psig, round of npks mismatch",
					"vote", vote,
					"npksRound", recv.npks.Round)
				return false, false
			}
			pubKey, exist := recv.npks.PublicKeys[vote.ProposerID]
			if !exist {
				recv.consensus.logger.Debug(
					"Unable to verify psig, proposer is not qualified",
					"vote", vote)
				return false, true
			}
			blockHash := vote.BlockHash
			if blockHash == types.NullBlockHash {
				var err error
				blockHash, err = recv.emptyBlockHash(vote.Position)
				if err != nil {
					recv.consensus.logger.Error(
						"Failed to verify vote for empty block",
						"position", vote.Position,
						"error", err)
					return false, true
				}
			}
			return pubKey.VerifySignature(
				blockHash, crypto.Signature(vote.PartialSignature)), true
		}
	}
	return len(vote.PartialSignature.Signature) == 0, true
}

func (recv *consensusBAReceiver) ProposeVote(vote *types.Vote) {
	if !recv.isNotary {
		return
	}
	if recv.psigSigner != nil &&
		vote.BlockHash != types.SkipBlockHash {
		if vote.Type == types.VoteCom || vote.Type == types.VoteFastCom {
			if vote.BlockHash == types.NullBlockHash {
				hash, err := recv.emptyBlockHash(vote.Position)
				if err != nil {
					recv.consensus.logger.Error(
						"Failed to propose vote for empty block",
						"position", vote.Position,
						"error", err)
					return
				}
				vote.PartialSignature = recv.psigSigner.sign(hash)
			} else {
				vote.PartialSignature = recv.psigSigner.sign(vote.BlockHash)
			}
		}
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
		recv.consensus.logger.Error("Unable to propose block", "error", err)
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
			recv.consensus.logger.Debug("Unknown block confirmed",
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
				recv.consensus.logger.Debug("Receive unknown block",
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

	if len(votes) == 0 && len(block.Randomness) == 0 {
		recv.consensus.logger.Error("No votes to recover randomness",
			"block", block)
	} else if votes != nil {
		voteList := make([]types.Vote, 0, len(votes))
		IDs := make(cryptoDKG.IDs, 0, len(votes))
		psigs := make([]cryptoDKG.PartialSignature, 0, len(votes))
		for _, vote := range votes {
			if vote.BlockHash != hash {
				continue
			}
			if block.Position.Round >= DKGDelayRound {
				ID, exist := recv.npks.IDMap[vote.ProposerID]
				if !exist {
					continue
				}
				IDs = append(IDs, ID)
				psigs = append(psigs, vote.PartialSignature)
			} else {
				voteList = append(voteList, *vote)
			}
		}
		if block.Position.Round >= DKGDelayRound {
			rand, err := cryptoDKG.RecoverSignature(psigs, IDs)
			if err != nil {
				recv.consensus.logger.Warn("Unable to recover randomness",
					"block", block,
					"error", err)
			} else {
				block.Randomness = rand.Signature[:]
			}
		} else {
			block.Randomness = NoRand
		}

		if recv.isNotary {
			result := &types.AgreementResult{
				BlockHash:    block.Hash,
				Position:     block.Position,
				Votes:        voteList,
				IsEmptyBlock: isEmptyBlockConfirmed,
				Randomness:   block.Randomness,
			}
			// touchAgreementResult does not support concurrent access.
			go func() {
				recv.consensus.priorityMsgChan <- (*selfAgreementResult)(result)
			}()
			recv.consensus.logger.Debug("Broadcast AgreementResult",
				"result", result)
			recv.consensus.network.BroadcastAgreementResult(result)
			if block.IsEmpty() {
				recv.consensus.bcModule.addBlockRandomness(
					block.Position, block.Randomness)
			}
			if block.Position.Round >= DKGDelayRound {
				recv.consensus.logger.Debug(
					"Broadcast finalized block",
					"block", block)
				recv.consensus.network.BroadcastBlock(block)
			}
		}
	}

	if !block.IsGenesis() &&
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
				if !block.IsFinalized() {
					// TODO(jimmy): use a seperate message to pull finalized
					// block. Here, we pull it again as workaround.
					continue
				}
				recv.consensus.processBlockChan <- block
				parentHash = block.ParentHash
				if block.IsGenesis() || recv.consensus.bcModule.confirmed(
					block.Position.Height-1) {
					return
				}
			}
		}(block.ParentHash)
	}
	if !block.IsEmpty() {
		recv.consensus.processBlockChan <- block
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
	recv.restartNotary <- block.Position
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
	b1Clone := b1.Clone()
	b2Clone := b2.Clone()
	b1Clone.Payload = []byte{}
	b2Clone.Payload = []byte{}
	recv.consensus.gov.ReportForkBlock(b1Clone, b2Clone)
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
	recv.gov.AddDKGComplaint(complaint)
}

// ProposeDKGMasterPublicKey propose a DKGMasterPublicKey.
func (recv *consensusDKGReceiver) ProposeDKGMasterPublicKey(
	mpk *typesDKG.MasterPublicKey) {
	if err := recv.signer.SignDKGMasterPublicKey(mpk); err != nil {
		recv.logger.Error("Failed to sign DKG master public key", "error", err)
		return
	}
	recv.logger.Debug("Calling Governance.AddDKGMasterPublicKey", "key", mpk)
	recv.gov.AddDKGMasterPublicKey(mpk)
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
	recv.gov.AddDKGMPKReady(ready)
}

// ProposeDKGFinalize propose a DKGFinalize message.
func (recv *consensusDKGReceiver) ProposeDKGFinalize(final *typesDKG.Finalize) {
	if err := recv.signer.SignDKGFinalize(final); err != nil {
		recv.logger.Error("Failed to sign DKG finalize", "error", err)
		return
	}
	recv.logger.Debug("Calling Governance.AddDKGFinalize", "final", final)
	recv.gov.AddDKGFinalize(final)
}

// ProposeDKGSuccess propose a DKGSuccess message.
func (recv *consensusDKGReceiver) ProposeDKGSuccess(success *typesDKG.Success) {
	if err := recv.signer.SignDKGSuccess(success); err != nil {
		recv.logger.Error("Failed to sign DKG successize", "error", err)
		return
	}
	recv.logger.Debug("Calling Governance.AddDKGSuccess", "success", success)
	recv.gov.AddDKGSuccess(success)
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
	tsigVerifierCache        *TSigVerifierCache
	lock                     sync.RWMutex
	ctx                      context.Context
	ctxCancel                context.CancelFunc
	event                    *common.Event
	roundEvent               *utils.RoundEvent
	logger                   common.Logger
	resetDeliveryGuardTicker chan struct{}
	msgChan                  chan types.Msg
	priorityMsgChan          chan interface{}
	waitGroup                sync.WaitGroup
	processBlockChan         chan *types.Block

	// Context of Dummy receiver during switching from syncer.
	dummyCancel    context.CancelFunc
	dummyFinished  <-chan struct{}
	dummyMsgBuffer []types.Msg
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
		nil, dMoment, app, gov, db, network, prv, logger, true)
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
		nil, dMoment, app, gov, db, network, prv, logger, false)
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
	startWithEmpty bool,
	dMoment time.Time,
	app Application,
	gov Governance,
	db db.Database,
	networkModule Network,
	prv crypto.PrivateKey,
	confirmedBlocks []*types.Block,
	cachedMessages []types.Msg,
	logger common.Logger) (*Consensus, error) {
	// Setup Consensus instance.
	con := newConsensusForRound(initBlock, dMoment, app, gov, db,
		networkModule, prv, logger, true)
	// Launch a dummy receiver before we start receiving from network module.
	con.dummyMsgBuffer = cachedMessages
	con.dummyCancel, con.dummyFinished = utils.LaunchDummyReceiver(
		con.ctx, networkModule.ReceiveChan(), func(msg types.Msg) {
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
	if startWithEmpty {
		emptyPos := types.Position{
			Round:  con.bcModule.tipRound(),
			Height: initBlock.Position.Height + 1,
		}
		_, err := con.bcModule.addEmptyBlock(emptyPos)
		if err != nil {
			panic(err)
		}
	}
	return con, nil
}

// newConsensusForRound creates a Consensus instance.
func newConsensusForRound(
	initBlock *types.Block,
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
	initPos := types.Position{
		Round:  0,
		Height: types.GenesisHeight,
	}
	if initBlock != nil {
		initPos = initBlock.Position
	}
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
	recv.cfgModule = cfgModule
	signer.SetBLSSigner(
		func(round uint64, hash common.Hash) (crypto.Signature, error) {
			_, signer, err := cfgModule.getDKGInfo(round, false)
			if err != nil {
				return crypto.Signature{}, err
			}
			return crypto.Signature(signer.sign(hash)), nil
		})
	appModule := app
	if usingNonBlocking {
		appModule = newNonBlocking(app, debugApp)
	}
	tsigVerifierCache := NewTSigVerifierCache(gov, 7)
	bcModule := newBlockChain(ID, dMoment, initBlock, appModule,
		tsigVerifierCache, signer, logger)
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
		tsigVerifierCache:        tsigVerifierCache,
		signer:                   signer,
		event:                    common.NewEvent(),
		logger:                   logger,
		resetDeliveryGuardTicker: make(chan struct{}),
		msgChan:                  make(chan types.Msg, 1024),
		priorityMsgChan:          make(chan interface{}, 1024),
		processBlockChan:         make(chan *types.Block, 1024),
	}
	con.ctx, con.ctxCancel = context.WithCancel(context.Background())
	var err error
	con.roundEvent, err = utils.NewRoundEvent(con.ctx, gov, logger, initPos,
		ConfigRoundShift)
	if err != nil {
		panic(err)
	}
	if con.baMgr, err = newAgreementMgr(con); err != nil {
		panic(err)
	}
	if err = con.prepare(initBlock); err != nil {
		panic(err)
	}
	return con
}

// prepare the Consensus instance to be ready for blocks after 'initBlock'.
// 'initBlock' could be either:
//  - nil
//  - the last finalized block
func (con *Consensus) prepare(initBlock *types.Block) (err error) {
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
	// Measure time elapse for each handler of round events.
	elapse := func(what string, lastE utils.RoundEventParam) func() {
		start := time.Now()
		con.logger.Info("Handle round event",
			"what", what,
			"event", lastE)
		return func() {
			con.logger.Info("Finish round event",
				"what", what,
				"event", lastE,
				"elapse", time.Since(start))
		}
	}
	// Register round event handler to purge cached node set. To make sure each
	// modules see the up-to-date node set, we need to make sure this action
	// should be taken as the first one.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		defer elapse("purge-cache", evts[len(evts)-1])()
		for _, e := range evts {
			if e.Reset == 0 {
				continue
			}
			con.nodeSetCache.Purge(e.Round + 1)
			con.tsigVerifierCache.Purge(e.Round + 1)
		}
	})
	// Register round event handler to abort previous running DKG if any.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		e := evts[len(evts)-1]
		go func() {
			defer elapse("abort-DKG", e)()
			if e.Reset > 0 {
				aborted := con.cfgModule.abortDKG(con.ctx, e.Round+1, e.Reset-1)
				con.logger.Info("DKG aborting result",
					"round", e.Round+1,
					"reset", e.Reset-1,
					"aborted", aborted)
			}
		}()
	})
	// Register round event handler to update BA and BC modules.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		defer elapse("append-config", evts[len(evts)-1])()
		// Always updates newer configs to the later modules first in the data
		// flow.
		if err := con.bcModule.notifyRoundEvents(evts); err != nil {
			panic(err)
		}
		if err := con.baMgr.notifyRoundEvents(evts); err != nil {
			panic(err)
		}
	})
	// Register round event handler to reset DKG if the DKG set for next round
	// failed to setup.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		e := evts[len(evts)-1]
		defer elapse("reset-DKG", e)()
		nextRound := e.Round + 1
		if nextRound < DKGDelayRound {
			return
		}
		curNotarySet, err := con.nodeSetCache.GetNotarySet(e.Round)
		if err != nil {
			con.logger.Error("Error getting notary set when proposing CRS",
				"round", e.Round,
				"error", err)
			return
		}
		if _, exist := curNotarySet[con.ID]; !exist {
			return
		}
		con.event.RegisterHeight(e.NextDKGResetHeight(), func(uint64) {
			if ok, _ := utils.IsDKGValid(
				con.gov, con.logger, nextRound, e.Reset); ok {
				return
			}
			// Aborting all previous running DKG protocol instance if any.
			go con.runCRS(e.Round, utils.Rehash(e.CRS, uint(e.Reset+1)), true)
		})
	})
	// Register round event handler to propose new CRS.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		// We don't have to propose new CRS during DKG reset, the reset of DKG
		// would be done by the notary set in previous round.
		e := evts[len(evts)-1]
		defer elapse("propose-CRS", e)()
		if e.Reset != 0 || e.Round < DKGDelayRound {
			return
		}
		if curNotarySet, err := con.nodeSetCache.GetNotarySet(e.Round); err != nil {
			con.logger.Error("Error getting notary set when proposing CRS",
				"round", e.Round,
				"error", err)
		} else {
			if _, exist := curNotarySet[con.ID]; !exist {
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
				go con.runCRS(e.Round, e.CRS, false)
			})
		}
	})
	// Touch nodeSetCache for next round.
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		e := evts[len(evts)-1]
		defer elapse("touch-NodeSetCache", e)()
		con.event.RegisterHeight(e.NextTouchNodeSetCacheHeight(), func(uint64) {
			if e.Reset == 0 {
				return
			}
			go func() {
				nextRound := e.Round + 1
				if err := con.nodeSetCache.Touch(nextRound); err != nil {
					con.logger.Warn("Failed to update nodeSetCache",
						"round", nextRound,
						"error", err)
				}
			}()
		})
	})
	con.roundEvent.Register(func(evts []utils.RoundEventParam) {
		e := evts[len(evts)-1]
		if e.Reset != 0 {
			return
		}
		defer elapse("touch-DKGCache", e)()
		go func() {
			if _, err :=
				con.tsigVerifierCache.Update(e.Round); err != nil {
				con.logger.Warn("Failed to update tsig cache",
					"round", e.Round,
					"error", err)
			}
		}()
		go func() {
			threshold := utils.GetDKGThreshold(
				utils.GetConfigWithPanic(con.gov, e.Round, con.logger))
			// Restore group public key.
			con.logger.Debug(
				"Calling Governance.DKGMasterPublicKeys for recoverDKGInfo",
				"round", e.Round)
			con.logger.Debug(
				"Calling Governance.DKGComplaints for recoverDKGInfo",
				"round", e.Round)
			_, qualifies, err := typesDKG.CalcQualifyNodes(
				con.gov.DKGMasterPublicKeys(e.Round),
				con.gov.DKGComplaints(e.Round),
				threshold)
			if err != nil {
				con.logger.Warn("Failed to calculate dkg set",
					"round", e.Round,
					"error", err)
				return
			}
			if _, exist := qualifies[con.ID]; !exist {
				return
			}
			if _, _, err :=
				con.cfgModule.getDKGInfo(e.Round, true); err != nil {
				con.logger.Warn("Failed to recover DKG info",
					"round", e.Round,
					"error", err)
			}
		}()
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
		defer elapse("next-round", e)()
		// Register a routine to trigger round events.
		con.event.RegisterHeight(e.NextRoundValidationHeight(),
			utils.RoundEventRetryHandlerGenerator(con.roundEvent, con.event))
		// Register a routine to register next DKG.
		con.event.RegisterHeight(e.NextDKGRegisterHeight(), func(uint64) {
			nextRound := e.Round + 1
			if nextRound < DKGDelayRound {
				con.logger.Info("Skip runDKG for round",
					"round", nextRound,
					"reset", e.Reset)
				return
			}
			go func() {
				// Normally, gov.CRS would return non-nil. Use this for in case
				// of unexpected network fluctuation and ensure the robustness.
				if !checkWithCancel(
					con.ctx, 500*time.Millisecond, checkCRS(nextRound)) {
					con.logger.Debug("unable to prepare CRS for notary set",
						"round", nextRound,
						"reset", e.Reset)
					return
				}
				nextNotarySet, err := con.nodeSetCache.GetNotarySet(nextRound)
				if err != nil {
					con.logger.Error("Error getting notary set for next round",
						"round", nextRound,
						"reset", e.Reset,
						"error", err)
					return
				}
				if _, exist := nextNotarySet[con.ID]; !exist {
					con.logger.Info("Not selected as notary set",
						"round", nextRound,
						"reset", e.Reset)
					return
				}
				con.logger.Info("Selected as notary set",
					"round", nextRound,
					"reset", e.Reset)
				nextConfig := utils.GetConfigWithPanic(con.gov, nextRound,
					con.logger)
				con.cfgModule.registerDKG(con.ctx, nextRound, e.Reset,
					utils.GetDKGThreshold(nextConfig))
				con.event.RegisterHeight(e.NextDKGPreparationHeight(),
					func(h uint64) {
						func() {
							con.dkgReady.L.Lock()
							defer con.dkgReady.L.Unlock()
							con.dkgRunning = 0
						}()
						// We want to skip some of the DKG phases when started.
						dkgCurrentHeight := h - e.NextDKGPreparationHeight()
						con.runDKG(
							nextRound, e.Reset,
							e.NextDKGPreparationHeight(), dkgCurrentHeight)
					})
			}()
		})
	})
	con.roundEvent.TriggerInitEvent()
	if initBlock != nil {
		con.event.NotifyHeight(initBlock.Position.Height)
	}
	con.baMgr.prepare()
	return
}

// Run starts running DEXON Consensus.
func (con *Consensus) Run() {
	// There may have emptys block in blockchain added by force sync.
	blocksWithoutRandomness := con.bcModule.pendingBlocksWithoutRandomness()
	// Launch BA routines.
	con.baMgr.run()
	// Launch network handler.
	con.logger.Debug("Calling Network.ReceiveChan")
	con.waitGroup.Add(1)
	go con.deliverNetworkMsg()
	con.waitGroup.Add(1)
	go con.processMsg()
	go con.processBlockLoop()
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
	con.generateBlockRandomness(blocksWithoutRandomness)
	// Sleep until dMoment come.
	time.Sleep(con.dMoment.Sub(time.Now().UTC()))
	// Take some time to bootstrap.
	time.Sleep(3 * time.Second)
	con.waitGroup.Add(1)
	go con.deliveryGuard()
	// Block until done.
	select {
	case <-con.ctx.Done():
	}
}

func (con *Consensus) generateBlockRandomness(blocks []*types.Block) {
	con.logger.Debug("Start generating block randomness", "blocks", blocks)
	isNotarySet := make(map[uint64]bool)
	for _, block := range blocks {
		if block.Position.Round < DKGDelayRound {
			continue
		}
		doRun, exist := isNotarySet[block.Position.Round]
		if !exist {
			curNotarySet, err := con.nodeSetCache.GetNotarySet(block.Position.Round)
			if err != nil {
				con.logger.Error("Error getting notary set when generate block tsig",
					"round", block.Position.Round,
					"error", err)
				continue
			}
			_, exist := curNotarySet[con.ID]
			isNotarySet[block.Position.Round] = exist
			doRun = exist
		}
		if !doRun {
			continue
		}
		go func(block *types.Block) {
			psig, err := con.cfgModule.preparePartialSignature(
				block.Position.Round, block.Hash)
			if err != nil {
				con.logger.Error("Failed to prepare partial signature",
					"block", block,
					"error", err)
			} else if err = con.signer.SignDKGPartialSignature(psig); err != nil {
				con.logger.Error("Failed to sign DKG partial signature",
					"block", block,
					"error", err)
			} else if err = con.cfgModule.processPartialSignature(psig); err != nil {
				con.logger.Error("Failed to process partial signature",
					"block", block,
					"error", err)
			} else {
				con.logger.Debug("Calling Network.BroadcastDKGPartialSignature",
					"proposer", psig.ProposerID,
					"block", block)
				con.network.BroadcastDKGPartialSignature(psig)
				sig, err := con.cfgModule.runTSig(
					block.Position.Round,
					block.Hash,
					60*time.Minute,
				)
				if err != nil {
					con.logger.Error("Failed to run Block Tsig",
						"block", block,
						"error", err)
					return
				}
				result := &types.AgreementResult{
					BlockHash:  block.Hash,
					Position:   block.Position,
					Randomness: sig.Signature[:],
				}
				con.bcModule.addBlockRandomness(block.Position, sig.Signature[:])
				con.logger.Debug("Broadcast BlockRandomness",
					"block", block,
					"result", result)
				con.network.BroadcastAgreementResult(result)
				if err := con.deliverFinalizedBlocks(); err != nil {
					con.logger.Error("Failed to deliver finalized block",
						"error", err)
				}
			}
		}(block)
	}
}

// runDKG starts running DKG protocol.
func (con *Consensus) runDKG(
	round, reset, dkgBeginHeight, dkgHeight uint64) {
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
		if err :=
			con.cfgModule.runDKG(
				round, reset,
				con.event, dkgBeginHeight, dkgHeight); err != nil {
			con.logger.Error("Failed to runDKG", "error", err)
		}
	}()
}

func (con *Consensus) runCRS(round uint64, hash common.Hash, reset bool) {
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
		crs, err := con.cfgModule.runCRSTSig(round, hash)
		if err != nil {
			con.logger.Error("Failed to run CRS Tsig", "error", err)
		} else {
			if reset {
				con.logger.Debug("Calling Governance.ResetDKG",
					"round", round+1,
					"crs", hex.EncodeToString(crs))
				con.gov.ResetDKG(crs)
			} else {
				con.logger.Debug("Calling Governance.ProposeCRS",
					"round", round+1,
					"crs", hex.EncodeToString(crs))
				con.gov.ProposeCRS(round+1, crs)
			}
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
		var msg, peer interface{}
		select {
		case msg = <-con.priorityMsgChan:
		default:
		}
		if msg == nil {
			select {
			case message := <-con.msgChan:
				msg, peer = message.Payload, message.PeerID
			case msg = <-con.priorityMsgChan:
			case <-con.ctx.Done():
				return
			}
		}
		switch val := msg.(type) {
		case *selfAgreementResult:
			con.baMgr.touchAgreementResult((*types.AgreementResult)(val))
		case *types.Block:
			if ch, exist := func() (chan<- *types.Block, bool) {
				con.lock.RLock()
				defer con.lock.RUnlock()
				ch, e := con.baConfirmedBlock[val.Hash]
				return ch, e
			}(); exist {
				if val.IsEmpty() {
					hash, err := utils.HashBlock(val)
					if err != nil {
						con.logger.Error("Error verifying empty block hash",
							"block", val,
							"error, err")
						con.network.ReportBadPeerChan() <- peer
						continue MessageLoop
					}
					if hash != val.Hash {
						con.logger.Error("Incorrect confirmed empty block hash",
							"block", val,
							"hash", hash)
						con.network.ReportBadPeerChan() <- peer
						continue MessageLoop
					}
					if _, err := con.bcModule.proposeBlock(
						val.Position, time.Time{}, true); err != nil {
						con.logger.Error("Error adding empty block",
							"block", val,
							"error", err)
						con.network.ReportBadPeerChan() <- peer
						continue MessageLoop
					}
				} else {
					if !val.IsFinalized() {
						con.logger.Warn("Ignore not finalized block",
							"block", val)
						continue MessageLoop
					}
					ok, err := con.bcModule.verifyRandomness(
						val.Hash, val.Position.Round, val.Randomness)
					if err != nil {
						con.logger.Error("Error verifying confirmed block randomness",
							"block", val,
							"error", err)
						con.network.ReportBadPeerChan() <- peer
						continue MessageLoop
					}
					if !ok {
						con.logger.Error("Incorrect confirmed block randomness",
							"block", val)
						con.network.ReportBadPeerChan() <- peer
						continue MessageLoop
					}
					if err := utils.VerifyBlockSignature(val); err != nil {
						con.logger.Error("VerifyBlockSignature failed",
							"block", val,
							"error", err)
						con.network.ReportBadPeerChan() <- peer
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
				if err := con.processFinalizedBlock(val); err != nil {
					con.logger.Error("Failed to process finalized block",
						"block", val,
						"error", err)
					con.network.ReportBadPeerChan() <- peer
				}
			} else {
				if err := con.preProcessBlock(val); err != nil {
					con.logger.Error("Failed to pre process block",
						"block", val,
						"error", err)
					con.network.ReportBadPeerChan() <- peer
				}
			}
		case *types.Vote:
			if err := con.ProcessVote(val); err != nil {
				con.logger.Error("Failed to process vote",
					"vote", val,
					"error", err)
				con.network.ReportBadPeerChan() <- peer
			}
		case *types.AgreementResult:
			if err := con.ProcessAgreementResult(val); err != nil {
				con.logger.Error("Failed to process agreement result",
					"result", val,
					"error", err)
				con.network.ReportBadPeerChan() <- peer
			}
		case *typesDKG.PrivateShare:
			if err := con.cfgModule.processPrivateShare(val); err != nil {
				con.logger.Error("Failed to process private share",
					"error", err)
				con.network.ReportBadPeerChan() <- peer
			}

		case *typesDKG.PartialSignature:
			if err := con.cfgModule.processPartialSignature(val); err != nil {
				con.logger.Error("Failed to process partial signature",
					"error", err)
				con.network.ReportBadPeerChan() <- peer
			}
		}
	}
}

// ProcessVote is the entry point to submit ont vote to a Consensus instance.
func (con *Consensus) ProcessVote(vote *types.Vote) (err error) {
	err = con.baMgr.processVote(vote)
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
	if err := con.bcModule.processAgreementResult(rand); err != nil {
		con.baMgr.untouchAgreementResult(rand)
		if err == ErrSkipButNoError {
			return nil
		}
		return err
	}
	// Syncing BA Module.
	if err := con.baMgr.processAgreementResult(rand); err != nil {
		con.baMgr.untouchAgreementResult(rand)
		return err
	}

	con.logger.Debug("Rebroadcast AgreementResult",
		"result", rand)
	con.network.BroadcastAgreementResult(rand)

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

func (con *Consensus) processFinalizedBlock(b *types.Block) (err error) {
	if b.Position.Round < DKGDelayRound {
		return
	}
	if err = utils.VerifyBlockSignature(b); err != nil {
		return
	}
	verifier, ok, err := con.tsigVerifierCache.UpdateAndGet(b.Position.Round)
	if err != nil {
		return
	}
	if !ok {
		err = ErrCannotVerifyBlockRandomness
		return
	}
	if !verifier.VerifySignature(b.Hash, crypto.Signature{
		Type:      "bls",
		Signature: b.Randomness,
	}) {
		err = ErrIncorrectBlockRandomness
		return
	}
	err = con.baMgr.processFinalizedBlock(b)
	if err == nil && con.debugApp != nil {
		con.debugApp.BlockReceived(b.Hash)
	}
	return
}

func (con *Consensus) deliveryGuard() {
	defer con.waitGroup.Done()
	select {
	case <-con.ctx.Done():
	case <-time.After(con.dMoment.Sub(time.Now())):
	}
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
			con.logger.Error("No blocks delivered for too long", "ID", con.ID)
			panic(fmt.Errorf("No blocks delivered for too long"))
		}
	}
}

// deliverBlock deliver a block to application layer.
func (con *Consensus) deliverBlock(b *types.Block) {
	select {
	case con.resetDeliveryGuardTicker <- struct{}{}:
	default:
	}
	if err := con.db.PutBlock(*b); err != nil {
		panic(err)
	}
	if err := con.db.PutCompactionChainTipInfo(b.Hash,
		b.Position.Height); err != nil {
		panic(err)
	}
	con.logger.Debug("Calling Application.BlockDelivered", "block", b)
	con.app.BlockDelivered(b.Hash, b.Position, common.CopyBytes(b.Randomness))
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
		con.event.NotifyHeight(b.Position.Height)
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

// PrepareBlock would setup header fields of block based on its ProposerID.
func (con *Consensus) proposeBlock(position types.Position) (
	*types.Block, error) {
	b, err := con.bcModule.proposeBlock(position, time.Now().UTC(), false)
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
