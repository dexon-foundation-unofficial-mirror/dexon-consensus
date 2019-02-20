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
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// TickerType is the type of ticker.
type TickerType int

// TickerType enum.
const (
	TickerBA TickerType = iota
	TickerDKG
	TickerCRS
)

// defaultTicker is a wrapper to implement ticker interface based on
// time.Ticker.
type defaultTicker struct {
	ticker     *time.Ticker
	tickerChan chan time.Time
	duration   time.Duration
	ctx        context.Context
	ctxCancel  context.CancelFunc
	waitGroup  sync.WaitGroup
}

// newDefaultTicker constructs an defaultTicker instance by giving an interval.
func newDefaultTicker(lambda time.Duration) *defaultTicker {
	ticker := &defaultTicker{duration: lambda}
	ticker.init()
	return ticker
}

// Tick implements Tick method of ticker interface.
func (t *defaultTicker) Tick() <-chan time.Time {
	return t.tickerChan
}

// Stop implements Stop method of ticker interface.
func (t *defaultTicker) Stop() {
	t.ticker.Stop()
	t.ctxCancel()
	t.waitGroup.Wait()
	t.ctx = nil
	t.ctxCancel = nil
	close(t.tickerChan)
	t.tickerChan = nil
}

// Restart implements Stop method of ticker interface.
func (t *defaultTicker) Restart() {
	t.Stop()
	t.init()
}

func (t *defaultTicker) init() {
	t.ticker = time.NewTicker(t.duration)
	t.tickerChan = make(chan time.Time)
	t.ctx, t.ctxCancel = context.WithCancel(context.Background())
	t.waitGroup.Add(1)
	go t.monitor()
}

func (t *defaultTicker) monitor() {
	defer t.waitGroup.Done()
loop:
	for {
		select {
		case <-t.ctx.Done():
			break loop
		case v := <-t.ticker.C:
			select {
			case t.tickerChan <- v:
			default:
			}
		}
	}
}

// newTicker is a helper to setup a ticker by giving an Governance. If
// the governace object implements a ticker generator, a ticker from that
// generator would be returned, else constructs a default one.
func newTicker(gov Governance, round uint64, tickerType TickerType) (t Ticker) {
	type tickerGenerator interface {
		NewTicker(TickerType) Ticker
	}

	if gen, ok := gov.(tickerGenerator); ok {
		t = gen.NewTicker(tickerType)
	}
	if t == nil {
		var duration time.Duration
		switch tickerType {
		case TickerBA:
			duration = utils.GetConfigWithPanic(gov, round, nil).LambdaBA
		case TickerDKG:
			duration = utils.GetConfigWithPanic(gov, round, nil).LambdaDKG
		default:
			panic(fmt.Errorf("unknown ticker type: %d", tickerType))
		}
		t = newDefaultTicker(duration)
	}
	return
}
