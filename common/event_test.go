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

package common

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type EventTestSuite struct {
	suite.Suite
}

func (s *EventTestSuite) TestTimeEvent() {
	event := NewEvent()
	now := time.Now()
	triggered := make(chan int, 100)
	trigger := func(id int) func(t time.Time) {
		return func(t time.Time) {
			triggered <- id
		}
	}
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.NotifyTime(now)
	s.Len(triggered, 0)
	event.NotifyTime(now.Add(150 * time.Millisecond))
	s.Len(triggered, 1)
	triggered = make(chan int, 100)
	event.NotifyTime(now.Add(150 * time.Millisecond))
	s.Len(triggered, 0)

	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.NotifyTime(now.Add(150 * time.Millisecond))
	s.Len(triggered, 4)

	triggered = make(chan int, 100)
	for i := 0; i < 10; i++ {
		event.RegisterTime(now.Add(time.Duration(100+i*10)*time.Millisecond),
			trigger(i))
	}
	event.NotifyTime(now.Add(130 * time.Millisecond))
	s.Require().Len(triggered, 4)
	for i := 0; i < 4; i++ {
		j := <-triggered
		s.Equal(i, j)
	}

	event = NewEvent()
	triggered = make(chan int, 100)
	nums := make([]int, 10)
	for i := range nums {
		nums[i] = i
	}
	rand.Shuffle(len(nums), func(i, j int) {
		nums[i], nums[j] = nums[j], nums[i]
	})
	for _, i := range nums {
		event.RegisterTime(now.Add(time.Duration(100+i*10)*time.Millisecond),
			trigger(i))
	}
	event.NotifyTime(now.Add(130 * time.Millisecond))
	s.Require().Len(triggered, 4)
	for i := 0; i < 4; i++ {
		j := <-triggered
		s.Equal(i, j)
	}
}

func (s *EventTestSuite) TestReset() {
	event := NewEvent()
	now := time.Now()
	triggered := make(chan int, 100)
	trigger := func(id int) func(t time.Time) {
		return func(t time.Time) {
			triggered <- id
		}
	}
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.RegisterTime(now.Add(100*time.Millisecond), trigger(0))
	event.Reset()
	event.NotifyTime(now.Add(150 * time.Millisecond))
	s.Len(triggered, 0)
}

func TestEvent(t *testing.T) {
	suite.Run(t, new(EventTestSuite))
}
