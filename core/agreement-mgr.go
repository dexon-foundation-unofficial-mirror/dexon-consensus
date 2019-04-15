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
	"errors"
	"math"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors returned from BA modules
var (
	ErrPreviousRoundIsNotFinished = errors.New("previous round is not finished")
	ErrRoundOutOfRange            = errors.New("round out of range")
	ErrInvalidBlock               = errors.New("invalid block")
	ErrNoValidLeader              = errors.New("no valid leader")
	ErrIncorrectCRSSignature      = errors.New("incorrect CRS signature")
	ErrBlockTooOld                = errors.New("block too old")
)

const maxResultCache = 100
const settingLimit = 3

// genValidLeader generate a validLeader function for agreement modules.
func genValidLeader(
	mgr *agreementMgr) validLeaderFn {
	return func(block *types.Block, crs common.Hash) (bool, error) {
		if block.Timestamp.After(time.Now()) {
			return false, nil
		}
		if block.Position.Round >= DKGDelayRound {
			if mgr.recv.npks == nil {
				return false, nil
			}
			if block.Position.Round > mgr.recv.npks.Round {
				return false, nil
			}
			if block.Position.Round < mgr.recv.npks.Round {
				return false, ErrBlockTooOld
			}
		}
		if !utils.VerifyCRSSignature(block, crs, mgr.recv.npks) {
			return false, ErrIncorrectCRSSignature
		}
		if err := mgr.bcModule.sanityCheck(block); err != nil {
			if err == ErrRetrySanityCheckLater {
				return false, nil
			}
			return false, err
		}
		mgr.logger.Debug("Calling Application.VerifyBlock", "block", block)
		switch mgr.app.VerifyBlock(block) {
		case types.VerifyInvalidBlock:
			return false, ErrInvalidBlock
		case types.VerifyRetryLater:
			return false, nil
		default:
		}
		return true, nil
	}
}

type agreementMgrConfig struct {
	utils.RoundBasedConfig

	notarySetSize uint32
	lambdaBA      time.Duration
	crs           common.Hash
}

func (c *agreementMgrConfig) from(
	round uint64, config *types.Config, crs common.Hash) {
	c.notarySetSize = config.NotarySetSize
	c.lambdaBA = config.LambdaBA
	c.crs = crs
	c.SetupRoundBasedFields(round, config)
}

func newAgreementMgrConfig(prev agreementMgrConfig, config *types.Config,
	crs common.Hash) (c agreementMgrConfig) {
	c = agreementMgrConfig{}
	c.from(prev.RoundID()+1, config, crs)
	c.AppendTo(prev.RoundBasedConfig)
	return
}

type baRoundSetting struct {
	round     uint64
	dkgSet    map[types.NodeID]struct{}
	threshold int
	ticker    Ticker
	crs       common.Hash
}

type agreementMgr struct {
	// TODO(mission): unbound Consensus instance from this module.
	con               *Consensus
	ID                types.NodeID
	app               Application
	gov               Governance
	network           Network
	logger            common.Logger
	cache             *utils.NodeSetCache
	signer            *utils.Signer
	bcModule          *blockChain
	ctx               context.Context
	configs           []agreementMgrConfig
	baModule          *agreement
	recv              *consensusBAReceiver
	processedBAResult map[types.Position]struct{}
	voteFilter        *utils.VoteFilter
	settingCache      *lru.Cache
	curRoundSetting   *baRoundSetting
	waitGroup         sync.WaitGroup
	isRunning         bool
	lock              sync.RWMutex
}

func newAgreementMgr(con *Consensus) (mgr *agreementMgr, err error) {
	settingCache, _ := lru.New(settingLimit)
	mgr = &agreementMgr{
		con:               con,
		ID:                con.ID,
		app:               con.app,
		gov:               con.gov,
		network:           con.network,
		logger:            con.logger,
		cache:             con.nodeSetCache,
		signer:            con.signer,
		bcModule:          con.bcModule,
		ctx:               con.ctx,
		processedBAResult: make(map[types.Position]struct{}, maxResultCache),
		voteFilter:        utils.NewVoteFilter(),
		settingCache:      settingCache,
	}
	mgr.recv = &consensusBAReceiver{
		consensus:     con,
		restartNotary: make(chan types.Position, 1),
	}
	return mgr, nil
}

