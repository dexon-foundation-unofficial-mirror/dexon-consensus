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

package core

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

const (
	epsilon = 1 * time.Microsecond
	tdelay  = 500 * time.Millisecond
)

const (
	infinity uint64 = math.MaxUint64
)

// BlockLattice represents the local view of a single validator.
//
// blockDB stores blocks that are final. blocks stores blocks that are in ToTo
// Status.
type BlockLattice struct {
	owner        types.ValidatorID
	ValidatorSet map[types.ValidatorID]struct{}
	blocks       map[common.Hash]*types.Block

	fmax               int
	phi                int
	lastSeenTimestamps map[types.ValidatorID]time.Time

	blockDB blockdb.BlockDatabase
	app     Application
	mutex   sync.Mutex

	// Reliable Broadcast.
	waitingSet       map[common.Hash]*types.Block
	stronglyAckedSet map[common.Hash]*types.Block
	ackCandidateSet  map[types.ValidatorID]*types.Block
	restricted       map[types.ValidatorID]struct{}

	// Total Ordering.
	pendingSet   map[common.Hash]*types.Block
	candidateSet map[common.Hash]*types.Block
	ABS          map[common.Hash]map[types.ValidatorID]uint64
	AHV          map[common.Hash]map[types.ValidatorID]uint64

	// Timestamp.
	timestampEngine consensusTimestamp
}

// NewBlockLattice returns a new empty BlockLattice instance.
func NewBlockLattice(
	db blockdb.BlockDatabase,
	app Application) *BlockLattice {
	return &BlockLattice{
		ValidatorSet:       make(map[types.ValidatorID]struct{}),
		blocks:             make(map[common.Hash]*types.Block),
		lastSeenTimestamps: make(map[types.ValidatorID]time.Time),
		blockDB:            db,
		app:                app,
		waitingSet:         make(map[common.Hash]*types.Block),
		stronglyAckedSet:   make(map[common.Hash]*types.Block),
		ackCandidateSet:    make(map[types.ValidatorID]*types.Block),
		restricted:         make(map[types.ValidatorID]struct{}),
		pendingSet:         make(map[common.Hash]*types.Block),
		candidateSet:       make(map[common.Hash]*types.Block),
		ABS:                make(map[common.Hash]map[types.ValidatorID]uint64),
		AHV:                make(map[common.Hash]map[types.ValidatorID]uint64),
		timestampEngine:    *newConsensusTimestamp(),
	}
}

// AddValidator adds a validator into the lattice.
func (l *BlockLattice) AddValidator(
	id types.ValidatorID, genesis *types.Block) {

	l.ValidatorSet[id] = struct{}{}
	l.fmax = (len(l.ValidatorSet) - 1) / 3
	l.phi = 2*l.fmax + 1

	// TODO: We should not make genesis blocks 'final' directly.
	genesis.Status = types.BlockStatusFinal
	l.blockDB.Put(*genesis)
}

// SetOwner sets the blocklattice's owner, which is the localview of whom.
func (l *BlockLattice) SetOwner(id types.ValidatorID) {
	if _, exists := l.ValidatorSet[id]; !exists {
		panic("SetOnwer: owner is not a valid validator")
	}
	l.owner = id
}

// getBlock returns a block no matter where it is located at (either local
// blocks cache or blockDB).
func (l *BlockLattice) getBlock(hash common.Hash) *types.Block {
	if b, exists := l.blocks[hash]; exists {
		return b
	}
	if b, err := l.blockDB.Get(hash); err == nil {
		return &b
	}
	return nil
}

