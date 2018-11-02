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
	// ErrDeliveredBlockNotAcked means some block delivered (confirmed) but
	// not strongly acked.
	ErrDeliveredBlockNotAcked = fmt.Errorf("delivered block not acked")
	// ErrMismatchTotalOrderingAndDelivered mean the sequence of total ordering
	// and delivered are different.
	ErrMismatchTotalOrderingAndDelivered = fmt.Errorf(
		"mismatch total ordering and delivered sequence")
)

// AppAckedRecord caches information when this application received
// a strongly-acked notification.
type AppAckedRecord struct {
	When time.Time
}

// AppTotalOrderRecord caches information when this application received
// a total-ordering deliver notification.
type AppTotalOrderRecord struct {
	BlockHashes common.Hashes
	Mode        uint32
	When        time.Time
}

// AppDeliveredRecord caches information when this application received
// a block delivered notification.
type AppDeliveredRecord struct {
	ConsensusTime   time.Time
	ConsensusHeight uint64
	When            time.Time
}

// App implements Application interface for testing purpose.
type App struct {
	Acked              map[common.Hash]*AppAckedRecord
	ackedLock          sync.RWMutex
	TotalOrdered       []*AppTotalOrderRecord
	TotalOrderedByHash map[common.Hash]*AppTotalOrderRecord
	totalOrderedLock   sync.RWMutex
	Delivered          map[common.Hash]*AppDeliveredRecord
	DeliverSequence    common.Hashes
	deliveredLock      sync.RWMutex
	blocks             map[common.Hash]*types.Block
	blocksLock         sync.Mutex
	state              *State
}

// NewApp constructs a TestApp instance.
func NewApp(state *State) *App {
	return &App{
		Acked:              make(map[common.Hash]*AppAckedRecord),
		TotalOrdered:       []*AppTotalOrderRecord{},
		TotalOrderedByHash: make(map[common.Hash]*AppTotalOrderRecord),
		Delivered:          make(map[common.Hash]*AppDeliveredRecord),
		DeliverSequence:    common.Hashes{},
		blocks:             make(map[common.Hash]*types.Block),
		state:              state,
	}
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
	return types.Witness{
		Height: height,
	}, nil
}

// VerifyBlock implements Application.
func (app *App) VerifyBlock(block *types.Block) types.BlockVerifyStatus {
	return types.VerifyOK
}

// BlockConfirmed implements Application interface.
func (app *App) BlockConfirmed(b types.Block) {
	app.blocksLock.Lock()
	defer app.blocksLock.Unlock()
	app.blocks[b.Hash] = &b
}

// StronglyAcked implements Application interface.
func (app *App) StronglyAcked(blockHash common.Hash) {
	app.ackedLock.Lock()
	defer app.ackedLock.Unlock()

	app.Acked[blockHash] = &AppAckedRecord{When: time.Now().UTC()}
}

// TotalOrderingDelivered implements Application interface.
func (app *App) TotalOrderingDelivered(blockHashes common.Hashes, mode uint32) {
	app.totalOrderedLock.Lock()
	defer app.totalOrderedLock.Unlock()

	rec := &AppTotalOrderRecord{
		BlockHashes: blockHashes,
		Mode:        mode,
		When:        time.Now().UTC(),
	}
	app.TotalOrdered = append(app.TotalOrdered, rec)
	for _, h := range blockHashes {
		if _, exists := app.TotalOrderedByHash[h]; exists {
			panic(fmt.Errorf("deliver duplicated blocks from total ordering"))
		}
		app.TotalOrderedByHash[h] = rec
	}
}

