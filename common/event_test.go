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

package common

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/suite"
)

type EventTestSuite struct {
	suite.Suite
}

func (s *EventTestSuite) TestHeightEvent() {
	event := NewEvent()
	triggered := make(chan int, 100)
	trigger := func(id int) func(uint64) {
		return func(uint64) {
			triggered <- id
		}
	}
	event.RegisterHeight(100, trigger(0))
	event.NotifyHeight(0)
	s.Len(triggered, 0)
	event.NotifyHeight(150)
	s.Len(triggered, 1)
	triggered = make(chan int, 100)
	event.NotifyHeight(150)
	s.Len(triggered, 0)

	event.RegisterHeight(100, trigger(0))
	event.RegisterHeight(100, trigger(0))
	event.RegisterHeight(100, trigger(0))
	event.RegisterHeight(100, trigger(0))
	event.NotifyHeight(150)
	s.Len(triggered, 4)

	triggered = make(chan int, 100)
	for i := 0; i < 10; i++ {
		event.RegisterHeight(uint64(100+i*10), trigger(i))
	}
	event.NotifyHeight(130)
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
		event.RegisterHeight(uint64(100+i*10), trigger(i))
	}
	event.NotifyHeight(130)
	s.Require().Len(triggered, 4)
	for i := 0; i < 4; i++ {
		j := <-triggered
		s.Equal(i, j)
	}
}

func (s *EventTestSuite) TestReset() {
	event := NewEvent()
	triggered := make(chan int, 100)
	trigger := func(id int) func(h uint64) {
		return func(uint64) {
			triggered <- id
		}
	}
	event.RegisterHeight(100, trigger(0))
	event.RegisterHeight(100, trigger(0))
	event.RegisterHeight(100, trigger(0))
	event.RegisterHeight(100, trigger(0))
	event.RegisterHeight(100, trigger(0))
	event.Reset()
	event.NotifyHeight(150)
	s.Len(triggered, 0)
}

func TestEvent(t *testing.T) {
	suite.Run(t, new(EventTestSuite))
}
