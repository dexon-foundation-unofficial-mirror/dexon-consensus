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
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

var (
	// ErrSchedulerAlreadyStarted means callers attempt to insert some
	// seed events after calling 'Run'.
	ErrSchedulerAlreadyStarted = fmt.Errorf("scheduler already started")
	// errNilEventWhenNotified is an internal error which means a worker routine
	// can't get an event when notified.
	errNilEventWhenNotified = fmt.Errorf("nil event when notified")
)

type schedulerHandlerRecord struct {
	handler EventHandler
	lock    sync.Mutex
}

// Scheduler is an event scheduler.
type Scheduler struct {
	events            eventQueue
	eventsLock        sync.Mutex
	history           []*Event
	historyLock       sync.RWMutex
	isStarted         bool
	handlers          map[types.NodeID]*schedulerHandlerRecord
	handlersLock      sync.RWMutex
	eventNotification chan struct{}
	ctx               context.Context
	cancelFunc        context.CancelFunc
	stopper           Stopper
}

// NewScheduler constructs an Scheduler instance.
func NewScheduler(stopper Stopper) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		events:            eventQueue{},
		history:           []*Event{},
		handlers:          make(map[types.NodeID]*schedulerHandlerRecord),
		eventNotification: make(chan struct{}, 100000),
		ctx:               ctx,
		cancelFunc:        cancel,
		stopper:           stopper,
	}
}

// Run would run the scheduler. If you need strict incrememtal execution order
// of events based on their 'Time' field, assign 'numWorkers' as 1. If you need
// faster execution, assign 'numWorkers' a larger number.
func (sch *Scheduler) Run(numWorkers int) {
	var wg sync.WaitGroup

	sch.isStarted = true
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go sch.workerRoutine(&wg)
	}
	// Blocks until all routines are finished.
	wg.Wait()
}

// Seed is used to provide the scheduler some seed events.
func (sch *Scheduler) Seed(e *Event) error {
	sch.eventsLock.Lock()
	defer sch.eventsLock.Unlock()

	if sch.isStarted {
		return ErrSchedulerAlreadyStarted
	}
	sch.addEvent(e)
	return nil
}

// RegisterEventHandler register an event handler by providing ID of
// corresponding node.
func (sch *Scheduler) RegisterEventHandler(
	nID types.NodeID,
	handler EventHandler) {

	sch.handlersLock.Lock()
	defer sch.handlersLock.Unlock()

	sch.handlers[nID] = &schedulerHandlerRecord{handler: handler}
}

// nextTick would pick the oldest event from eventQueue.
func (sch *Scheduler) nextTick() (e *Event) {
	sch.eventsLock.Lock()
	defer sch.eventsLock.Unlock()

	if len(sch.events) == 0 {
		return nil
	}
	return heap.Pop(&sch.events).(*Event)
}

// addEvent is an helper function to add events into eventQueue sorted by
// their 'Time' field.
func (sch *Scheduler) addEvent(e *Event) {
	// Perform sorted insertion.
	heap.Push(&sch.events, e)
	sch.eventNotification <- struct{}{}
}

// CloneExecutionHistory returns a cloned event execution history.
func (sch *Scheduler) CloneExecutionHistory() (cloned []*Event) {
	sch.historyLock.RLock()
	defer sch.historyLock.RUnlock()

	cloned = make([]*Event, len(sch.history))
	copy(cloned, sch.history)
	return
}

// workerRoutine is the mainloop when handling events.
func (sch *Scheduler) workerRoutine(wg *sync.WaitGroup) {
	defer wg.Done()

	handleEvent := func(e *Event) {
		// Find correspond handler record.
		hRec := func(nID types.NodeID) *schedulerHandlerRecord {
			sch.handlersLock.RLock()
			defer sch.handlersLock.RUnlock()

			return sch.handlers[nID]
		}(e.NodeID)

		newEvents := func() []*Event {
			// This lock makes sure there would be no concurrent access
			// against each handler.
			hRec.lock.Lock()
			defer hRec.lock.Unlock()

			// Handle incoming event, and record its execution time.
			beforeExecution := time.Now().UTC()
			newEvents := hRec.handler.Handle(e)
			e.ExecInterval = time.Now().UTC().Sub(beforeExecution)
			// It's safe to check status of that node under 'hRec.lock'.
			if sch.stopper.ShouldStop(e.NodeID) {
				sch.cancelFunc()
			}
			return newEvents
		}()
		// Record executed events as history.
		func() {
			sch.historyLock.Lock()
			defer sch.historyLock.Unlock()

			e.HistoryIndex = len(sch.history)
			sch.history = append(sch.history, e)
		}()
		// Include the execution interval of parent event to the expected time
		// to execute child events.
		for _, newEvent := range newEvents {
			newEvent.ParentHistoryIndex = e.HistoryIndex
			newEvent.Time = newEvent.Time.Add(e.ExecInterval)
		}
		// Add derivated events back to event queue.
		func() {
			sch.eventsLock.Lock()
			defer sch.eventsLock.Unlock()

			for _, newEvent := range newEvents {
				sch.addEvent(newEvent)
			}
		}()
	}

Done:
	for {
		// We favor scheduler-shutdown signal than other events.
		select {
		case <-sch.ctx.Done():
			break Done
		default:
		}
		// Block until new event arrival or scheduler shutdown.
		select {
		case <-sch.eventNotification:
			e := sch.nextTick()
			if e == nil {
				panic(errNilEventWhenNotified)
			}
			handleEvent(e)
		case <-sch.ctx.Done():
			break Done
		}
	}
}
