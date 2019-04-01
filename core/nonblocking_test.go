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
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// slowApp is an Application instance slow things down in every method.
type slowApp struct {
	sleep          time.Duration
	blockConfirmed map[common.Hash]struct{}
	blockDelivered map[common.Hash]struct{}
}

func newSlowApp(sleep time.Duration) *slowApp {
	return &slowApp{
		sleep:          sleep,
		blockConfirmed: make(map[common.Hash]struct{}),
		blockDelivered: make(map[common.Hash]struct{}),
	}
}

func (app *slowApp) PreparePayload(_ types.Position) ([]byte, error) {
	return []byte{}, nil
}

func (app *slowApp) PrepareWitness(_ uint64) (types.Witness, error) {
	return types.Witness{}, nil
}

func (app *slowApp) VerifyBlock(_ *types.Block) types.BlockVerifyStatus {
	return types.VerifyOK
}

func (app *slowApp) BlockConfirmed(block types.Block) {
	time.Sleep(app.sleep)
	app.blockConfirmed[block.Hash] = struct{}{}
}

func (app *slowApp) BlockDelivered(blockHash common.Hash,
	blockPosition types.Position, _ []byte) {
	time.Sleep(app.sleep)
	app.blockDelivered[blockHash] = struct{}{}
}

func (app *slowApp) BlockReceived(hash common.Hash) {}

func (app *slowApp) BlockReady(hash common.Hash) {}

// noDebugApp is to make sure nonBlocking works when Debug interface
// is not implemented by the provided Application instance.
type noDebugApp struct {
	blockConfirmed map[common.Hash]struct{}
	blockDelivered map[common.Hash]struct{}
}

func newNoDebugApp() *noDebugApp {
	return &noDebugApp{
		blockConfirmed: make(map[common.Hash]struct{}),
		blockDelivered: make(map[common.Hash]struct{}),
	}
}

func (app *noDebugApp) PreparePayload(_ types.Position) ([]byte, error) {
	panic("test")
}

func (app *noDebugApp) PrepareWitness(_ uint64) (types.Witness, error) {
	panic("test")
}

func (app *noDebugApp) VerifyBlock(_ *types.Block) types.BlockVerifyStatus {
	panic("test")
}

func (app *noDebugApp) BlockConfirmed(block types.Block) {
	app.blockConfirmed[block.Hash] = struct{}{}
}

func (app *noDebugApp) BlockDelivered(blockHash common.Hash,
	blockPosition types.Position, _ []byte) {
	app.blockDelivered[blockHash] = struct{}{}
}

type NonBlockingTestSuite struct {
	suite.Suite
}

func (s *NonBlockingTestSuite) TestNonBlocking() {
	sleep := 50 * time.Millisecond
	app := newSlowApp(sleep)
	nbModule := newNonBlocking(app, app)
	hashes := make(common.Hashes, 10)
	for idx := range hashes {
		hashes[idx] = common.NewRandomHash()
	}
	now := time.Now().UTC()
	shouldFinish := now.Add(100 * time.Millisecond)

	// Start doing some 'heavy' job.
	for _, hash := range hashes {
		nbModule.BlockConfirmed(types.Block{
			Hash:    hash,
			Witness: types.Witness{},
		})
		nbModule.BlockDelivered(hash, types.Position{}, []byte(nil))
	}

	// nonBlocking should be non-blocking.
	s.True(shouldFinish.After(time.Now().UTC()))

	nbModule.wait()
	for _, hash := range hashes {
		s.Contains(app.blockConfirmed, hash)
		s.Contains(app.blockDelivered, hash)
	}
}

func (s *NonBlockingTestSuite) TestNoDebug() {
	app := newNoDebugApp()
	nbModule := newNonBlocking(app, nil)
	hash := common.NewRandomHash()
	// Test BlockConfirmed.
	nbModule.BlockConfirmed(types.Block{Hash: hash})
	// Test BlockDelivered
	nbModule.BlockDelivered(hash, types.Position{}, []byte(nil))
	nbModule.wait()
	s.Contains(app.blockConfirmed, hash)
	s.Contains(app.blockDelivered, hash)
	// Test other synchronous methods.
	s.Panics(func() { nbModule.PreparePayload(types.Position{}) })
	s.Panics(func() { nbModule.PrepareWitness(0) })
	s.Panics(func() { nbModule.VerifyBlock(nil) })
}

func TestNonBlocking(t *testing.T) {
	suite.Run(t, new(NonBlockingTestSuite))
}
