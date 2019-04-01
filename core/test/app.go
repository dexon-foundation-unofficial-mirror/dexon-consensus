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
	"bytes"
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
	// ErrMismatchRandomness means the randomness between two blocks with the
	// same hash from two App instances are different.
	ErrMismatchRandomness = fmt.Errorf("mismatch randomness")
	// ErrApplicationIntegrityFailed means the internal datum in a App instance
	// is not integrated.
	ErrApplicationIntegrityFailed = fmt.Errorf("application integrity failed")
	// ErrTimestampOutOfOrder means the later delivered block has timestamp
	// older than previous block.
	ErrTimestampOutOfOrder = fmt.Errorf("timestamp out of order")
	// ErrHeightOutOfOrder means the later delivered block has height not equal
	// to height of previous block plus one.
	ErrHeightOutOfOrder = fmt.Errorf("height out of order")
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
	// ErrMismatchDeliverPosition raised when the block hash and position are
	// mismatched when calling BlockDelivered.
	ErrMismatchDeliverPosition = fmt.Errorf("mismatch deliver position")
	// ErrEmptyRandomness raised when the block contains empty randomness.
	ErrEmptyRandomness = fmt.Errorf("empty randomness")
	// ErrInvalidHeight refers to invalid value for block height.
	ErrInvalidHeight = fmt.Errorf("invalid height")
)

// AppDeliveredRecord caches information when this application received
// a block delivered notification.
type AppDeliveredRecord struct {
	Rand []byte
	When time.Time
	Pos  types.Position
}

// App implements Application interface for testing purpose.
type App struct {
	Confirmed           map[common.Hash]*types.Block
	LastConfirmedHeight uint64
	confirmedLock       sync.RWMutex
	Delivered           map[common.Hash]*AppDeliveredRecord
	DeliverSequence     common.Hashes
	deliveredLock       sync.RWMutex
	state               *State
	gov                 *Governance
	rEvt                *utils.RoundEvent
	hEvt                *common.Event
	roundToNotify       uint64
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
	// Although we only perform reading operations here, to make sure what we
	// prepared unique under concurrent access to this method, writer lock is
	// used.
	app.deliveredLock.Lock()
	defer app.deliveredLock.Unlock()
	hash, lastRec := app.LastDeliveredRecordNoLock()
	if lastRec == nil {
		return types.Witness{}, nil
	}
	if lastRec.Pos.Height < height {
		return types.Witness{}, ErrLowerPendingHeight
	}
	return types.Witness{
		Height: lastRec.Pos.Height,
		Data:   hash.Bytes(),
	}, nil
}

// VerifyBlock implements Application interface.
func (app *App) VerifyBlock(block *types.Block) types.BlockVerifyStatus {
	// Make sure we can handle the witness carried by this block.
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()
	_, rec := app.LastDeliveredRecordNoLock()
	if rec != nil && rec.Pos.Height < block.Witness.Height {
		return types.VerifyRetryLater
	}
	// Confirm if the consensus height matches corresponding block hash.
	var h common.Hash
	copy(h[:], block.Witness.Data)
	app.confirmedLock.RLock()
	defer app.confirmedLock.RUnlock()
	if block.Witness.Height >= types.GenesisHeight {
		// Make sure the hash and height are matched.
		confirmed, exist := app.Confirmed[h]
		if !exist || block.Witness.Height != confirmed.Position.Height {
			return types.VerifyInvalidBlock
		}
	}
	if block.Position.Height != types.GenesisHeight {
		// This check is copied from fullnode, below is quoted from coresponding
		// comment:
		//
		// Check if target block is the next height to be verified, we can only
		// verify the next block in a given chain.
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
	if app.LastConfirmedHeight+1 != b.Position.Height {
		panic(ErrConfirmedHeightNotIncreasing)
	}
	app.LastConfirmedHeight = b.Position.Height
}

// ClearUndeliveredBlocks --
func (app *App) ClearUndeliveredBlocks() {
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()
	app.confirmedLock.Lock()
	defer app.confirmedLock.Unlock()
	app.LastConfirmedHeight = uint64(len(app.DeliverSequence))
}

// BlockDelivered implements Application interface.
func (app *App) BlockDelivered(blockHash common.Hash, pos types.Position,
	rand []byte) {
	func() {
		app.deliveredLock.Lock()
		defer app.deliveredLock.Unlock()
		app.Delivered[blockHash] = &AppDeliveredRecord{
			Rand: common.CopyBytes(rand),
			When: time.Now().UTC(),
			Pos:  pos,
		}
		if len(app.DeliverSequence) > 0 {
			// Make sure parent block also delivered.
			lastHash := app.DeliverSequence[len(app.DeliverSequence)-1]
			d, exists := app.Delivered[lastHash]
			if !exists {
				panic(ErrParentBlockNotDelivered)
			}
			if d.Pos.Height+1 != pos.Height {
				panic(ErrHeightOutOfOrder)
			}
		}
		app.DeliverSequence = append(app.DeliverSequence, blockHash)
	}()
	// Apply packed state change requests in payload.
	func() {
		if app.state == nil {
			return
		}
		app.confirmedLock.RLock()
		defer app.confirmedLock.RUnlock()
		b, exists := app.Confirmed[blockHash]
		if !exists {
			panic(ErrDeliveredBlockNotConfirmed)
		}
		if !b.Position.Equal(pos) {
			panic(ErrMismatchDeliverPosition)
		}
		if err := app.state.Apply(b.Payload); err != nil {
			if err != ErrDuplicatedChange {
				panic(err)
			}
		}
		if app.roundToNotify == pos.Round {
			if app.gov != nil {
				app.gov.NotifyRound(app.roundToNotify, pos.Height)
				app.roundToNotify++
			}
		}
	}()
	app.hEvt.NotifyHeight(pos.Height)
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
				if bytes.Compare(app.Delivered[h].Rand,
					other.Delivered[h].Rand) != 0 {
					err = ErrMismatchRandomness
					return
				}
			}
		})
	})
	return
}

// Verify checks the integrity of date received by this App instance.
func (app *App) Verify() error {
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
		_, exist := app.Confirmed[h]
		if !exist {
			return ErrDeliveredBlockNotConfirmed
		}
		_, exist = app.Delivered[h]
		if !exist {
			return ErrApplicationIntegrityFailed
		}
		b, exist := app.Confirmed[h]
		if !exist {
			return ErrApplicationIntegrityFailed
		}
		// Make sure the consensus time is incremental.
		if prevTime.After(b.Timestamp) {
			return ErrTimestampOutOfOrder
		}
		prevTime = b.Timestamp
		// Make sure the consensus height is incremental.
		rec, exist := app.Delivered[h]
		if !exist {
			return ErrApplicationIntegrityFailed
		}
		if len(rec.Rand) == 0 {
			return ErrEmptyRandomness
		}
		// Make sure height is valid.
		if b.Position.Height < types.GenesisHeight {
			return ErrInvalidHeight
		}
		if expectHeight != rec.Pos.Height {
			return ErrHeightOutOfOrder
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

	function(app)
}

// LastDeliveredRecordNoLock returns the latest AppDeliveredRecord under lock.
func (app *App) LastDeliveredRecordNoLock() (common.Hash, *AppDeliveredRecord) {
	var hash common.Hash
	if len(app.DeliverSequence) == 0 {
		return hash, nil
	}
	hash = app.DeliverSequence[len(app.DeliverSequence)-1]
	return hash, app.Delivered[hash]
}