// processAcks updates the ack count of the blocks that is acked by *b*.
func (l *BlockLattice) processAcks(b *types.Block) {
	// Always acks it's own parent.
	b.Acks[b.ParentHash] = struct{}{}

	for ackBlockHash := range b.Acks {
		ackedBlock, ok := l.blocks[ackBlockHash]
		if !ok {
			// Acks a finalized block, don't need to increase it's count.
			if l.blockDB.Has(ackBlockHash) {
				continue
			}
			panic(fmt.Sprintf("failed to get block: %v", ackBlockHash))
		}

		// Populate Ackeds.
		if ackedBlock.Ackeds == nil {
			ackedBlock.Ackeds = make(map[common.Hash]struct{})
		}
		ackedBlock.Ackeds[b.Hash] = struct{}{}

		bp := ackedBlock
		for bp != nil && bp.Status < types.BlockStatusAcked {
			if bp.Ackeds == nil {
				bp.Ackeds = make(map[common.Hash]struct{})
			}
			if _, exists := bp.Ackeds[b.Hash]; !exists {
				bp.Ackeds[b.Hash] = struct{}{}
			}

			// Calculate acked by nodes.
			ackedByNodes := make(map[types.ValidatorID]struct{})
			for hash := range bp.Ackeds {
				bp := l.getBlock(hash)
				ackedByNodes[bp.ProposerID] = struct{}{}
			}

			if len(ackedByNodes) > 2*l.fmax {
				bp.Status = types.BlockStatusAcked
				l.stronglyAckedSet[bp.Hash] = bp
			}
			bp = l.getBlock(bp.ParentHash)
		}

		var populateAckBy func(bx, target *types.Block)
		populateAckBy = func(bx, target *types.Block) {
			for ab := range bx.Acks {
				abb := l.getBlock(ab)
				if abb.Status < types.BlockStatusFinal {
					if abb.Ackeds == nil {
						abb.Ackeds = make(map[common.Hash]struct{})
					}
					abb.Ackeds[target.Hash] = struct{}{}
					populateAckBy(abb, target)
				}
			}
		}
		populateAckBy(ackedBlock, b)
	}
}

// updateTimestamps updates the last seen timestamp of the lattice local view.
func (l *BlockLattice) updateTimestamps(b *types.Block) {
	q := b.ProposerID
	l.lastSeenTimestamps[q] = b.Timestamps[q].Add(epsilon)
	for vid := range l.ValidatorSet {
		if b.Timestamps[vid].After(l.lastSeenTimestamps[vid]) {
			l.lastSeenTimestamps[vid] = b.Timestamps[vid]
		}
	}
}

func (l *BlockLattice) recievedAndNotInWaitingSet(hash common.Hash) bool {
	if _, exists := l.blocks[hash]; !exists {
		if !l.blockDB.Has(hash) {
			return false
		}
	}
	return true
}

func (l *BlockLattice) isValidAckCandidate(b *types.Block) bool {
	// Block proposer is not restricted.
	if _, isRestricted := l.restricted[b.ProposerID]; isRestricted {
		return false
	}

	hasHistoryBeenRecieved := func(hash common.Hash) bool {
		bx := l.getBlock(hash)
		if bx == nil {
			return false
		}

		for {
			bx = l.getBlock(bx.ParentHash)
			if bx == nil {
				return false
			}
			if bx.Status == types.BlockStatusFinal {
				return true
			}
		}
	}

	// Previous block is recieved.
	if !hasHistoryBeenRecieved(b.ParentHash) {
		return false
	}

	// All acked blocks are recieved.
	for ackedBlockHash := range b.Acks {
		if !hasHistoryBeenRecieved(ackedBlockHash) {
			return false
		}
	}

	return true
}

// ProcessBlock implements the recieving part of DEXON reliable broadcast.
func (l *BlockLattice) ProcessBlock(b *types.Block, runTotal ...bool) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if b.Hash == b.ParentHash {
		if _, exists := l.ValidatorSet[b.ProposerID]; !exists {
			l.AddValidator(b.ProposerID, b)
		}
	}

	if l.getBlock(b.Hash) != nil {
		return
	}

	// TODO(w): drop if it does not pass sanity check.

	// Store into local blocks cache.
	l.blocks[b.Hash] = b

	if l.isValidAckCandidate(b) {
		l.ackCandidateSet[b.ProposerID] = b
		l.processAcks(b)
	} else {
		l.waitingSet[b.Hash] = b
	}

	// Scan the rest of waiting set for valid candidate.
	for bpHash, bp := range l.waitingSet {
		if l.isValidAckCandidate(bp) {
			l.ackCandidateSet[bp.ProposerID] = bp
			l.processAcks(bp)
			delete(l.waitingSet, bpHash)
		}
	}

