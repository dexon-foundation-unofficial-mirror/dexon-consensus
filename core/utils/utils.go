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

package utils

import (
	"context"
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

var dkgDelayRound uint64

// SetDKGDelayRound sets the variable.
func SetDKGDelayRound(delay uint64) {
	dkgDelayRound = delay
}

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

// VerifyDKGComplaint verifies if its a valid DKGCompliant.
func VerifyDKGComplaint(
	complaint *typesDKG.Complaint, mpk *typesDKG.MasterPublicKey) (bool, error) {
	ok, err := VerifyDKGComplaintSignature(complaint)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if complaint.IsNack() {
		return true, nil
	}
	if complaint.Round != mpk.Round {
		return false, nil
	}
	ok, err = VerifyDKGMasterPublicKeySignature(mpk)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	ok, err = mpk.PublicKeyShares.VerifyPrvShare(
		typesDKG.NewID(complaint.PrivateShare.ReceiverID),
		&complaint.PrivateShare.PrivateShare)
	if err != nil {
		return false, err
	}
	return !ok, nil
}

// LaunchDummyReceiver launches a go routine to receive from the receive
// channel of a network module. An context is required to stop the go routine
// automatically. An optinal message handler could be provided.
func LaunchDummyReceiver(
	ctx context.Context, recv <-chan types.Msg, handler func(types.Msg)) (
	context.CancelFunc, <-chan struct{}) {
	var (
		dummyCtx, dummyCancel = context.WithCancel(ctx)
		finishedChan          = make(chan struct{}, 1)
	)
	go func() {
		defer func() {
			finishedChan <- struct{}{}
		}()
	loop:
		for {
			select {
			case <-dummyCtx.Done():
				break loop
			case v, ok := <-recv:
				if !ok {
					panic(fmt.Errorf(
						"receive channel is closed before dummy receiver"))
				}
				if handler != nil {
					handler(v)
				}
			}
		}
	}()
	return dummyCancel, finishedChan
}

// GetDKGThreshold return expected threshold for given DKG set size.
func GetDKGThreshold(config *types.Config) int {
	return int(config.NotarySetSize*2/3) + 1
}

// GetDKGValidThreshold return threshold for DKG set to considered valid.
func GetDKGValidThreshold(config *types.Config) int {
	return int(config.NotarySetSize * 5 / 6)
}

// GetBAThreshold return threshold for BA votes.
func GetBAThreshold(config *types.Config) int {
	return int(config.NotarySetSize*2/3 + 1)
}

// GetNextRoundValidationHeight returns the block height to check if the next
// round is ready.
func GetNextRoundValidationHeight(begin, length uint64) uint64 {
	return begin + length*9/10
}

// GetRoundHeight wraps the workaround for the round height logic in fullnode.
func GetRoundHeight(accessor interface{}, round uint64) uint64 {
	type roundHeightAccessor interface {
		GetRoundHeight(round uint64) uint64
	}
	accessorInst := accessor.(roundHeightAccessor)
	height := accessorInst.GetRoundHeight(round)
	if round == 0 && height < types.GenesisHeight {
		return types.GenesisHeight
	}
	return height
}

// IsDKGValid check if DKG is correctly prepared.
func IsDKGValid(
	gov governanceAccessor, logger common.Logger, round, reset uint64) (
	valid bool, gpkInvalid bool) {
	if !gov.IsDKGFinal(round) {
		logger.Debug("DKG is not final", "round", round, "reset", reset)
		return
	}
	if !gov.IsDKGSuccess(round) {
		logger.Debug("DKG is not successful", "round", round, "reset", reset)
		return
	}
	cfg := GetConfigWithPanic(gov, round, logger)
	gpk, err := typesDKG.NewGroupPublicKey(
		round,
		gov.DKGMasterPublicKeys(round),
		gov.DKGComplaints(round),
		GetDKGThreshold(cfg))
	if err != nil {
		logger.Debug("Group public key setup failed",
			"round", round,
			"reset", reset,
			"error", err)
		gpkInvalid = true
		return
	}
	if len(gpk.QualifyNodeIDs) < GetDKGValidThreshold(cfg) {
		logger.Debug("Group public key threshold not reach",
			"round", round,
			"reset", reset,
			"qualified", len(gpk.QualifyNodeIDs))
		gpkInvalid = true
		return
	}
	valid = true
	return
}
