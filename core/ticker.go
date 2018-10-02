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

package core

import "time"

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
	ticker *time.Ticker
}

// newDefaultTicker constructs an defaultTicker instance by giving an interval.
func newDefaultTicker(lambda time.Duration) *defaultTicker {
	return &defaultTicker{ticker: time.NewTicker(lambda)}
}

// Tick implements Tick method of ticker interface.
func (t *defaultTicker) Tick() <-chan time.Time {
	return t.ticker.C
}

// Stop implements Stop method of ticker interface.
func (t *defaultTicker) Stop() {
	t.ticker.Stop()
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
			duration = gov.Configuration(round).LambdaBA
		case TickerDKG:
			duration = gov.Configuration(round).LambdaDKG
		case TickerCRS:
			duration = gov.Configuration(round).RoundInterval / 2
		}
		t = newDefaultTicker(duration)
	}
	return
}
