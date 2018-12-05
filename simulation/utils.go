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
)

func calcMeanAndStdDeviation(a []float64) (float64, float64) {
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
