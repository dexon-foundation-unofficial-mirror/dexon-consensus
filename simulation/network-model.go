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
