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

package common

import (
	"container/heap"
	"sync"
)

type heightEventFn func(uint64)

type heightEvent struct {
	h  uint64
	fn heightEventFn
}

// heightEvents implements a Min-Heap structure.
type heightEvents []heightEvent

func (h heightEvents) Len() int           { return len(h) }
func (h heightEvents) Less(i, j int) bool { return h[i].h < h[j].h }
func (h heightEvents) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *heightEvents) Push(x interface{}) {
	*h = append(*h, x.(heightEvent))
}
func (h *heightEvents) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// Event implements the Observer pattern.
type Event struct {
	heightEvents     heightEvents
	heightEventsLock sync.Mutex
}

// NewEvent creates a new event instance.
func NewEvent() *Event {
	he := heightEvents{}
	heap.Init(&he)
	return &Event{
		heightEvents: he,
	}
}

// RegisterHeight to get notified on a specific height.
func (e *Event) RegisterHeight(h uint64, fn heightEventFn) {
	e.heightEventsLock.Lock()
	defer e.heightEventsLock.Unlock()
	heap.Push(&e.heightEvents, heightEvent{
		h:  h,
		fn: fn,
	})
}

// NotifyHeight and trigger function callback.
func (e *Event) NotifyHeight(h uint64) {
	fns := func() (fns []heightEventFn) {
		e.heightEventsLock.Lock()
		defer e.heightEventsLock.Unlock()
		if len(e.heightEvents) == 0 {
			return
		}
		for h >= e.heightEvents[0].h {
			he := heap.Pop(&e.heightEvents).(heightEvent)
			fns = append(fns, he.fn)
			if len(e.heightEvents) == 0 {
				return
			}
		}
		return
	}()
	for _, fn := range fns {
		fn(h)
	}
}

// Reset clears all pending event
func (e *Event) Reset() {
	e.heightEventsLock.Lock()
	defer e.heightEventsLock.Unlock()
	e.heightEvents = heightEvents{}
}