func (mgr *agreementMgr) prepare() {
	round := mgr.bcModule.tipRound()
	agr := newAgreement(
		mgr.ID,
		mgr.recv,
		newLeaderSelector(genValidLeader(mgr), mgr.logger),
		mgr.signer,
		mgr.logger)
	setting := mgr.generateSetting(round)
	if setting == nil {
		mgr.logger.Warn("Unable to prepare init setting", "round", round)
		return
	}
	mgr.curRoundSetting = setting
	agr.notarySet = mgr.curRoundSetting.dkgSet
	// Hacky way to make agreement module self contained.
	mgr.recv.agreementModule = agr
	mgr.baModule = agr
	if round >= DKGDelayRound {
		if _, exist := setting.dkgSet[mgr.ID]; exist {
			mgr.logger.Debug("Preparing signer and npks.", "round", round)
			npk, signer, err := mgr.con.cfgModule.getDKGInfo(round, false)
			if err != nil {
				mgr.logger.Error("Failed to prepare signer and npks.",
					"round", round,
					"error", err)
			}
			mgr.logger.Debug("Prepared signer and npks.",
				"round", round, "signer", signer != nil, "npks", npk != nil)
		}
	}
	return
}

func (mgr *agreementMgr) run() {
	mgr.lock.Lock()
	defer mgr.lock.Unlock()
	if mgr.isRunning {
		return
	}
	mgr.isRunning = true
	mgr.waitGroup.Add(1)
	go func() {
		defer mgr.waitGroup.Done()
		mgr.runBA(mgr.bcModule.tipRound())
	}()
}

func (mgr *agreementMgr) calcLeader(
	dkgSet map[types.NodeID]struct{},
	crs common.Hash, pos types.Position) (
	types.NodeID, error) {
	nodeSet := types.NewNodeSetFromMap(dkgSet)
	leader := nodeSet.GetSubSet(1, types.NewNodeLeaderTarget(
		crs, pos.Height))
	for nID := range leader {
		return nID, nil
	}
	return types.NodeID{}, ErrNoValidLeader
}

func (mgr *agreementMgr) config(round uint64) *agreementMgrConfig {
	mgr.lock.RLock()
	defer mgr.lock.RUnlock()
	if round < mgr.configs[0].RoundID() {
		panic(ErrRoundOutOfRange)
	}
	roundIndex := round - mgr.configs[0].RoundID()
	if roundIndex >= uint64(len(mgr.configs)) {
		return nil
	}
	return &mgr.configs[roundIndex]
}

func (mgr *agreementMgr) notifyRoundEvents(evts []utils.RoundEventParam) error {
	mgr.lock.Lock()
	defer mgr.lock.Unlock()
	apply := func(e utils.RoundEventParam) error {
		if len(mgr.configs) > 0 {
			lastCfg := mgr.configs[len(mgr.configs)-1]
			if e.BeginHeight != lastCfg.RoundEndHeight() {
				return ErrInvalidBlockHeight
			}
			if lastCfg.RoundID() == e.Round {
				mgr.configs[len(mgr.configs)-1].ExtendLength()
			} else if lastCfg.RoundID()+1 == e.Round {
				mgr.configs = append(mgr.configs, newAgreementMgrConfig(
					lastCfg, e.Config, e.CRS))
			} else {
				return ErrInvalidRoundID
			}
		} else {
			c := agreementMgrConfig{}
			c.from(e.Round, e.Config, e.CRS)
			c.SetRoundBeginHeight(e.BeginHeight)
			mgr.configs = append(mgr.configs, c)
		}
		return nil
	}
	for _, e := range evts {
		if err := apply(e); err != nil {
			return err
		}
	}
	return nil
}

