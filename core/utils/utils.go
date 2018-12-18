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

package utils

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type configAccessor interface {
	Configuration(round uint64) *types.Config
}

// GetConfigWithPanic is a helper to access configs, and panic when config for
// that round is not ready yet.
func GetConfigWithPanic(accessor configAccessor, round uint64,
	logger common.Logger) *types.Config {
	if logger != nil {
		logger.Debug("Calling Governance.Configuration", "round", round)
	}
	c := accessor.Configuration(round)
	if c == nil {
		panic(fmt.Errorf("configuration is not ready %v", round))
	}
	return c
}

type crsAccessor interface {
	CRS(round uint64) common.Hash
}

// GetCRSWithPanic is a helper to access CRS, and panic when CRS for that
// round is not ready yet.
func GetCRSWithPanic(accessor crsAccessor, round uint64,
	logger common.Logger) common.Hash {
	if logger != nil {
		logger.Debug("Calling Governance.CRS", "round", round)
	}
	crs := accessor.CRS(round)
	if (crs == common.Hash{}) {
		panic(fmt.Errorf("CRS is not ready %v", round))
	}
	return crs

}
