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

import "github.com/dexon-foundation/dexon-consensus/core/utils"

// ConfigRoundShift refers to the difference between block's round and config
// round derived from its state.
//
// For example, when round shift is 2, a block in round 0 should derive config
// for round 2.
const ConfigRoundShift uint64 = 2

// DKGDelayRound refers to the round that first DKG is run.
//
// For example, when delay round is 1, new DKG will run at round 1. Round 0 will
// have neither DKG nor CRS.
const DKGDelayRound uint64 = 1

// NoRand is the magic placeholder for randomness field in blocks for blocks
// proposed before DKGDelayRound.
var NoRand = []byte("norand")

func init() {
	utils.SetDKGDelayRound(DKGDelayRound)
}