func (mgr *agreementMgr) checkProposer(
	round uint64, proposerID types.NodeID) error {
	if round == mgr.curRoundSetting.round {
		if _, exist := mgr.curRoundSetting.dkgSet[proposerID]; !exist {
			return ErrNotInNotarySet
		}
	} else if round == mgr.curRoundSetting.round+1 {
		setting := mgr.generateSetting(round)
		if setting == nil {
			return ErrConfigurationNotReady
		}
		if _, exist := setting.dkgSet[proposerID]; !exist {
			return ErrNotInNotarySet
		}
	}
	return nil
}

func (mgr *agreementMgr) processVote(v *types.Vote) (err error) {
	if !mgr.recv.isNotary {
		return nil
	}
	if mgr.voteFilter.Filter(v) {
		return nil
	}
	if err := mgr.checkProposer(v.Position.Round, v.ProposerID); err != nil {
		return err
	}
	if err = mgr.baModule.processVote(v); err == nil {
		mgr.baModule.updateFilter(mgr.voteFilter)
		mgr.voteFilter.AddVote(v)
	}
	if err == ErrSkipButNoError {
		err = nil
	}
	return
}

func (mgr *agreementMgr) processBlock(b *types.Block) error {
	if err := mgr.checkProposer(b.Position.Round, b.ProposerID); err != nil {
		return err
	}
	return mgr.baModule.processBlock(b)
}

func (mgr *agreementMgr) touchAgreementResult(
	result *types.AgreementResult) (first bool) {
	// DO NOT LOCK THIS FUNCTION!!!!!!!! YOU WILL REGRET IT!!!!!
	if _, exist := mgr.processedBAResult[result.Position]; !exist {
		first = true
		if len(mgr.processedBAResult) > maxResultCache {
			for k := range mgr.processedBAResult {
				// Randomly drop one element.
				delete(mgr.processedBAResult, k)
				break
			}
		}
		mgr.processedBAResult[result.Position] = struct{}{}
	}
	return
}

func (mgr *agreementMgr) untouchAgreementResult(
	result *types.AgreementResult) {
	// DO NOT LOCK THIS FUNCTION!!!!!!!! YOU WILL REGRET IT!!!!!
	delete(mgr.processedBAResult, result.Position)
}

func (mgr *agreementMgr) processAgreementResult(
	result *types.AgreementResult) error {
	aID := mgr.baModule.agreementID()
	if isStop(aID) {
		return nil
	}
	if result.Position == aID && !mgr.baModule.confirmed() {
		mgr.logger.Info("Syncing BA", "position", result.Position)
		if result.Position.Round >= DKGDelayRound {
			return mgr.baModule.processAgreementResult(result)
		}
		for key := range result.Votes {
			if err := mgr.baModule.processVote(&result.Votes[key]); err != nil {
				return err
			}
		}
	} else if result.Position.Newer(aID) {
		mgr.logger.Info("Fast syncing BA", "position", result.Position)
		if result.Position.Round < DKGDelayRound {
			mgr.logger.Debug("Calling Network.PullBlocks for fast syncing BA",
				"hash", result.BlockHash)
			mgr.network.PullBlocks(common.Hashes{result.BlockHash})
			for key := range result.Votes {
				if err := mgr.baModule.processVote(&result.Votes[key]); err != nil {
					return err
				}
			}
		}
		setting := mgr.generateSetting(result.Position.Round)
		if setting == nil {
			mgr.logger.Warn("unable to get setting", "round",
				result.Position.Round)
			return ErrConfigurationNotReady
		}
		mgr.curRoundSetting = setting
		leader, err := mgr.calcLeader(setting.dkgSet, setting.crs, result.Position)
		if err != nil {
			return err
		}
		mgr.baModule.restart(
			setting.dkgSet, setting.threshold,
			result.Position, leader, setting.crs)
		if result.Position.Round >= DKGDelayRound {
			return mgr.baModule.processAgreementResult(result)
		}
	}
	return nil
}

