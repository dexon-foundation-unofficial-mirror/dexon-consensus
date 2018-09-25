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

package test

import (
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

var (
	// ErrEmptyDeliverSequence means there is no delivery event in this App
	// instance.
	ErrEmptyDeliverSequence = fmt.Errorf("emptry deliver sequence")
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
	// ErrDeliveredBlockNotAcked means some block delivered (confirmed) but
	// not strongly acked.
	ErrDeliveredBlockNotAcked = fmt.Errorf("delivered block not acked")
	// ErrMismatchTotalOrderingAndDelivered mean the sequence of total ordering
	// and delivered are different.
	ErrMismatchTotalOrderingAndDelivered = fmt.Errorf(
		"mismatch total ordering and delivered sequence")
	// ErrWitnessAckUnknownBlock means the witness ack is acking on the unknown
	// block.
	ErrWitnessAckUnknownBlock = fmt.Errorf(
		"witness ack on unknown block")
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
	Early       bool
	When        time.Time
}

// AppDeliveredRecord caches information when this application received
// a block delivered notification.
type AppDeliveredRecord struct {
	ConsensusTime time.Time
	When          time.Time
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
	WitnessAckSequence []*types.WitnessAck
	witnessAckLock     sync.RWMutex
	witnessResultChan  chan types.WitnessResult
}

// NewApp constructs a TestApp instance.
func NewApp() *App {
	return &App{
		Acked:              make(map[common.Hash]*AppAckedRecord),
		TotalOrdered:       []*AppTotalOrderRecord{},
		TotalOrderedByHash: make(map[common.Hash]*AppTotalOrderRecord),
		Delivered:          make(map[common.Hash]*AppDeliveredRecord),
		DeliverSequence:    common.Hashes{},
		witnessResultChan:  make(chan types.WitnessResult),
	}
}

// PreparePayload implements Application interface.
func (app *App) PreparePayload(position types.Position) []byte {
	return []byte{}
}

// VerifyPayload implements Application.
func (app *App) VerifyPayload(payloads []byte) bool {
	return true
}

// BlockConfirmed implements Application interface.
func (app *App) BlockConfirmed(_ common.Hash) {
}

// StronglyAcked implements Application interface.
func (app *App) StronglyAcked(blockHash common.Hash) {
	app.ackedLock.Lock()
	defer app.ackedLock.Unlock()

	app.Acked[blockHash] = &AppAckedRecord{When: time.Now().UTC()}
}

// TotalOrderingDelivered implements Application interface.
func (app *App) TotalOrderingDelivered(blockHashes common.Hashes, early bool) {
	app.totalOrderedLock.Lock()
	defer app.totalOrderedLock.Unlock()

	rec := &AppTotalOrderRecord{
		BlockHashes: blockHashes,
		Early:       early,
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
func (app *App) BlockDelivered(block types.Block) {
	app.deliveredLock.Lock()
	defer app.deliveredLock.Unlock()

	app.Delivered[block.Hash] = &AppDeliveredRecord{
		ConsensusTime: block.Witness.Timestamp,
		When:          time.Now().UTC(),
	}
	app.DeliverSequence = append(app.DeliverSequence, block.Hash)
}

// BlockProcessedChan returns a channel to receive the block hashes that have
// finished processing by the application.
func (app *App) BlockProcessedChan() <-chan types.WitnessResult {
	return app.witnessResultChan
}

// WitnessAckDelivered implements Application interface.
func (app *App) WitnessAckDelivered(witnessAck *types.WitnessAck) {
	app.witnessAckLock.Lock()
	defer app.witnessAckLock.Unlock()

	app.WitnessAckSequence = append(app.WitnessAckSequence, witnessAck)
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

	// Make sure that witnessAck is acking the correct block.
	app.witnessAckLock.RLock()
	defer app.witnessAckLock.RUnlock()
	for _, witnessAck := range app.WitnessAckSequence {
		if _, exists := app.Delivered[witnessAck.WitnessBlockHash]; !exists {
			return ErrWitnessAckUnknownBlock
		}
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
