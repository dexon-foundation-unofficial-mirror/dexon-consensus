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

package core

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

var (
	debug = false
	// ErrEmptyTimestamps would be reported if Block.timestamps is empty.
	ErrEmptyTimestamps = errors.New("timestamp vector should not be empty")
)

func init() {
	if os.Getenv("DEBUG") != "" {
		debug = true
	}
}

// Debugf is like fmt.Printf, but only output when we are in debug mode.
func Debugf(format string, args ...interface{}) {
	if debug {
		fmt.Printf(format, args...)
	}
}

// Debugln is like fmt.Println, but only output when we are in debug mode.
func Debugln(args ...interface{}) {
	if debug {
		fmt.Println(args)
	}
}

func interpoTime(t1 time.Time, t2 time.Time, sep int) []time.Time {
	if sep == 0 {
		return []time.Time{}
	}
	if t1.After(t2) {
		return interpoTime(t2, t1, sep)
	}
	timestamps := make([]time.Time, sep)
	duration := t2.Sub(t1)
	period := time.Duration(
		(duration.Nanoseconds() / int64(sep+1))) * time.Nanosecond
	prevTime := t1
	for idx := range timestamps {
		prevTime = prevTime.Add(period)
		timestamps[idx] = prevTime
	}
	return timestamps
}
func getMedianTime(block *types.Block) (t time.Time, err error) {
	timestamps := []time.Time{}
	for _, timestamp := range block.Timestamps {
		timestamps = append(timestamps, timestamp)
	}
	if len(timestamps) == 0 {
		err = ErrEmptyTimestamps
		return
	}
	sort.Sort(common.ByTime(timestamps))
	if len(timestamps)%2 == 0 {
		t1 := timestamps[len(timestamps)/2-1]
		t2 := timestamps[len(timestamps)/2]
		t = interpoTime(t1, t2, 1)[0]
	} else {
		t = timestamps[len(timestamps)/2]
	}
	return

}