func (mgr *agreementMgr) processFinalizedBlock(block *types.Block) error {
	aID := mgr.baModule.agreementID()
	if block.Position.Older(aID) {
		return nil
	}
	mgr.baModule.processFinalizedBlock(block)
	return nil
}

func (mgr *agreementMgr) stop() {
	// Stop all running agreement modules.
	func() {
		mgr.lock.Lock()
		defer mgr.lock.Unlock()
		mgr.baModule.stop()
	}()
	// Block until all routines are done.
	mgr.waitGroup.Wait()
}

func (mgr *agreementMgr) generateSetting(round uint64) *baRoundSetting {
	if setting, exist := mgr.settingCache.Get(round); exist {
		return setting.(*baRoundSetting)
	}
	curConfig := mgr.config(round)
	if curConfig == nil {
		return nil
	}
	var dkgSet map[types.NodeID]struct{}
	if round >= DKGDelayRound {
		_, qualidifed, err := typesDKG.CalcQualifyNodes(
			mgr.gov.DKGMasterPublicKeys(round),
			mgr.gov.DKGComplaints(round),
			utils.GetDKGThreshold(mgr.gov.Configuration(round)),
		)
		if err != nil {
			mgr.logger.Error("Failed to get gpk", "round", round, "error", err)
			return nil
		}
		dkgSet = qualidifed
	}
	if len(dkgSet) == 0 {
		var err error
		dkgSet, err = mgr.cache.GetNotarySet(round)
		if err != nil {
			mgr.logger.Error("Failed to get notarySet", "round", round, "error", err)
			return nil
		}
	}
	setting := &baRoundSetting{
		crs:    curConfig.crs,
		dkgSet: dkgSet,
		round:  round,
		threshold: utils.GetBAThreshold(&types.Config{
			NotarySetSize: curConfig.notarySetSize}),
	}
	mgr.settingCache.Add(round, setting)
	return setting
}

func (mgr *agreementMgr) runBA(initRound uint64) {
	// These are round based variables.
	var (
		currentRound uint64
		nextRound    = initRound
		curConfig    = mgr.config(initRound)
		setting      = &baRoundSetting{}
		tickDuration time.Duration
		ticker       Ticker
	)

	// Check if this routine needs to awake in this round and prepare essential
	// variables when yes.
	checkRound := func() (isDKG bool) {
		defer func() {
			currentRound = nextRound
			nextRound++
		}()
		// Wait until the configuartion for next round is ready.
		for {
			if setting = mgr.generateSetting(nextRound); setting != nil {
				break
			} else {
				mgr.logger.Debug("Round is not ready", "round", nextRound)
				time.Sleep(1 * time.Second)
			}
		}
		_, isDKG = setting.dkgSet[mgr.ID]
		if isDKG {
			mgr.logger.Info("Selected as dkg set",
				"ID", mgr.ID,
				"round", nextRound)
		} else {
			mgr.logger.Info("Not selected as dkg set",
				"ID", mgr.ID,
				"round", nextRound)
		}
		// Setup ticker
		if tickDuration != curConfig.lambdaBA {
			if ticker != nil {
				ticker.Stop()
			}
			ticker = newTicker(mgr.gov, nextRound, TickerBA)
			tickDuration = curConfig.lambdaBA
		}
		setting.ticker = ticker
		return
	}
Loop:
	for {
		select {
		case <-mgr.ctx.Done():
			break Loop
		default:
		}
		mgr.recv.isNotary = checkRound()
		mgr.voteFilter = utils.NewVoteFilter()
		mgr.voteFilter.Position.Round = currentRound
		mgr.recv.emptyBlockHashMap = &sync.Map{}
		if currentRound >= DKGDelayRound && mgr.recv.isNotary {
			var err error
			mgr.recv.npks, mgr.recv.psigSigner, err =
				mgr.con.cfgModule.getDKGInfo(currentRound, false)
			if err != nil {
				mgr.logger.Warn("cannot get dkg info",
					"round", currentRound, "error", err)
			}
		} else {
			mgr.recv.npks = nil
			mgr.recv.psigSigner = nil
		}
		// Run BA for this round.
		mgr.recv.restartNotary <- types.Position{
			Round:  currentRound,
			Height: math.MaxUint64,
		}
		if err := mgr.baRoutineForOneRound(setting); err != nil {
			mgr.logger.Error("BA routine failed",
				"error", err,
				"nodeID", mgr.ID)
			break Loop
		}
	}
}

