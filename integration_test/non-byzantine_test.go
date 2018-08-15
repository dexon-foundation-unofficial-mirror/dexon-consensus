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

package integration

import (
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type NonByzantineTestSuite struct {
	suite.Suite
}

func (s *NonByzantineTestSuite) TestNonByzantine() {
	var (
		networkLatency = &normalLatencyModel{
			Sigma: 20,
			Mean:  250,
		}
		proposingLatency = &normalLatencyModel{
			Sigma: 30,
			Mean:  500,
		}
		apps = make(map[types.ValidatorID]*test.App)
		dbs  = make(map[types.ValidatorID]blockdb.BlockDatabase)
		req  = s.Require()
	)

	gov, err := test.NewGovernance(25, 700)
	req.Nil(err)
	now := time.Now().UTC()
	for vID := range gov.GetValidatorSet() {
		apps[vID] = test.NewApp()

		db, err := blockdb.NewMemBackedBlockDB()
		req.Nil(err)
		dbs[vID] = db
	}
	stopper := test.NewStopByConfirmedBlocks(50, apps, dbs)
	sch := test.NewScheduler(stopper)
	for vID := range gov.GetValidatorSet() {
		key, err := gov.GetPrivateKey(vID)
		req.Nil(err)
		v := newValidator(
			apps[vID],
			gov,
			dbs[vID],
			key,
			vID,
			networkLatency,
			proposingLatency)
		sch.RegisterEventHandler(vID, v)
		req.Nil(sch.Seed(newProposeBlockEvent(vID, now)))
	}
	sch.Run(10)
	// Check results by comparing test.App instances.
	for vFrom := range gov.GetValidatorSet() {
		req.Nil(apps[vFrom].Verify())
		for vTo := range gov.GetValidatorSet() {
			if vFrom == vTo {
				continue
			}
			req.Nil(apps[vFrom].Compare(apps[vTo]))
		}
	}
}

func TestNonByzantine(t *testing.T) {
	suite.Run(t, new(NonByzantineTestSuite))
}
