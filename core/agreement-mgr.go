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
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors returned from BA modules
var (
	ErrPreviousRoundIsNotFinished = errors.New("previous round is not finished")
)

// genValidLeader generate a validLeader function for agreement modules.
func genValidLeader(
	mgr *agreementMgr) func(*types.Block) (bool, error) {
	return func(block *types.Block) (bool, error) {
		if block.Timestamp.After(time.Now()) {
			return false, nil
		}
		if err := mgr.lattice.SanityCheck(block); err != nil {
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
	beginTime     time.Time
	numChains     uint32
	roundInterval time.Duration
	notarySetSize uint32
	lambdaBA      time.Duration
	crs           common.Hash
}

type baRoundSetting struct {
	chainID   uint32
	notarySet map[types.NodeID]struct{}
	agr       *agreement
	recv      *consensusBAReceiver
	ticker    Ticker
	crs       common.Hash
}

type agreementMgr struct {
	// TODO(mission): unbound Consensus instance from this module.
	con           *Consensus
	ID            types.NodeID
	app           Application
	gov           Governance
	network       Network
	logger        common.Logger
	cache         *utils.NodeSetCache
	auth          *Authenticator
	lattice       *Lattice
	ctx           context.Context
	lastEndTime   time.Time
	configs       []*agreementMgrConfig
	baModules     []*agreement
	waitGroup     sync.WaitGroup
	pendingVotes  map[uint64][]*types.Vote
	pendingBlocks map[uint64][]*types.Block

	// This lock should be used when attempting to:
	//  - add a new baModule.
	//  - remove all baModules when stopping. In this case, the cleaner need
	//    to wait for all routines runnning baModules finished.
	//  - access a method of baModule.
	//  - append a config from new round.
	// The routine running corresponding baModule, however, doesn't have to
	// acquire this lock.
	lock sync.RWMutex
}

func newAgreementMgr(con *Consensus, dMoment time.Time) *agreementMgr {
	return &agreementMgr{
		con:         con,
		ID:          con.ID,
		app:         con.app,
		gov:         con.gov,
		network:     con.network,
		logger:      con.logger,
		cache:       con.nodeSetCache,
		auth:        con.authModule,
		lattice:     con.lattice,
		ctx:         con.ctx,
		lastEndTime: dMoment,
	}
}

func (mgr *agreementMgr) appendConfig(
	round uint64, config *types.Config, crs common.Hash) (err error) {
	mgr.lock.Lock()
	defer mgr.lock.Unlock()
	// TODO(mission): initiate this module from some round > 0.
	if round != uint64(len(mgr.configs)) {
		return ErrRoundNotIncreasing
	}
	newConfig := &agreementMgrConfig{
		beginTime:     mgr.lastEndTime,
		numChains:     config.NumChains,
		roundInterval: config.RoundInterval,
		notarySetSize: config.NotarySetSize,
		lambdaBA:      config.LambdaBA,
		crs:           crs,
	}
	mgr.configs = append(mgr.configs, newConfig)
	mgr.lastEndTime = mgr.lastEndTime.Add(config.RoundInterval)
	// Create baModule for newly added chain.
	for i := uint32(len(mgr.baModules)); i < newConfig.numChains; i++ {
		// Prepare modules.
		recv := &consensusBAReceiver{
			consensus:     mgr.con,
			chainID:       i,
			restartNotary: make(chan bool, 1),
		}
		agrModule := newAgreement(
			mgr.con.ID,
			recv,
			newLeaderSelector(genValidLeader(mgr), mgr.logger),
			mgr.auth)
		// Hacky way to make agreement module self contained.
		recv.agreementModule = agrModule
		mgr.baModules = append(mgr.baModules, agrModule)
		go mgr.runBA(round, i)
	}
	return nil
}

func (mgr *agreementMgr) processVote(v *types.Vote) error {
	v = v.Clone()
	mgr.lock.RLock()
	defer mgr.lock.RUnlock()
	if v.Position.ChainID >= uint32(len(mgr.baModules)) {
		mgr.logger.Error("Process vote for unknown chain to BA",
			"position", &v.Position,
			"baChain", len(mgr.baModules),
			"baRound", len(mgr.configs))
		return utils.ErrInvalidChainID
	}
	return mgr.baModules[v.Position.ChainID].processVote(v)
}

func (mgr *agreementMgr) processBlock(b *types.Block) error {
	mgr.lock.RLock()
	defer mgr.lock.RUnlock()
	if b.Position.ChainID >= uint32(len(mgr.baModules)) {
		mgr.logger.Error("Process block for unknown chain to BA",
			"position", &b.Position,
			"baChain", len(mgr.baModules),
			"baRound", len(mgr.configs))
		return utils.ErrInvalidChainID
	}
	return mgr.baModules[b.Position.ChainID].processBlock(b)
}

func (mgr *agreementMgr) processAgreementResult(
	result *types.AgreementResult) error {
	mgr.lock.RLock()
	defer mgr.lock.RUnlock()
	if result.Position.ChainID >= uint32(len(mgr.baModules)) {
		mgr.logger.Error("Process unknown result for unknown chain to BA",
			"position", &result.Position,
			"baChain", len(mgr.baModules),
			"baRound", len(mgr.configs))
		return utils.ErrInvalidChainID
	}
	agreement := mgr.baModules[result.Position.ChainID]
	aID := agreement.agreementID()
	if isStop(aID) {
		return nil
	}
	if result.Position.Newer(&aID) {
		mgr.logger.Info("Syncing BA", "position", &result.Position)
		nodes, err := mgr.cache.GetNodeSet(result.Position.Round)
		if err != nil {
			return err
		}
		mgr.logger.Debug("Calling Network.PullBlocks for syncing BA",
			"hash", result.BlockHash)
		mgr.network.PullBlocks(common.Hashes{result.BlockHash})
		mgr.logger.Debug("Calling Governance.CRS", "round", result.Position.Round)
		crs := mgr.gov.CRS(result.Position.Round)
		nIDs := nodes.GetSubSet(
			int(mgr.gov.Configuration(result.Position.Round).NotarySetSize),
			types.NewNotarySetTarget(crs, result.Position.ChainID))
		for _, vote := range result.Votes {
			agreement.processVote(&vote)
		}
		agreement.restart(nIDs, result.Position, crs)
	}
	return nil
}

func (mgr *agreementMgr) stop() {
	// Stop all running agreement modules.
	func() {
		mgr.lock.Lock()
		defer mgr.lock.Unlock()
		for _, agr := range mgr.baModules {
			agr.stop()
		}
	}()
	// Block until all routines are done.
	mgr.waitGroup.Wait()
}

func (mgr *agreementMgr) runBA(initRound uint64, chainID uint32) {
	mgr.waitGroup.Add(1)
	defer mgr.waitGroup.Done()
	// Acquire agreement module.
	agr, recv := func() (*agreement, *consensusBAReceiver) {
		mgr.lock.RLock()
		defer mgr.lock.RUnlock()
		agr := mgr.baModules[chainID]
		return agr, agr.data.recv.(*consensusBAReceiver)
	}()
	// These are round based variables.
	var (
		currentRound uint64
		nextRound    = initRound
		setting      = baRoundSetting{
			chainID: chainID,
			agr:     agr,
			recv:    recv,
		}
		roundBeginTime time.Time
		roundEndTime   time.Time
		tickDuration   time.Duration
	)

	// Check if this routine needs to awake in this round and prepare essential
	// variables when yes.
	checkRound := func() (awake bool) {
		defer func() {
			currentRound = nextRound
			nextRound++
		}()
		// Wait until the configuartion for next round is ready.
		var config *agreementMgrConfig
		for {
			config = func() *agreementMgrConfig {
				mgr.lock.RLock()
				defer mgr.lock.RUnlock()
				if nextRound < uint64(len(mgr.configs)) {
					return mgr.configs[nextRound]
				}
				return nil
			}()
			if config != nil {
				break
			} else {
				mgr.logger.Info("round is not ready", "round", nextRound)
				time.Sleep(1 * time.Second)
			}
		}
		// Set next checkpoint.
		roundBeginTime = config.beginTime
		roundEndTime = config.beginTime.Add(config.roundInterval)
		// Check if this chain handled by this routine included in this round.
		if chainID >= config.numChains {
			return false
		}
		// Check if this node in notary set of this chain in this round.
		nodeSet, err := mgr.cache.GetNodeSet(nextRound)
		if err != nil {
			panic(err)
		}
		setting.crs = config.crs
		setting.notarySet = nodeSet.GetSubSet(
			int(config.notarySetSize),
			types.NewNotarySetTarget(config.crs, chainID))
		_, awake = setting.notarySet[mgr.ID]
		// Setup ticker
		if tickDuration != config.lambdaBA {
			if setting.ticker != nil {
				setting.ticker.Stop()
			}
			setting.ticker = newTicker(mgr.gov, nextRound, TickerBA)
			tickDuration = config.lambdaBA
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
		now := time.Now().UTC()
		if !checkRound() {
			if now.After(roundEndTime) {
				// That round is passed.
				continue Loop
			}
			// Sleep until next checkpoint.
			select {
			case <-mgr.ctx.Done():
				break Loop
			case <-time.After(roundEndTime.Sub(now)):
				continue Loop
			}
		}
		// Sleep until round begin. Here a biased round begin time would be
		// used instead of the one in config. The reason it to disperse the load
		// of fullnodes to verify confirmed blocks from each chain.
		if now.Before(pickBiasedTime(roundBeginTime, 4*tickDuration)) {
			select {
			case <-mgr.ctx.Done():
				break Loop
			case <-time.After(roundBeginTime.Sub(now)):
			}
			// Clean the tick channel after awake: the tick would be queued in
			// channel, thus the first few ticks would not tick on expected
			// interval.
			<-setting.ticker.Tick()
			<-setting.ticker.Tick()
		}
		// Run BA for this round.
		recv.round = currentRound
		recv.changeNotaryTime = roundEndTime
		recv.restartNotary <- false
		if err := mgr.baRoutineForOneRound(&setting); err != nil {
			mgr.logger.Error("BA routine failed",
				"error", err,
				"nodeID", mgr.ID,
				"chain", chainID)
			break Loop
		}
	}
}

func (mgr *agreementMgr) baRoutineForOneRound(
	setting *baRoundSetting) (err error) {
	agr := setting.agr
	recv := setting.recv
Loop:
	for {
		select {
		case <-mgr.ctx.Done():
			break Loop
		default:
		}
		select {
		case newNotary := <-recv.restartNotary:
			if newNotary {
				// This round is finished.
				break Loop
			}
			oldPos := agr.agreementID()
			var nextHeight uint64
			for {
				nextHeight, err = mgr.lattice.NextHeight(recv.round, setting.chainID)
				if err != nil {
					panic(err)
				}
				if isStop(oldPos) || nextHeight == 0 {
					break
				}
				if nextHeight > oldPos.Height {
					break
				}
				time.Sleep(100 * time.Millisecond)
				mgr.logger.Debug("Lattice not ready!!!",
					"old", &oldPos, "next", nextHeight)
			}
			nextPos := types.Position{
				Round:   recv.round,
				ChainID: setting.chainID,
				Height:  nextHeight,
			}
			agr.restart(setting.notarySet, nextPos, setting.crs)
		default:
		}
		if agr.pullVotes() {
			pos := agr.agreementID()
			mgr.logger.Debug("Calling Network.PullVotes for syncing votes",
				"position", &pos)
			mgr.network.PullVotes(pos)
		}
		if err = agr.nextState(); err != nil {
			mgr.logger.Error("Failed to proceed to next state",
				"nodeID", mgr.ID.String(),
				"error", err)
			break Loop
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