// BlockDelivered implements Application interface.
func (app *App) BlockDelivered(
	blockHash common.Hash, result types.FinalizationResult) {
	func() {
		app.deliveredLock.Lock()
		defer app.deliveredLock.Unlock()
		app.Delivered[blockHash] = &AppDeliveredRecord{
			ConsensusTime:   result.Timestamp,
			ConsensusHeight: result.Height,
			When:            time.Now().UTC(),
		}
		app.DeliverSequence = append(app.DeliverSequence, blockHash)
	}()
	// Apply packed state change requests in payload.
	func() {
		if app.state == nil {
			return
		}
		app.blocksLock.Lock()
		defer app.blocksLock.Unlock()
		b := app.blocks[blockHash]
		if err := app.state.Apply(b.Payload); err != nil {
			if err != ErrDuplicatedChange {
				panic(err)
			}
		}
	}()
}

// Compare performs these checks against another App instance
// and return erros if not passed:
// - deliver sequence by comparing block hashes.
// - consensus timestamp of each block are equal.
func (app *App) Compare(other *App) error {
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()
	other.deliveredLock.RLock()
	defer other.deliveredLock.RUnlock()

	minLength := len(app.DeliverSequence)
	if minLength > len(other.DeliverSequence) {
		minLength = len(other.DeliverSequence)
	}
	if minLength == 0 {
		return ErrEmptyDeliverSequence
	}
	for idx, h := range app.DeliverSequence[:minLength] {
		hOther := other.DeliverSequence[idx]
		if hOther != h {
			return ErrMismatchBlockHashSequence
		}
		if app.Delivered[h].ConsensusTime != other.Delivered[h].ConsensusTime {
			return ErrMismatchConsensusTime
		}
	}
	return nil
}

// Verify checks the integrity of date received by this App instance.
func (app *App) Verify() error {
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()

	if len(app.DeliverSequence) == 0 {
		return ErrEmptyDeliverSequence
	}
	if len(app.DeliverSequence) != len(app.Delivered) {
		return ErrApplicationIntegrityFailed
	}

	app.ackedLock.RLock()
	defer app.ackedLock.RUnlock()

	expectHeight := uint64(1)
	prevTime := time.Time{}
	for _, h := range app.DeliverSequence {
		// Make sure delivered block is strongly acked.
		if _, acked := app.Acked[h]; !acked {
			return ErrDeliveredBlockNotAcked
		}
		rec, exists := app.Delivered[h]
		if !exists {
			return ErrApplicationIntegrityFailed
		}
		// Make sure the consensus time is incremental.
		ok := prevTime.Before(rec.ConsensusTime) ||
			prevTime.Equal(rec.ConsensusTime)
		if !ok {
			return ErrConsensusTimestampOutOfOrder
		}
		prevTime = rec.ConsensusTime

		// Make sure the consensus height is incremental.
		if expectHeight != rec.ConsensusHeight {
			return ErrConsensusHeightOutOfOrder
		}
		expectHeight++
	}
	// Make sure the order of delivered and total ordering are the same by
	// comparing the concated string.
	app.totalOrderedLock.RLock()
	defer app.totalOrderedLock.RUnlock()

	hashSequenceIdx := 0
Loop:
	for _, rec := range app.TotalOrdered {
		for _, h := range rec.BlockHashes {
			if hashSequenceIdx >= len(app.DeliverSequence) {
				break Loop
			}
			if h != app.DeliverSequence[hashSequenceIdx] {
				return ErrMismatchTotalOrderingAndDelivered
			}
			hashSequenceIdx++
		}
	}
	if hashSequenceIdx != len(app.DeliverSequence) {
		// The count of delivered blocks should be larger than those delivered
		// by total ordering.
		return ErrMismatchTotalOrderingAndDelivered
	}
	return nil
}

// Check provides a backdoor to check status of App with reader lock.
func (app *App) Check(checker func(*App)) {
	app.ackedLock.RLock()
	defer app.ackedLock.RUnlock()
	app.totalOrderedLock.RLock()
	defer app.totalOrderedLock.RUnlock()
	app.deliveredLock.RLock()
	defer app.deliveredLock.RUnlock()

	checker(app)
}