func (mgr *agreementMgr) baRoutineForOneRound(
	setting *baRoundSetting) (err error) {
	agr := mgr.baModule
	recv := mgr.recv
	oldPos := agr.agreementID()
	restart := func(restartPos types.Position) (breakLoop bool, err error) {
		if !isStop(restartPos) {
			if restartPos.Height+1 >= mgr.config(setting.round).RoundEndHeight() {
				for {
					select {
					case <-mgr.ctx.Done():
						break
					default:
					}
					tipRound := mgr.bcModule.tipRound()
					if tipRound > setting.round {
						break
					} else {
						mgr.logger.Debug("Waiting blockChain to change round...",
							"curRound", setting.round,
							"tipRound", tipRound)
					}
					time.Sleep(100 * time.Millisecond)
				}
				// This round is finished.
				breakLoop = true
				return
			}
			if restartPos.Older(oldPos) {
				// The restartNotary event is triggered by 'BlockConfirmed'
				// of some older block.
				return
			}
		}
		var nextHeight uint64
		var nextTime time.Time
		for {
			// Make sure we are stoppable.
			select {
			case <-mgr.ctx.Done():
				breakLoop = true
				return
			default:
			}
			nextHeight, nextTime = mgr.bcModule.nextBlock()
			if nextHeight != notReadyHeight {
				if isStop(restartPos) {
					break
				}
				if nextHeight > restartPos.Height {
					break
				}
			}
			mgr.logger.Debug("BlockChain not ready!!!",
				"old", oldPos, "restart", restartPos, "next", nextHeight)
			time.Sleep(100 * time.Millisecond)
		}
		nextPos := types.Position{
			Round:  setting.round,
			Height: nextHeight,
		}
		oldPos = nextPos
		var leader types.NodeID
		leader, err = mgr.calcLeader(setting.dkgSet, setting.crs, nextPos)
		if err != nil {
			return
		}
		time.Sleep(nextTime.Sub(time.Now()))
		setting.ticker.Restart()
		agr.restart(setting.dkgSet, setting.threshold, nextPos, leader, setting.crs)
		return
	}
Loop:
	for {
		select {
		case <-mgr.ctx.Done():
			break Loop
		default:
		}
		if agr.confirmed() {
			// Block until receive restartPos
			select {
			case restartPos := <-recv.restartNotary:
				breakLoop, err := restart(restartPos)
				if err != nil {
					return err
				}
				if breakLoop {
					break Loop
				}
			case <-mgr.ctx.Done():
				break Loop
			}
		}
		select {
		case restartPos := <-recv.restartNotary:
			breakLoop, err := restart(restartPos)
			if err != nil {
				return err
			}
			if breakLoop {
				break Loop
			}
		default:
		}
		if !mgr.recv.isNotary {
			select {
			case <-setting.ticker.Tick():
				continue Loop
			case <-mgr.ctx.Done():
				break Loop
			}
		}
		if err = agr.nextState(); err != nil {
			mgr.logger.Error("Failed to proceed to next state",
				"nodeID", mgr.ID.String(),
				"error", err)
			break Loop
		}
		if agr.pullVotes() {
			pos := agr.agreementID()
			mgr.logger.Debug("Calling Network.PullVotes for syncing votes",
				"position", pos)
			mgr.network.PullVotes(pos)
		}
		for i := 0; i < agr.clocks(); i++ {
			// Priority select for agreement.done().
			select {
			case <-agr.done():
				continue Loop
			default:
			}
			select {
			case <-agr.done():
				continue Loop
			case <-setting.ticker.Tick():
			}
		}
	}
	return nil
}
