// Copyright 2019 The dexon-consensus Authors
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

package utils

import (
	"context"
	"fmt"
	"sync"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

// ErrUnmatchedBlockHeightWithConfig is for invalid parameters for NewRoundEvent.
type ErrUnmatchedBlockHeightWithConfig struct {
	round       uint64
	reset       uint64
	blockHeight uint64
}

func (e ErrUnmatchedBlockHeightWithConfig) Error() string {
	return fmt.Sprintf("unsynced block height and cfg: round:%d reset:%d h:%d",
		e.round, e.reset, e.blockHeight)
}

// RoundEventParam defines the parameters passed to event handlers of
// RoundEvent.
type RoundEventParam struct {
	// 'Round' of next checkpoint, might be identical to previous checkpoint.
	Round uint64
	// the count of reset DKG for 'Round+1'.
	Reset uint64
	// the begin block height of this event, the end block height of this event
	// would be BeginHeight + config.RoundLength.
	BeginHeight uint64
	// The configuration for 'Round'.
	Config *types.Config
	// The CRS for 'Round'.
	CRS common.Hash
}

// NextRoundValidationHeight returns the height to check if the next round is
// ready.
func (e RoundEventParam) NextRoundValidationHeight() uint64 {
	return e.BeginHeight + e.Config.RoundLength*9/10
}

// NextCRSProposingHeight returns the height to propose CRS for next round.
func (e RoundEventParam) NextCRSProposingHeight() uint64 {
	return e.BeginHeight + e.Config.RoundLength/2
}

// NextDKGPreparationHeight returns the height to prepare DKG set for next
// round.
func (e RoundEventParam) NextDKGPreparationHeight() uint64 {
	return e.BeginHeight + e.Config.RoundLength*2/3
}

// NextRoundHeight returns the height of the beginning of next round.
func (e RoundEventParam) NextRoundHeight() uint64 {
	return e.BeginHeight + e.Config.RoundLength
}

// NextTouchNodeSetCacheHeight returns the height to touch the node set cache.
func (e RoundEventParam) NextTouchNodeSetCacheHeight() uint64 {
	return e.BeginHeight + e.Config.RoundLength/2
}

// NextDKGResetHeight returns the height to reset DKG for next period.
func (e RoundEventParam) NextDKGResetHeight() uint64 {
	return e.BeginHeight + e.Config.RoundLength*85/100
}

// NextDKGRegisterHeight returns the height to register DKG.
func (e RoundEventParam) NextDKGRegisterHeight() uint64 {
	return e.BeginHeight + e.Config.RoundLength/2
}

// RoundEndHeight returns the round ending height of this round event.
func (e RoundEventParam) RoundEndHeight() uint64 {
	return e.BeginHeight + e.Config.RoundLength
}

func (e RoundEventParam) String() string {
	return fmt.Sprintf("roundEvtParam{Round:%d Reset:%d Height:%d}",
		e.Round,
		e.Reset,
		e.BeginHeight)
}

// roundEventFn defines the fingerprint of handlers of round events.
type roundEventFn func([]RoundEventParam)

// governanceAccessor is a subset of core.Governance to break the dependency
// between core and utils package.
type governanceAccessor interface {
	// Configuration returns the configuration at a given round.
	// Return the genesis configuration if round == 0.
	Configuration(round uint64) *types.Config

	// CRS returns the CRS for a given round.
	// Return the genesis CRS if round == 0.
	CRS(round uint64) common.Hash

	// DKGComplaints gets all the DKGComplaints of round.
	DKGComplaints(round uint64) []*typesDKG.Complaint

	// DKGMasterPublicKeys gets all the DKGMasterPublicKey of round.
	DKGMasterPublicKeys(round uint64) []*typesDKG.MasterPublicKey

	// IsDKGFinal checks if DKG is final.
	IsDKGFinal(round uint64) bool

	// IsDKGSuccess checks if DKG is success.
	IsDKGSuccess(round uint64) bool

	// DKGResetCount returns the reset count for DKG of given round.
	DKGResetCount(round uint64) uint64

	// Get the begin height of a round.
	GetRoundHeight(round uint64) uint64
}

// RoundEventRetryHandlerGenerator generates a handler to common.Event, which
// would register itself to retry next round validation if round event is not
// triggered.
func RoundEventRetryHandlerGenerator(
	rEvt *RoundEvent, hEvt *common.Event) func(uint64) {
	var hEvtHandler func(uint64)
	hEvtHandler = func(h uint64) {
		if rEvt.ValidateNextRound(h) == 0 {
			// Retry until at least one round event is triggered.
			hEvt.RegisterHeight(h+1, hEvtHandler)
		}
	}
	return hEvtHandler
}

// RoundEvent would be triggered when either:
// - the next DKG set setup is ready.
// - the next DKG set setup is failed, and previous DKG set already reset the
//   CRS.
type RoundEvent struct {
	gov                     governanceAccessor
	logger                  common.Logger
	lock                    sync.Mutex
	handlers                []roundEventFn
	config                  RoundBasedConfig
	lastTriggeredRound      uint64
	lastTriggeredResetCount uint64
	roundShift              uint64
	gpkInvalid              bool
	ctx                     context.Context
	ctxCancel               context.CancelFunc
}

// NewRoundEvent creates an RoundEvent instance.
func NewRoundEvent(parentCtx context.Context, gov governanceAccessor,
	logger common.Logger, initPos types.Position, roundShift uint64) (
	*RoundEvent, error) {
	// We need to generate valid ending block height of this round (taken
	// DKG reset count into consideration).
	logger.Info("new RoundEvent", "position", initPos, "shift", roundShift)
	initConfig := GetConfigWithPanic(gov, initPos.Round, logger)
	e := &RoundEvent{
		gov:                gov,
		logger:             logger,
		lastTriggeredRound: initPos.Round,
		roundShift:         roundShift,
	}
	e.ctx, e.ctxCancel = context.WithCancel(parentCtx)
	e.config = RoundBasedConfig{}
	e.config.SetupRoundBasedFields(initPos.Round, initConfig)
	e.config.SetRoundBeginHeight(GetRoundHeight(gov, initPos.Round))
	// Make sure the DKG reset count in current governance can cover the initial
	// block height.
	if initPos.Height >= types.GenesisHeight {
		resetCount := gov.DKGResetCount(initPos.Round + 1)
		remains := resetCount
		for ; remains > 0 && !e.config.Contains(initPos.Height); remains-- {
			e.config.ExtendLength()
		}
		if !e.config.Contains(initPos.Height) {
			return nil, ErrUnmatchedBlockHeightWithConfig{
				round:       initPos.Round,
				reset:       resetCount,
				blockHeight: initPos.Height,
			}
		}
		e.lastTriggeredResetCount = resetCount - remains
	}
	return e, nil
}

// Register a handler to be called when new round is confirmed or new DKG reset
// is detected.
//
// The earlier registered handler has higher priority.
func (e *RoundEvent) Register(h roundEventFn) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.handlers = append(e.handlers, h)
}

