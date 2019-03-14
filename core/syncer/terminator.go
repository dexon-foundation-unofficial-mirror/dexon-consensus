// Copyright 2019 The dexon-consensus Authors
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

package syncer

import (
	"context"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

type configReader interface {
	Configuration(round uint64) *types.Config
}

// Terminator is reponsible for signaling if syncer object should be terminated.
type Terminator struct {
	recovery     core.Recovery
	configReader configReader
	ping         chan types.Position
	polling      time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	logger       common.Logger
}

// NewTerminator creats a new terminator object.
func NewTerminator(
	recovery core.Recovery,
	configReader configReader,
	polling time.Duration,
	logger common.Logger) *Terminator {
	tt := &Terminator{
		recovery:     recovery,
		configReader: configReader,
		ping:         make(chan types.Position),
		polling:      polling,
		logger:       logger,
	}
	return tt
}

// Ping the terminator so it won't produce the termination signal.
func (tt *Terminator) Ping(position types.Position) {
	tt.ping <- position
}

// Start the terminator.
func (tt *Terminator) Start(timeout time.Duration) {
	tt.Stop()
	tt.ctx, tt.cancel = context.WithCancel(context.Background())
	go func() {
		var lastPos types.Position
	MonitorLoop:
		for {
			select {
			case <-tt.ctx.Done():
				return
			default:
			}
			select {
			case <-tt.ctx.Done():
				return
			case pos := <-tt.ping:
				if !pos.Newer(lastPos) {
					tt.logger.Warn("Ping with older height",
						"pos", pos, "lastPos", lastPos)
					continue
				}
				lastPos = pos
			case <-time.After(timeout):
				tt.logger.Info("Calling Recovery.ProposeSkipBlock",
					"height", lastPos.Height)
				tt.recovery.ProposeSkipBlock(lastPos.Height)
				break MonitorLoop
			}
		}
		go func() {
			for {
				select {
				case <-tt.ctx.Done():
					return
				case <-tt.ping:
				}
			}
		}()
		defer tt.cancel()
		threshold := uint64(
			utils.GetConfigWithPanic(tt.configReader, lastPos.Round, tt.logger).
				NotarySetSize / 2)
		tt.logger.Info("Threshold for recovery", "votes", threshold)
	ResetLoop:
		for {
			votes, err := tt.recovery.Votes(lastPos.Height)
			if err != nil {
				tt.logger.Error("Failed to get recovery votes", "height", lastPos.Height)
			} else if votes > threshold {
				tt.logger.Info("Threshold for recovery reached!")
				break ResetLoop
			}
			select {
			case <-tt.ctx.Done():
				return
			case <-time.After(tt.polling):
			}
		}
	}()
}

// Stop the terminator.
func (tt *Terminator) Stop() {
	if tt.cancel != nil {
		tt.cancel()
	}
}

// Terminated return a closed channel if syncer should be terminated.
func (tt *Terminator) Terminated() <-chan struct{} {
	return tt.ctx.Done()
}
