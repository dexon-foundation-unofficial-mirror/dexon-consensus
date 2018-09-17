package integration

import (
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// PrepareValidators setups validators for testing.
func PrepareValidators(
	validatorCount int,
	networkLatency, proposingLatency test.LatencyModel) (
	apps map[types.ValidatorID]*test.App,
	dbs map[types.ValidatorID]blockdb.BlockDatabase,
	validators map[types.ValidatorID]*Validator,
	err error) {

	var (
		db  blockdb.BlockDatabase
		key crypto.PrivateKey
	)

	apps = make(map[types.ValidatorID]*test.App)
	dbs = make(map[types.ValidatorID]blockdb.BlockDatabase)
	validators = make(map[types.ValidatorID]*Validator)

	gov, err := test.NewGovernance(validatorCount, 700)
	if err != nil {
		return
	}
	for vID := range gov.GetValidatorSet() {
		apps[vID] = test.NewApp()

		if db, err = blockdb.NewMemBackedBlockDB(); err != nil {
			return
		}
		dbs[vID] = db
	}
	for vID := range gov.GetValidatorSet() {
		if key, err = gov.GetPrivateKey(vID); err != nil {
			return
		}
		validators[vID] = NewValidator(
			apps[vID],
			gov,
			dbs[vID],
			key,
			vID,
			networkLatency,
			proposingLatency)
	}
	return
}

// VerifyApps is a helper to check delivery between test.Apps
func VerifyApps(apps map[types.ValidatorID]*test.App) (err error) {
	for vFrom, fromApp := range apps {
		if err = fromApp.Verify(); err != nil {
			return
		}
		for vTo, toApp := range apps {
			if vFrom == vTo {
				continue
			}
			if err = fromApp.Compare(toApp); err != nil {
				return
			}
		}
	}
	return
}