// TriggerInitEvent triggers event from the initial setting.
func (e *RoundEvent) TriggerInitEvent() {
	e.lock.Lock()
	defer e.lock.Unlock()
	events := []RoundEventParam{RoundEventParam{
		Round:       e.lastTriggeredRound,
		Reset:       e.lastTriggeredResetCount,
		BeginHeight: e.config.LastPeriodBeginHeight(),
		CRS:         GetCRSWithPanic(e.gov, e.lastTriggeredRound, e.logger),
		Config:      GetConfigWithPanic(e.gov, e.lastTriggeredRound, e.logger),
	}}
	for _, h := range e.handlers {
		h(events)
	}
}

// ValidateNextRound validate if the DKG set for next round is ready to go or
// failed to setup, all registered handlers would be called once some decision
// is made on chain.
//
// The count of triggered events would be returned.
func (e *RoundEvent) ValidateNextRound(blockHeight uint64) (count uint) {
	// To make triggers continuous and sequential, the next validation should
	// wait for previous one finishing. That's why I use mutex here directly.
	var events []RoundEventParam
	e.lock.Lock()
	defer e.lock.Unlock()
	e.logger.Trace("ValidateNextRound",
		"height", blockHeight,
		"round", e.lastTriggeredRound,
		"count", e.lastTriggeredResetCount)
	defer func() {
		count = uint(len(events))
		if count == 0 {
			return
		}
		for _, h := range e.handlers {
			// To make sure all handlers receive triggers sequentially, we can't
			// raise go routines here.
			h(events)
		}
	}()
	var (
		triggered   bool
		param       RoundEventParam
		beginHeight = blockHeight
		startRound  = e.lastTriggeredRound
	)
	for {
		param, triggered = e.check(beginHeight, startRound)
		if !triggered {
			break
		}
		events = append(events, param)
		beginHeight = param.BeginHeight
	}
	return
}

