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

package simulation

import (
	"math/rand"
	"time"
)

// Model is the interface for define a given network environment.
type Model interface {
	// LossRate returns the message lost ratio between [0, 1)
	LossRate() float64

	// Delay returns the send delay of the message.  This function is called each
	// time before the message is sent, so one can return different number each
	// time.
	Delay() time.Duration
}

// LosslessNetwork is a lossless network model.
type LosslessNetwork struct {
}

// LossRate returns lossrate for the model.
func (l *LosslessNetwork) LossRate() float64 {
	return 0.0
}

// Delay returns the send delay of a given message.
func (l *LosslessNetwork) Delay() time.Duration {
	return time.Duration(0)
}

// FixedLostNoDelayModel is a network with no delay and a fixed lost
// ratio.
type FixedLostNoDelayModel struct {
	LossRateValue float64
}

// LossRate returns lossrate for the model.
func (f *FixedLostNoDelayModel) LossRate() float64 {
	return f.LossRateValue
}

// Delay returns the send delay of a given message.
func (f *FixedLostNoDelayModel) Delay() time.Duration {
	return time.Duration(0)
}

// NormalNetwork is a model where it's delay is a normal distribution.
type NormalNetwork struct {
	Sigma         float64
	Mean          float64
	LossRateValue float64
}

// LossRate returns lossrate for the model.
func (n *NormalNetwork) LossRate() float64 {
	return n.LossRateValue
}

// Delay returns the send delay of a given message.
func (n *NormalNetwork) Delay() time.Duration {
	delay := rand.NormFloat64()*n.Sigma + n.Mean
	if delay < 0 {
		delay = n.Sigma / 2
	}
	return time.Duration(delay) * time.Millisecond
}