IterateStronglyAckedSet:
	for bpHash, bp := range l.stronglyAckedSet {
		for ackBlockHash := range bp.Acks {
			bx := l.getBlock(ackBlockHash)
			if bx == nil || bx.Status < types.BlockStatusAcked {
				break IterateStronglyAckedSet
			}
		}
		bp.Status = types.BlockStatusOrdering
		l.pendingSet[bp.Hash] = bp
		delete(l.stronglyAckedSet, bpHash)

		if len(runTotal) > 0 && runTotal[0] {
			l.totalOrdering(bp)
		}
	}
}

// PrepareBlock prepare a block for broadcast.
func (l *BlockLattice) PrepareBlock(b *types.Block) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	b.Acks = make(map[common.Hash]struct{})
	for _, bp := range l.ackCandidateSet {
		b.Acks[bp.Hash] = struct{}{}
		l.updateTimestamps(b)
	}
	l.lastSeenTimestamps[l.owner] = time.Now().UTC()

	b.Timestamps = make(map[types.ValidatorID]time.Time)
	for vID, ts := range l.lastSeenTimestamps {
		b.Timestamps[vID] = ts
	}

	//l.ProcessBlock(b)
	l.ackCandidateSet = make(map[types.ValidatorID]*types.Block)
}

// detectNack implements the NACK detection.
func (l *BlockLattice) detectNack() {

}

func (l *BlockLattice) abs() map[types.ValidatorID]struct{} {
	abs := make(map[types.ValidatorID]struct{})
	for blockHash := range l.candidateSet {
		for x := range l.ABS[blockHash] {
			abs[x] = struct{}{}
		}
	}
	return abs
}

func (l *BlockLattice) calculateABSofBlock(b *types.Block) {
	// Calculate ABS of a block.
	l.ABS[b.Hash] = make(map[types.ValidatorID]uint64)

	var calculateABSRecursive func(target *types.Block)

	calculateABSRecursive = func(target *types.Block) {
		for hash := range target.Ackeds {
			ackedByBlock := l.getBlock(hash)
			if ackedByBlock.Status != types.BlockStatusOrdering {
				continue
			}
			v, exists := l.ABS[b.Hash][ackedByBlock.ProposerID]
			if !exists || ackedByBlock.Height < v {
				l.ABS[b.Hash][ackedByBlock.ProposerID] = ackedByBlock.Height
			}
			calculateABSRecursive(ackedByBlock)
		}
	}

	// ABS always include the block's proposer
	l.ABS[b.Hash][b.ProposerID] = b.Height

	calculateABSRecursive(b)
}

func (l *BlockLattice) calculateAHVofBlock(
	b *types.Block, globalMins map[types.ValidatorID]uint64) {

	// Calculate ABS of a block.
	l.AHV[b.Hash] = make(map[types.ValidatorID]uint64)

	for v := range l.ValidatorSet {
		gv, gExists := globalMins[v]
		lv, lExists := l.ABS[b.Hash][v]

		if !gExists {
			// Do nothing.
		} else if !lExists || lv > gv {
			l.AHV[b.Hash][v] = infinity
		} else {
			l.AHV[b.Hash][v] = gv
		}
	}
}

func (l *BlockLattice) updateABSAHV() {
	globalMins := make(map[types.ValidatorID]uint64)

	for _, block := range l.pendingSet {
		v, exists := globalMins[block.ProposerID]
		if !exists || block.Height < v {
			globalMins[block.ProposerID] = block.Height
		}
	}

	for _, block := range l.candidateSet {
		l.calculateABSofBlock(block)
		l.calculateAHVofBlock(block, globalMins)
	}
}

