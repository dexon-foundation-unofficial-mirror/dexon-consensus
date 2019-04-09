// Copyright 2019 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.  //
// The dexon-consensus library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package syncer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type WatchCatTestSuite struct {
	suite.Suite
}

type testConfigAccessor struct {
	notarySetSize uint32
}

func (cfg *testConfigAccessor) Configuration(uint64) *types.Config {
	return &types.Config{
		NotarySetSize: cfg.notarySetSize,
	}
}

type recovery struct {
	lock  sync.RWMutex
	votes map[uint64]uint64
}

func (rec *recovery) ProposeSkipBlock(height uint64) error {
	rec.lock.Lock()
	defer rec.lock.Unlock()
	rec.votes[height]++
	return nil
}

func (rec *recovery) Votes(height uint64) (uint64, error) {
	rec.lock.RLock()
	defer rec.lock.RUnlock()
	return rec.votes[height], nil
}

func (s *WatchCatTestSuite) newWatchCat(
	notarySetSize uint32, polling, timeout time.Duration) (*WatchCat, *recovery) {
	cfg := &testConfigAccessor{
		notarySetSize: notarySetSize,
	}
	recovery := &recovery{
		votes: make(map[uint64]uint64),
	}
	wc := NewWatchCat(recovery, cfg, polling, timeout, &common.NullLogger{})
	return wc, recovery
}

func (s *WatchCatTestSuite) TestBasicUsage() {
	polling := 50 * time.Millisecond
	timeout := 50 * time.Millisecond
	notarySet := uint32(24)
	watchCat, rec := s.newWatchCat(notarySet, polling, timeout)
	watchCat.Start()
	defer watchCat.Stop()
	pos := types.Position{
		Height: 10,
	}

	for i := 0; i < 10; i++ {
		pos.Height++
		watchCat.Feed(pos)
		time.Sleep(timeout / 2)
		select {
		case <-watchCat.Meow():
			s.FailNow("unexpected terminated")
		default:
		}
	}

	time.Sleep(timeout)
	rec.lock.RLock()
	s.Require().Equal(1, len(rec.votes))
	s.Require().Equal(uint64(1), rec.votes[pos.Height])
	rec.lock.RUnlock()

	time.Sleep(polling * 2)
	select {
	case <-watchCat.Meow():
		s.FailNow("unexpected terminated")
	default:
	}

	rec.lock.Lock()
	rec.votes[pos.Height] = uint64(notarySet/2 + 1)
	rec.lock.Unlock()

	time.Sleep(polling * 2)
	select {
	case <-watchCat.Meow():
	default:
		s.FailNow("expecting terminated")
	}
	s.Equal(pos, watchCat.LastPosition())
}

func TestWatchCat(t *testing.T) {
	suite.Run(t, new(WatchCatTestSuite))
}
