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

package config

import (
	"fmt"
	"strconv"

	"github.com/dexon-foundation/dexon-consensus/core/test"
)

// StateChangeTypeFromString convert a string to test.StateChangeType.
func StateChangeTypeFromString(s string) test.StateChangeType {
	switch s {
	case "lambda_ba":
		return test.StateChangeLambdaBA
	case "lambda_dkg":
		return test.StateChangeLambdaDKG
	case "round_interval":
		return test.StateChangeRoundLength
	case "min_block_interval":
		return test.StateChangeMinBlockInterval
	case "notary_set_size":
		return test.StateChangeNotarySetSize
	}
	panic(fmt.Errorf("unsupported state change type %s", s))
}

// StateChangeValueFromString converts a string to a value for state change
// request.
func StateChangeValueFromString(
	t test.StateChangeType, v string) interface{} {
	switch t {
	case test.StateChangeNotarySetSize:
		ret, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			panic(err)
		}
		return uint32(ret)
	case test.StateChangeLambdaBA, test.StateChangeLambdaDKG,
		test.StateChangeRoundLength, test.StateChangeMinBlockInterval:
		ret, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			panic(err)
		}
		return int(ret)
	}
	panic(fmt.Errorf("unsupported state change type %s", t))
}
