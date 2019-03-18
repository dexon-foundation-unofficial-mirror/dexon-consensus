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
	"sync/atomic"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors returned from BA modules
var (
	ErrPreviousRoundIsNotFinished = errors.New("previous round is not finished")
	ErrRoundOutOfRange            = errors.New("round out of range")
	ErrInvalidBlock               = errors.New("invalid block")
)

const maxResultCache = 100

// genValidLeader generate a validLeader function for agreement modules.
func genValidLeader(
	mgr *agreementMgr) func(*types.Block) (bool, error) {
	return func(block *types.Block) (bool, error) {
		if block.Timestamp.After(time.Now()) {
			return false, nil
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
	notarySet map[types.NodeID]struct{}
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
	initRound         uint64
	configs           []agreementMgrConfig
	baModule          *agreement
	recv              *consensusBAReceiver
	processedBAResult map[types.Position]struct{}
	voteFilter        *utils.VoteFilter
	waitGroup         sync.WaitGroup
	isRunning         bool
	lock              sync.RWMutex
}

func newAgreementMgr(con *Consensus, initRound uint64,
	initConfig agreementMgrConfig) (mgr *agreementMgr, err error) {
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
		initRound:         initRound,
		processedBAResult: make(map[types.Position]struct{}, maxResultCache),
		configs:           []agreementMgrConfig{initConfig},
		voteFilter:        utils.NewVoteFilter(),
	}
	mgr.recv = &consensusBAReceiver{
		consensus:               con,
		restartNotary:           make(chan types.Position, 1),
		roundValue:              &atomic.Value{},
		changeNotaryHeightValue: &atomic.Value{},
	}
	mgr.recv.roundValue.Store(uint64(0))
	mgr.recv.changeNotaryHeightValue.Store(uint64(0))
	agr := newAgreement(
		mgr.ID,
		mgr.recv,
		newLeaderSelector(genValidLeader(mgr), mgr.logger),
		mgr.signer,
		mgr.logger)
	// Hacky way to initialize first notarySet.
	nodes, err := mgr.cache.GetNodeSet(initRound)
	if err != nil {
		return
	}
	agr.notarySet = nodes.GetSubSet(
		int(initConfig.notarySetSize), types.NewNotarySetTarget(initConfig.crs))
	// Hacky way to make agreement module self contained.
	mgr.recv.agreementModule = agr
	mgr.baModule = agr
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
		mgr.runBA(mgr.initRound)
	}()
}

