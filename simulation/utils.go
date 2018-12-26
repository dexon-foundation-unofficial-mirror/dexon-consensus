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

package simulation

import (
	"math"
	"sort"

	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/simulation/config"
)

func calculateMeanStdDeviationFloat64s(a []float64) (float64, float64) {
	sum := float64(0)
	for _, i := range a {
		sum += i
	}
	mean := sum / float64(len(a))
	dev := float64(0)
	for _, i := range a {
		dev += (i - mean) * (i - mean)
	}
	dev = math.Sqrt(dev / float64(len(a)))
	return mean, dev
}

func calculateMeanStdDeviationInts(a []int) (float64, float64) {
	floats := []float64{}
	for _, i := range a {
		floats = append(floats, float64(i))
	}
	return calculateMeanStdDeviationFloat64s(floats)
}

func getMinMedianMaxInts(a []int) (int, int, int) {
	aCopied := make([]int, len(a))
	copy(aCopied, a)
	sort.Ints(aCopied)
	return aCopied[0], aCopied[len(aCopied)/2], aCopied[len(aCopied)-1]
}

func getMinMedianMaxFloat64s(a []float64) (float64, float64, float64) {
	aCopied := make([]float64, len(a))
	copy(aCopied, a)
	sort.Float64s(aCopied)
	return aCopied[0], aCopied[len(aCopied)/2], aCopied[len(aCopied)-1]
}

func prepareConfigs(
	round uint64, cfgs []config.Change, gov *test.Governance) {
	for _, c := range cfgs {
		if c.Round != round {
			continue
		}
		t := config.StateChangeTypeFromString(c.Type)
		if err := gov.State().RequestChange(
			t, config.StateChangeValueFromString(t, c.Value)); err != nil {
			panic(err)
		}
	}
	gov.CatchUpWithRound(round)
}
