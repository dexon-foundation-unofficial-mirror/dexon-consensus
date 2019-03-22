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

package test

import (
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

var (
	// ErrEmptyDeliverSequence means there is no delivery event in this App
	// instance.
	ErrEmptyDeliverSequence = fmt.Errorf("empty deliver sequence")
	// ErrMismatchBlockHashSequence means the delivering sequence between two App
	// instances are different.
	ErrMismatchBlockHashSequence = fmt.Errorf("mismatch block hash sequence")
	// ErrMismatchConsensusTime means the consensus timestamp between two blocks
	// with the same hash from two App instances are different.
	ErrMismatchConsensusTime = fmt.Errorf("mismatch consensus time")
	// ErrApplicationIntegrityFailed means the internal datum in a App instance
	// is not integrated.
	ErrApplicationIntegrityFailed = fmt.Errorf("application integrity failed")
	// ErrConsensusTimestampOutOfOrder means the later delivered block has
	// consensus timestamp older than previous block.
	ErrConsensusTimestampOutOfOrder = fmt.Errorf(
		"consensus timestamp out of order")
	// ErrConsensusHeightOutOfOrder means the later delivered block has
	// consensus height not equal to height of previous block plus one.
	ErrConsensusHeightOutOfOrder = fmt.Errorf(
		"consensus height out of order")
	// ErrDeliveredBlockNotConfirmed means some block delivered (confirmed) but
	// not confirmed.
	ErrDeliveredBlockNotConfirmed = fmt.Errorf("delivered block not confirmed")
	// ErrAckingBlockNotDelivered means the delivered sequence not forming a
	// DAG.
	ErrAckingBlockNotDelivered = fmt.Errorf("acking block not delivered")
	// ErrLowerPendingHeight raised when lastPendingHeight is lower than the
	// height to be prepared.
	ErrLowerPendingHeight = fmt.Errorf(
		"last pending height < consensus height")
	// ErrConfirmedHeightNotIncreasing raise when the height of the confirmed
	// block doesn't follow previous confirmed block on the same chain.
	ErrConfirmedHeightNotIncreasing = fmt.Errorf(
		"confirmed height not increasing")
	// ErrParentBlockNotDelivered raised when the parent block is not seen by
	// this app.
	ErrParentBlockNotDelivered = fmt.Errorf("parent block not delivered")
)

// AppDeliveredRecord caches information when this application received
// a block delivered notification.
type AppDeliveredRecord struct {
	Result types.FinalizationResult
	When   time.Time
	Pos    types.Position
}

// App implements Application interface for testing purpose.
type App struct {
	Confirmed             map[common.Hash]*types.Block
	LastConfirmedHeight   uint64
	confirmedLock         sync.RWMutex
	Delivered             map[common.Hash]*AppDeliveredRecord
	DeliverSequence       common.Hashes
	deliveredLock         sync.RWMutex
	state                 *State
	gov                   *Governance
	rEvt                  *utils.RoundEvent
	hEvt                  *common.Event
	lastPendingHeightLock sync.RWMutex
	LastPendingHeight     uint64
	roundToNotify         uint64
}

// NewApp constructs a TestApp instance.
func NewApp(initRound uint64, gov *Governance, rEvt *utils.RoundEvent) (
	app *App) {
	app = &App{
		Confirmed:       make(map[common.Hash]*types.Block),
		Delivered:       make(map[common.Hash]*AppDeliveredRecord),
		DeliverSequence: common.Hashes{},
		gov:             gov,
		rEvt:            rEvt,
		hEvt:            common.NewEvent(),
		roundToNotify:   initRound,
	}
	if gov != nil {
		app.state = gov.State()
	}
	if rEvt != nil {
		rEvt.Register(func(evts []utils.RoundEventParam) {
			app.hEvt.RegisterHeight(
				evts[len(evts)-1].NextRoundValidationHeight(),
				utils.RoundEventRetryHandlerGenerator(rEvt, app.hEvt),
			)
		})
		rEvt.TriggerInitEvent()
	}
	return app
}

// PreparePayload implements Application interface.
func (app *App) PreparePayload(position types.Position) ([]byte, error) {
	if app.state == nil {
		return []byte{}, nil
	}
	return app.state.PackRequests()
}

// PrepareWitness implements Application interface.
func (app *App) PrepareWitness(height uint64) (types.Witness, error) {
	pendingHeight := app.getLastPendingWitnessHeight()
	if pendingHeight < height {
		return types.Witness{}, ErrLowerPendingHeight
	}
	if pendingHeight == 0 {
		return types.Witness{}, nil
	}
	hash := func() common.Hash {
		app.deliveredLock.RLock()
		defer app.deliveredLock.RUnlock()
		// Our witness height starts from 1.
		h := app.DeliverSequence[pendingHeight-1]
		// Double confirm if the delivered record matches the pending height.
		if app.Delivered[h].Result.Height != pendingHeight {
			app.confirmedLock.RLock()
			defer app.confirmedLock.RUnlock()
			panic(fmt.Errorf("unmatched finalization record: %s, %v, %v",
				app.Confirmed[h], pendingHeight,
				app.Delivered[h].Result.Height))
		}
		return h
	}()
	return types.Witness{
		Height: pendingHeight,
		Data:   hash.Bytes(),
	}, nil
}

// VerifyBlock implements Application interface.
func (app *App) VerifyBlock(block *types.Block) types.BlockVerifyStatus {
	// Make sure we can handle the witness carried by this block.
	pendingHeight := app.getLastPendingWitnessHeight()
	if pendingHeight < block.Witness.Height {
		return types.VerifyRetryLater
	}
	// Confirm if the consensus height matches corresponding block hash.
	var h common.Hash
	copy(h[:], block.Witness.Data)
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()
	// This is the difference between test.App and fullnode, fullnode has the
	// genesis block at height=0, we don't. Thus our reasonable witness starts
	// from 1.
	if block.Witness.Height > 0 {
		if block.Witness.Height != app.Delivered[h].Result.Height {
			return types.VerifyInvalidBlock
		}
	}
	if block.Position.Height != 0 {
		// This check is copied from fullnode, below is quoted from coresponding
		// comment:
		//
		// Check if target block is the next height to be verified, we can only
		// verify the next block in a given chain.
		app.confirmedLock.RLock()
		defer app.confirmedLock.RUnlock()
		if app.LastConfirmedHeight+1 != block.Position.Height {
			return types.VerifyRetryLater
		}
	}
	return types.VerifyOK
}

// BlockConfirmed implements Application interface.
func (app *App) BlockConfirmed(b types.Block) {
	app.confirmedLock.Lock()
	defer app.confirmedLock.Unlock()
	app.Confirmed[b.Hash] = &b
	if b.Position.Height != 0 {
		if app.LastConfirmedHeight+1 != b.Position.Height {
			panic(ErrConfirmedHeightNotIncreasing)
		}
	}
	app.LastConfirmedHeight = b.Position.Height
}

// ClearUndeliveredBlocks --
func (app *App) ClearUndeliveredBlocks() {
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()
	app.confirmedLock.Lock()
	defer app.confirmedLock.Unlock()
	app.LastConfirmedHeight = uint64(len(app.DeliverSequence) - 1)
}

// BlockDelivered implements Application interface.
func (app *App) BlockDelivered(blockHash common.Hash, pos types.Position,
	result types.FinalizationResult) {
	func() {
		app.deliveredLock.Lock()
		defer app.deliveredLock.Unlock()
		app.Delivered[blockHash] = &AppDeliveredRecord{
			Result: result,
			When:   time.Now().UTC(),
			Pos:    pos,
		}
		app.DeliverSequence = append(app.DeliverSequence, blockHash)
		// Make sure parent block also delivered.
		if !result.ParentHash.Equal(common.Hash{}) {
			d, exists := app.Delivered[result.ParentHash]
			if !exists {
				panic(ErrParentBlockNotDelivered)
			}
			if d.Result.Height+1 != result.Height {
				panic(ErrConsensusHeightOutOfOrder)
			}
		}
		app.lastPendingHeightLock.Lock()
		defer app.lastPendingHeightLock.Unlock()
		app.LastPendingHeight = result.Height
	}()
	// Apply packed state change requests in payload.
	func() {
		if app.state == nil {
			return
		}
		app.confirmedLock.RLock()
		defer app.confirmedLock.RUnlock()
		b := app.Confirmed[blockHash]
		if err := app.state.Apply(b.Payload); err != nil {
			if err != ErrDuplicatedChange {
				panic(err)
			}
		}
		if app.roundToNotify == pos.Round {
			if app.gov != nil {
				app.gov.NotifyRound(app.roundToNotify)
				app.roundToNotify++
			}
		}
	}()
	app.hEvt.NotifyHeight(result.Height)
}

// GetLatestDeliveredPosition would return the latest position of delivered
// block seen by this application instance.
func (app *App) GetLatestDeliveredPosition() types.Position {
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()
	app.confirmedLock.RLock()
	defer app.confirmedLock.RUnlock()
	if len(app.DeliverSequence) == 0 {
		return types.Position{}
	}
	return app.Confirmed[app.DeliverSequence[len(app.DeliverSequence)-1]].Position
}

// Compare performs these checks against another App instance
// and return erros if not passed:
// - deliver sequence by comparing block hashes.
// - consensus timestamp of each block are equal.
func (app *App) Compare(other *App) (err error) {
	app.WithLock(func(app *App) {
		other.WithLock(func(other *App) {
			minLength := len(app.DeliverSequence)
			if minLength > len(other.DeliverSequence) {
				minLength = len(other.DeliverSequence)
			}
			if minLength == 0 {
				err = ErrEmptyDeliverSequence
				return
			}
			// Here we assumes both Apps begin from the same height.
			for idx, h := range app.DeliverSequence[:minLength] {
				hOther := other.DeliverSequence[idx]
				if hOther != h {
					err = ErrMismatchBlockHashSequence
					return
				}
				if app.Delivered[h].Result.Timestamp !=
					other.Delivered[h].Result.Timestamp {
					err = ErrMismatchConsensusTime
					return
				}
			}
		})
	})
	return
}

// Verify checks the integrity of date received by this App instance.
func (app *App) Verify() error {
	// TODO(mission): verify blocks' position when delivered.
	app.confirmedLock.RLock()
	defer app.confirmedLock.RUnlock()
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()

	if len(app.DeliverSequence) == 0 {
		return ErrEmptyDeliverSequence
	}
	if len(app.DeliverSequence) != len(app.Delivered) {
		return ErrApplicationIntegrityFailed
	}
	expectHeight := uint64(1)
	prevTime := time.Time{}
	for _, h := range app.DeliverSequence {
		_, exists := app.Confirmed[h]
		if !exists {
			return ErrDeliveredBlockNotConfirmed
		}
		rec, exists := app.Delivered[h]
		if !exists {
			return ErrApplicationIntegrityFailed
		}
		// Make sure the consensus time is incremental.
		ok := prevTime.Before(rec.Result.Timestamp) ||
			prevTime.Equal(rec.Result.Timestamp)
		if !ok {
			return ErrConsensusTimestampOutOfOrder
		}
		prevTime = rec.Result.Timestamp
		// Make sure the consensus height is incremental.
		if expectHeight != rec.Result.Height {
			return ErrConsensusHeightOutOfOrder
		}
		expectHeight++
	}
	return nil
}

// BlockReceived implements interface Debug.
func (app *App) BlockReceived(hash common.Hash) {}

// BlockReady implements interface Debug.
func (app *App) BlockReady(hash common.Hash) {}

// WithLock provides a backdoor to check status of App with reader lock.
func (app *App) WithLock(function func(*App)) {
	app.confirmedLock.RLock()
	defer app.confirmedLock.RUnlock()
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()
	app.lastPendingHeightLock.RLock()
	defer app.lastPendingHeightLock.RUnlock()

	function(app)
}

func (app *App) getLastPendingWitnessHeight() uint64 {
	app.lastPendingHeightLock.RLock()
	defer app.lastPendingHeightLock.RUnlock()
	return app.LastPendingHeight
}
