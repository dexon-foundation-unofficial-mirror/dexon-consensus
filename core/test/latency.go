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

package test

import (
	"math/rand"
	"time"
)

// LatencyModel defines an interface to randomly decide latency
// for one operation.
type LatencyModel interface {
	Delay() time.Duration
}

// NormalLatencyModel would return latencies in normal distribution.
type NormalLatencyModel struct {
	Sigma float64
	Mean  float64
}

// Delay implements LatencyModel interface.
func (m *NormalLatencyModel) Delay() time.Duration {
	delay := rand.NormFloat64()*m.Sigma + m.Mean
	if delay < 0 {
		delay = m.Sigma / 2
	}
	return time.Duration(delay) * time.Millisecond
}

// FixedLatencyModel return fixed latencies.
type FixedLatencyModel struct {
	Latency float64
}

// Delay implements LatencyModel interface.
func (m *FixedLatencyModel) Delay() time.Duration {
	return time.Duration(m.Latency) * time.Millisecond
}
