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

package test

import (
	"sync"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type SchedulerTestSuite struct {
	suite.Suite
}

type simpleStopper struct {
	lock         sync.Mutex
	touched      map[types.ValidatorID]int
	touchedCount int
}

func newSimpleStopper(
	validators []types.ValidatorID, touchedCount int) *simpleStopper {

	touched := make(map[types.ValidatorID]int)
	for _, vID := range validators {
		touched[vID] = 0
	}
	return &simpleStopper{
		touched:      touched,
		touchedCount: touchedCount,
	}
}

func (stopper *simpleStopper) ShouldStop(vID types.ValidatorID) bool {
	stopper.lock.Lock()
	defer stopper.lock.Unlock()

	stopper.touched[vID] = stopper.touched[vID] + 1
	for _, count := range stopper.touched {
		if count < stopper.touchedCount {
			return false
		}
	}
	return true
}

type simpleHandler struct {
	count int
	vID   types.ValidatorID
}

func (handler *simpleHandler) Handle(e *Event) (events []*Event) {
	if e.ValidatorID == handler.vID {
		handler.count++
	}
	return
}

type fixedLatencyHandler struct {
	vID types.ValidatorID
}

func (handler *fixedLatencyHandler) Handle(e *Event) (events []*Event) {
	// Simulate execution time.
	time.Sleep(500 * time.Millisecond)
	return []*Event{&Event{
		ValidatorID: handler.vID,
		Time:        e.Time.Add(800 * time.Millisecond),
	}}
}

func (s *SchedulerTestSuite) TestEventSequence() {
	// This test case makes sure the event sequence is correctly increment
	// by their timestamps in 'Time' field.
	var (
		sch = NewScheduler(nil)
		req = s.Require()
	)

	req.NotNil(sch)
	now := time.Now()
	req.Nil(sch.Seed(&Event{Time: now.Add(100 * time.Second), Payload: 1}))
	req.Nil(sch.Seed(&Event{Time: now.Add(99 * time.Second), Payload: 2}))
	req.Nil(sch.Seed(&Event{Time: now.Add(98 * time.Second), Payload: 3}))
	req.Nil(sch.Seed(&Event{Time: now.Add(97 * time.Second), Payload: 4}))
	req.Nil(sch.Seed(&Event{Time: now.Add(96 * time.Second), Payload: 5}))

	req.Equal(sch.nextTick().Payload.(int), 5)
	req.Equal(sch.nextTick().Payload.(int), 4)
	req.Equal(sch.nextTick().Payload.(int), 3)
	req.Equal(sch.nextTick().Payload.(int), 2)
	req.Equal(sch.nextTick().Payload.(int), 1)
	req.Nil(sch.nextTick())
}

func (s *SchedulerTestSuite) TestBasicRound() {
	// This test case makes sure these facts:
	//  - event is dispatched by validatorID attached to each handler.
	//  - stopper can stop the execution when condition is met.
	var (
		req        = s.Require()
		validators = GenerateRandomValidatorIDs(3)
		stopper    = newSimpleStopper(validators, 2)
		sch        = NewScheduler(stopper)
		handlers   = make(map[types.ValidatorID]*simpleHandler)
	)

	for _, vID := range validators {
		handler := &simpleHandler{vID: vID}
		handlers[vID] = handler
		sch.RegisterEventHandler(vID, handler)
		req.Nil(sch.Seed(&Event{ValidatorID: vID}))
		req.Nil(sch.Seed(&Event{ValidatorID: vID}))
	}
	sch.Run(10)
	// Verify result.
	for _, h := range handlers {
		req.Equal(h.count, 2)
	}
}

func (s *SchedulerTestSuite) TestChildEvent() {
	// This test case makes sure these fields of child events are
	// assigned correctly.
	var (
		req     = s.Require()
		vID     = types.ValidatorID{Hash: common.NewRandomHash()}
		stopper = newSimpleStopper(types.ValidatorIDs{vID}, 3)
		handler = &fixedLatencyHandler{vID: vID}
		sch     = NewScheduler(stopper)
	)

	sch.RegisterEventHandler(vID, handler)
	req.Nil(sch.Seed(&Event{
		ValidatorID: vID,
		Time:        time.Now().UTC(),
	}))
	sch.Run(1)
	// Verify result.
	history := sch.CloneExecutionHistory()
	req.Len(history, 3)
	curEvent := history[0]
	for _, e := range history[1:] {
		// Make sure the time difference between events are more than
		// 1.3 second.
		req.True(e.Time.Sub(curEvent.Time) >= 1300*time.Millisecond)
		// Make sure ParentTime field is set and is equal to parent event's
		// time.
		req.Equal(e.ParentTime, curEvent.Time)
		curEvent = e
	}
}

func TestScheduler(t *testing.T) {
	suite.Run(t, new(SchedulerTestSuite))
}