func (mgr *agreementMgr) config(round uint64) *agreementMgrConfig {
	mgr.lock.RLock()
	defer mgr.lock.RUnlock()
	if round < mgr.initRound {
		panic(ErrRoundOutOfRange)
	}
	roundIndex := round - mgr.initRound
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
				// It's not an atomic operation to update an atomic value based
				// on another. However, it's the best way so far to extend
				// length of round without refactoring.
				if mgr.recv.round() == e.Round {
					mgr.recv.changeNotaryHeightValue.Store(
						mgr.configs[len(mgr.configs)-1].RoundEndHeight())
				}
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

func (mgr *agreementMgr) processVote(v *types.Vote) (err error) {
	if mgr.voteFilter.Filter(v) {
		return nil
	}
	if err = mgr.baModule.processVote(v); err == nil {
		mgr.baModule.updateFilter(mgr.voteFilter)
	}
	return
}

func (mgr *agreementMgr) processBlock(b *types.Block) error {
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
		for key := range result.Votes {
			if err := mgr.baModule.processVote(&result.Votes[key]); err != nil {
				return err
			}
		}
	} else if result.Position.Newer(aID) {
		mgr.logger.Info("Fast syncing BA", "position", result.Position)
		nIDs, err := mgr.cache.GetNotarySet(result.Position.Round)
		if err != nil {
			return err
		}
		mgr.logger.Debug("Calling Network.PullBlocks for fast syncing BA",
			"hash", result.BlockHash)
		mgr.network.PullBlocks(common.Hashes{result.BlockHash})
		mgr.logger.Debug("Calling Governance.CRS", "round", result.Position.Round)
		crs := utils.GetCRSWithPanic(mgr.gov, result.Position.Round, mgr.logger)
		for key := range result.Votes {
			if err := mgr.baModule.processVote(&result.Votes[key]); err != nil {
				return err
			}
		}
		leader, err := mgr.cache.GetLeaderNode(result.Position)
		if err != nil {
			return err
		}
		mgr.baModule.restart(nIDs, result.Position, leader, crs)
	}
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

func (mgr *agreementMgr) runBA(initRound uint64) {
	// These are round based variables.
	var (
		currentRound uint64
		nextRound    = initRound
		curConfig    = mgr.config(initRound)
		setting      = baRoundSetting{}
		tickDuration time.Duration
	)

	// Check if this routine needs to awake in this round and prepare essential
	// variables when yes.
	checkRound := func() (isNotary bool) {
		defer func() {
			currentRound = nextRound
			nextRound++
		}()
		// Wait until the configuartion for next round is ready.
		for {
			if curConfig = mgr.config(nextRound); curConfig != nil {
				break
			} else {
				mgr.logger.Debug("round is not ready", "round", nextRound)
				time.Sleep(1 * time.Second)
			}
		}
		// Check if this node in notary set of this chain in this round.
		notarySet, err := mgr.cache.GetNotarySet(nextRound)
		if err != nil {
			panic(err)
		}
		setting.crs = curConfig.crs
		setting.notarySet = notarySet
		_, isNotary = setting.notarySet[mgr.ID]
		if isNotary {
			mgr.logger.Info("selected as notary set",
				"ID", mgr.ID,
				"round", nextRound)
		} else {
			mgr.logger.Info("not selected as notary set",
				"ID", mgr.ID,
				"round", nextRound)
		}
		// Setup ticker
		if tickDuration != curConfig.lambdaBA {
			if setting.ticker != nil {
				setting.ticker.Stop()
			}
			setting.ticker = newTicker(mgr.gov, nextRound, TickerBA)
			tickDuration = curConfig.lambdaBA
		}
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
		// Run BA for this round.
		mgr.recv.roundValue.Store(currentRound)
		mgr.recv.changeNotaryHeightValue.Store(curConfig.RoundEndHeight())
		mgr.recv.restartNotary <- types.Position{
			Round:  mgr.recv.round(),
			Height: math.MaxUint64,
		}
		mgr.voteFilter = utils.NewVoteFilter()
		if err := mgr.baRoutineForOneRound(&setting); err != nil {
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
			if restartPos.Round > oldPos.Round {
				for {
					select {
					case <-mgr.ctx.Done():
						break
					default:
					}
					tipRound := mgr.bcModule.tipRound()
					if tipRound > restartPos.Round {
						// It's a vary rare that this go routine sleeps for entire round.
						break
					} else if tipRound != restartPos.Round {
						mgr.logger.Debug("Waiting blockChain to change round...",
							"pos", restartPos)
					} else {
						break
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
			if isStop(oldPos) && nextHeight == 0 {
				break
			}
			if isStop(restartPos) {
				break
			}
			if nextHeight > restartPos.Height {
				break
			}
			mgr.logger.Debug("BlockChain not ready!!!",
				"old", oldPos, "restart", restartPos, "next", nextHeight)
			time.Sleep(100 * time.Millisecond)
		}
		nextPos := types.Position{
			Round:  recv.round(),
			Height: nextHeight,
		}
		oldPos = nextPos
		var leader types.NodeID
		leader, err = mgr.cache.GetLeaderNode(nextPos)
		if err != nil {
			return
		}
		time.Sleep(nextTime.Sub(time.Now()))
		setting.ticker.Restart()
		agr.restart(setting.notarySet, nextPos, leader, setting.crs)
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