func (e *RoundEvent) check(blockHeight, startRound uint64) (
	param RoundEventParam, triggered bool) {
	defer func() {
		if !triggered {
			return
		}
		// A simple assertion to make sure we didn't pick the wrong round.
		if e.config.RoundID() != e.lastTriggeredRound {
			panic(fmt.Errorf("Triggered round not matched: %d, %d",
				e.config.RoundID(), e.lastTriggeredRound))
		}
		param.Round = e.lastTriggeredRound
		param.Reset = e.lastTriggeredResetCount
		param.BeginHeight = e.config.LastPeriodBeginHeight()
		param.CRS = GetCRSWithPanic(e.gov, e.lastTriggeredRound, e.logger)
		param.Config = GetConfigWithPanic(e.gov, e.lastTriggeredRound, e.logger)
		e.logger.Info("New RoundEvent triggered",
			"round", e.lastTriggeredRound,
			"reset", e.lastTriggeredResetCount,
			"begin-height", e.config.LastPeriodBeginHeight(),
			"crs", param.CRS.String()[:6],
		)
	}()
	nextRound := e.lastTriggeredRound + 1
	if nextRound >= startRound+e.roundShift {
		// Avoid access configuration newer than last confirmed one over
		// 'roundShift' rounds. Fullnode might crash if we access it before it
		// knows.
		return
	}
	nextCfg := GetConfigWithPanic(e.gov, nextRound, e.logger)
	resetCount := e.gov.DKGResetCount(nextRound)
	if resetCount > e.lastTriggeredResetCount {
		e.lastTriggeredResetCount++
		e.config.ExtendLength()
		e.gpkInvalid = false
		triggered = true
		return
	}
	if e.gpkInvalid {
		// We know that DKG already failed, now wait for the DKG set from
		// previous round to reset DKG and don't have to reconstruct the
		// group public key again.
		return
	}
	if nextRound >= dkgDelayRound {
		var ok bool
		ok, e.gpkInvalid = IsDKGValid(
			e.gov, e.logger, nextRound, e.lastTriggeredResetCount)
		if !ok {
			return
		}
	}
	// The DKG set for next round is well prepared.
	e.lastTriggeredRound = nextRound
	e.lastTriggeredResetCount = 0
	e.gpkInvalid = false
	rCfg := RoundBasedConfig{}
	rCfg.SetupRoundBasedFields(nextRound, nextCfg)
	rCfg.AppendTo(e.config)
	e.config = rCfg
	triggered = true
	return
}

// Stop the event source and block until last trigger returns.
func (e *RoundEvent) Stop() {
	e.ctxCancel()
}

// LastPeriod returns block height related info of the last period, including
// begin height and round length.
func (e *RoundEvent) LastPeriod() (begin uint64, length uint64) {
	e.lock.Lock()
	defer e.lock.Unlock()
	begin = e.config.LastPeriodBeginHeight()
	length = e.config.RoundEndHeight() - e.config.LastPeriodBeginHeight()
	return
}
