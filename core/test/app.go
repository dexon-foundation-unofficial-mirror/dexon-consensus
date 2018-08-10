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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
)

// App implements Application interface for testing purpose.
type App struct {
	Acked        map[common.Hash]struct{}
	TotalOrdered []*struct {
		BlockHashes common.Hashes
		Early       bool
	}
	Delivered map[common.Hash]time.Time
}

// NewApp constructs a TestApp instance.
func NewApp() *App {
	return &App{
		Acked: make(map[common.Hash]struct{}),
		TotalOrdered: []*struct {
			BlockHashes common.Hashes
			Early       bool
		}{},
		Delivered: make(map[common.Hash]time.Time),
	}
}

// StronglyAcked implements Application interface.
func (app *App) StronglyAcked(blockHash common.Hash) {
	app.Acked[blockHash] = struct{}{}
}

// TotalOrderingDeliver implements Application interface.
func (app *App) TotalOrderingDeliver(blockHashes common.Hashes, early bool) {
	app.TotalOrdered = append(app.TotalOrdered, &struct {
		BlockHashes common.Hashes
		Early       bool
	}{
		BlockHashes: blockHashes,
		Early:       early,
	})
}

// DeliverBlock implements Application interface.
func (app *App) DeliverBlock(blockHash common.Hash, timestamp time.Time) {
	app.Delivered[blockHash] = timestamp
}