// totalOrdering implements the DEXON total ordering algorithm.
func (l *BlockLattice) totalOrdering(b *types.Block) {
	acksOnlyFinal := true
	for ackedBlockHash := range b.Acks {
		bp := l.getBlock(ackedBlockHash)
		if bp.Status != types.BlockStatusFinal {
			acksOnlyFinal = false
			break
		}
	}

	if acksOnlyFinal {
		l.candidateSet[b.Hash] = b
	}

	// Update ABS and AHV.
	l.updateABSAHV()
	abs := l.abs()

	// Calculate preceding set.
	precedingSet := make(map[common.Hash]*types.Block)

	// Grade(b', b) = 0 for all b' in candidate set.
	for targetHash, targetBlock := range l.candidateSet {
		winAll := true
		for otherHash := range l.candidateSet {
			if targetHash.Equal(otherHash) {
				continue
			}

			lose := 0
			for vID, targetAHV := range l.AHV[targetHash] {
				if otherAHV, exists := l.AHV[otherHash][vID]; exists {
					if otherAHV < targetAHV {
						lose++
					}
				} else if otherAHV != infinity {
					lose++
				}
			}

			if lose >= l.phi {
				winAll = false
				break
			} else if lose < l.phi-len(l.ValidatorSet)+len(abs) {
				// Do nothing.
			} else {
				winAll = false
				break
			}
		}

		if winAll {
			precedingSet[targetHash] = targetBlock
		}
	}

	// Internal stability.
	winned := false
	for hash := range l.candidateSet {
		if _, exists := precedingSet[hash]; exists {
			continue
		}

		// Grade(b, b') = 1
		for precedingHash := range precedingSet {
			win := 0
			for vID, precedingAHV := range l.AHV[precedingHash] {
				if candidateAHV, exists := l.AHV[hash][vID]; exists {
					if precedingAHV < candidateAHV {
						win++
					}
				} else if precedingAHV != infinity {
					win++
				}
			}
			if win > l.phi {
				winned = true
				break
			}
		}
		if !winned {
			return
		}
	}

	earlyDelivery := false

	// Does not satisfy External stability a.
	if len(abs) < len(l.ValidatorSet) {
		earlyDelivery = true

		// External stability b.
		extBSatisfied := false
		for precedingHash := range precedingSet {
			count := 0
			for _, ahv := range l.AHV[precedingHash] {
				if ahv != infinity {
					count++
				}
			}
			if count > l.phi {
				extBSatisfied = true
				break
			}
		}
		if !extBSatisfied {
			return
		}
		for precedingHash := range precedingSet {
			if len(l.ABS[precedingHash]) < len(l.ValidatorSet)-l.phi {
				extBSatisfied = false
			}
		}
		if !extBSatisfied {
			return
		}
	}

	var output []*types.Block
	for hash, x := range precedingSet {
		output = append(output, x)
		x.Status = types.BlockStatusFinal

		// Remove from pending set and candidate set.
		delete(l.pendingSet, hash)
		delete(l.candidateSet, hash)

		// Delete ABS and AHV
		delete(l.ABS, hash)
		delete(l.AHV, hash)

		// Store output blocks into blockDB.
		l.blockDB.Put(*x)
		delete(l.blocks, hash)
	}
	sort.Sort(types.ByHash(output))

	if len(output) > 0 {
		l.app.TotalOrderingDeliver(output, earlyDelivery)
		blocksReady, _, err := l.timestampEngine.processBlocks(output)
		if err != nil && err != ErrEmptyTimestamps {
			panic(err)
		}
		for _, block := range blocksReady {
			l.app.DeliverBlock(block.Hash, block.ConsensusInfo.Timestamp)
		}
	}

	// Rescan pending blocks to add into candidate set.
	for hash, block := range l.pendingSet {
		if _, exists := l.candidateSet[hash]; exists {
			continue
		}
		acksOnlyFinal := true
		for ackedBlockHash := range block.Acks {
			bp := l.getBlock(ackedBlockHash)
			if bp.Status != types.BlockStatusFinal {
				acksOnlyFinal = false
				break
			}
		}
		if acksOnlyFinal {
			l.candidateSet[hash] = block
		}
	}
}
