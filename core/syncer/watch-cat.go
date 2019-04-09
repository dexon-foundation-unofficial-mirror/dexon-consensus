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

// WatchCat is reponsible for signaling if syncer object should be terminated.
type WatchCat struct {
	recovery     core.Recovery
	timeout      time.Duration
	configReader configReader
	feed         chan types.Position
	lastPosition types.Position
	polling      time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	logger       common.Logger
}

// NewWatchCat creats a new WatchCat 🐱 object.
func NewWatchCat(
	recovery core.Recovery,
	configReader configReader,
	polling time.Duration,
	timeout time.Duration,
	logger common.Logger) *WatchCat {
	wc := &WatchCat{
		recovery:     recovery,
		timeout:      timeout,
		configReader: configReader,
		feed:         make(chan types.Position),
		polling:      polling,
		logger:       logger,
	}
	return wc
}

// Feed the WatchCat so it won't produce the termination signal.
func (wc *WatchCat) Feed(position types.Position) {
	wc.feed <- position
}

// Start the WatchCat.
func (wc *WatchCat) Start() {
	wc.Stop()
	wc.lastPosition = types.Position{}
	wc.ctx, wc.cancel = context.WithCancel(context.Background())
	go func() {
		var lastPos types.Position
	MonitorLoop:
		for {
			select {
			case <-wc.ctx.Done():
				return
			default:
			}
			select {
			case <-wc.ctx.Done():
				return
			case pos := <-wc.feed:
				if !pos.Newer(lastPos) {
					wc.logger.Warn("Feed with older height",
						"pos", pos, "lastPos", lastPos)
					continue
				}
				lastPos = pos
			case <-time.After(wc.timeout):
				break MonitorLoop
			}
		}
		go func() {
			for {
				select {
				case <-wc.ctx.Done():
					return
				case <-wc.feed:
				}
			}
		}()
		defer wc.cancel()
		proposed := false
		threshold := uint64(
			utils.GetConfigWithPanic(wc.configReader, lastPos.Round, wc.logger).
				NotarySetSize / 2)
		wc.logger.Info("Threshold for recovery", "votes", threshold)
	ResetLoop:
		for {
			if !proposed {
				wc.logger.Info("Calling Recovery.ProposeSkipBlock",
					"height", lastPos.Height)
				if err := wc.recovery.ProposeSkipBlock(lastPos.Height); err != nil {
					wc.logger.Warn("Failed to proposeSkipBlock", "height", lastPos.Height, "error", err)
				} else {
					proposed = true
				}
			}
			votes, err := wc.recovery.Votes(lastPos.Height)
			if err != nil {
				wc.logger.Error("Failed to get recovery votes", "height", lastPos.Height, "error", err)
			} else if votes > threshold {
				wc.logger.Info("Threshold for recovery reached!")
				wc.lastPosition = lastPos
				break ResetLoop
			}
			select {
			case <-wc.ctx.Done():
				return
			case <-time.After(wc.polling):
			}
		}
	}()
}

// Stop the WatchCat.
func (wc *WatchCat) Stop() {
	if wc.cancel != nil {
		wc.cancel()
	}
}

// Meow return a closed channel if syncer should be terminated.
func (wc *WatchCat) Meow() <-chan struct{} {
	return wc.ctx.Done()
}

// LastPosition returns the last position for recovery.
func (wc *WatchCat) LastPosition() types.Position {
	return wc.lastPosition
}
