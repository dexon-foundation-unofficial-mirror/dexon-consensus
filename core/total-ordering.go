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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

const (
	infinity uint64 = math.MaxUint64
)

var (
	// ErrNotValidDAG would be reported when block subbmitted to totalOrdering
	// didn't form a DAG.
	ErrNotValidDAG = fmt.Errorf("not a valid dag")
	// ErrValidatorNotRecognized means the validator is unknown to this module.
	ErrValidatorNotRecognized = fmt.Errorf("validator not recognized")
)

// totalOrderinWinRecord caches which validators this candidate
// wins another one based on their height vector.
type totalOrderingWinRecord struct {
	wins  []int8
	count uint
}

func (rec *totalOrderingWinRecord) reset() {
	rec.count = 0
	for idx := range rec.wins {
		rec.wins[idx] = 0
	}
}

func newTotalOrderingWinRecord(validatorCount int) (
	rec *totalOrderingWinRecord) {

	rec = &totalOrderingWinRecord{}
	rec.reset()
	rec.wins = make([]int8, validatorCount)
	return
}

// grade implements the 'grade' potential function described in white paper.
func (rec *totalOrderingWinRecord) grade(
	validatorCount, phi uint64,
	globalAnsLength uint64) int {

	if uint64(rec.count) >= phi {
		return 1
	} else if uint64(rec.count) < phi-validatorCount+globalAnsLength {
		return 0
	} else {
		return -1
	}
}

// totalOrderingHeightRecord records two things:
//  - the minimum heiht of block from that validator acking this block.
//  - the count of blocks from that validator acking this block.
type totalOrderingHeightRecord struct{ minHeight, count uint64 }

// totalOrderingObjectCache caches objects for reuse.
// The target object is map because:
//  - reuse map would prevent it grows during usage, when map grows,
//    hashes of key would be recaculated, bucket reallocated, and values
//    are copied.
// However, to reuse a map, we have no easy way to erase its content but
// iterating its keys and delete corresponding values.
type totalOrderingObjectCache struct {
	ackedStatus         [][]*totalOrderingHeightRecord
	heightVectors       [][]uint64
	winRecordContainers [][]*totalOrderingWinRecord
	ackedVectors        []map[common.Hash]struct{}
	winRecordPool       sync.Pool
	validatorCount      int
}

// newTotalOrderingObjectCache constructs an totalOrderingObjectCache
// instance.
func newTotalOrderingObjectCache(validatorCount int) *totalOrderingObjectCache {
	return &totalOrderingObjectCache{
		winRecordPool: sync.Pool{
			New: func() interface{} {
				return newTotalOrderingWinRecord(validatorCount)
			},
		},
		validatorCount: validatorCount,
	}
}

// requestAckedStatus requests a structure to record acking status of one
// candidate (or a global view of acking status of pending set).
func (cache *totalOrderingObjectCache) requestAckedStatus() (
	acked []*totalOrderingHeightRecord) {

	if len(cache.ackedStatus) == 0 {
		acked = make([]*totalOrderingHeightRecord, cache.validatorCount)
		for idx := range acked {
			acked[idx] = &totalOrderingHeightRecord{count: 0}
		}
	} else {
		acked, cache.ackedStatus =
			cache.ackedStatus[len(cache.ackedStatus)-1],
			cache.ackedStatus[:len(cache.ackedStatus)-1]
		// Reset acked status.
		for idx := range acked {
			acked[idx].count = 0
		}
	}
	return
}

// recycleAckedStatys recycles the structure to record acking status.
func (cache *totalOrderingObjectCache) recycleAckedStatus(
	acked []*totalOrderingHeightRecord) {

	cache.ackedStatus = append(cache.ackedStatus, acked)
}

// requestWinRecord requests an totalOrderingWinRecord instance.
func (cache *totalOrderingObjectCache) requestWinRecord() (
	win *totalOrderingWinRecord) {

	win = cache.winRecordPool.Get().(*totalOrderingWinRecord)
	win.reset()
	return
}

// recycleWinRecord recycles an totalOrderingWinRecord instance.
func (cache *totalOrderingObjectCache) recycleWinRecord(
	win *totalOrderingWinRecord) {

	if win == nil {
		return
	}
	cache.winRecordPool.Put(win)
}

// requestHeightVector requests a structure to record acking heights
// of one candidate.
func (cache *totalOrderingObjectCache) requestHeightVector() (
	hv []uint64) {

	if len(cache.heightVectors) == 0 {
		hv = make([]uint64, cache.validatorCount)
	} else {
		hv, cache.heightVectors =
			cache.heightVectors[len(cache.heightVectors)-1],
			cache.heightVectors[:len(cache.heightVectors)-1]
	}
	for idx := range hv {
		hv[idx] = infinity
	}
	return
}

// recycleHeightVector recycles an instance to record acking heights
// of one candidate.
func (cache *totalOrderingObjectCache) recycleHeightVector(
	hv []uint64) {

	cache.heightVectors = append(cache.heightVectors, hv)
}

// requestWinRecordContainer requests a map of totalOrderingWinRecord.
func (cache *totalOrderingObjectCache) requestWinRecordContainer() (
	con []*totalOrderingWinRecord) {

	if len(cache.winRecordContainers) == 0 {
		con = make([]*totalOrderingWinRecord, cache.validatorCount)
	} else {
		con, cache.winRecordContainers =
			cache.winRecordContainers[len(cache.winRecordContainers)-1],
			cache.winRecordContainers[:len(cache.winRecordContainers)-1]
		for idx := range con {
			con[idx] = nil
		}
	}
	return
}

// recycleWinRecordContainer recycles a map of totalOrderingWinRecord.
func (cache *totalOrderingObjectCache) recycleWinRecordContainer(
	con []*totalOrderingWinRecord) {

	cache.winRecordContainers = append(cache.winRecordContainers, con)
}

// requestAckedVector requests an acked vector instance.
func (cache *totalOrderingObjectCache) requestAckedVector() (
	acked map[common.Hash]struct{}) {

	if len(cache.ackedVectors) == 0 {
		acked = make(map[common.Hash]struct{})
	} else {
		acked, cache.ackedVectors =
			cache.ackedVectors[len(cache.ackedVectors)-1],
			cache.ackedVectors[:len(cache.ackedVectors)-1]
		for k := range acked {
			delete(acked, k)
		}
	}
	return
}

// recycleAckedVector recycles an acked vector instance.
func (cache *totalOrderingObjectCache) recycleAckedVector(
	acked map[common.Hash]struct{}) {

	if acked == nil {
		return
	}
	cache.ackedVectors = append(cache.ackedVectors, acked)
}

// totalOrderingCandidateInfo describes proceeding status for one candidate,
// including:
//  - acked status as height records, which could keep 'how many blocks from
//    one validator acking this candidate.
//  - cached height vector, which valid height based on K-level used for
//    comparison in 'grade' function.
//  - cached result of grade function to other candidates.
//
// Height Record:
//  When block A acks block B, all blocks proposed from the same proposer
//  as block A with higher height would also acks block B. Therefore,
//  we just need to record:
//   - the minimum height of acking block from that proposer
//   - count of acking blocks from that proposer
//  to repsent the acking status for block A.
type totalOrderingCandidateInfo struct {
	ackedStatus        []*totalOrderingHeightRecord
	cachedHeightVector []uint64
	winRecords         []*totalOrderingWinRecord
	hash               common.Hash
}

// newTotalOrderingCandidateInfo constructs an totalOrderingCandidateInfo
// instance.
func newTotalOrderingCandidateInfo(
	candidateHash common.Hash,
	objCache *totalOrderingObjectCache) *totalOrderingCandidateInfo {

	return &totalOrderingCandidateInfo{
		ackedStatus: objCache.requestAckedStatus(),
		winRecords:  objCache.requestWinRecordContainer(),
		hash:        candidateHash,
	}
}

// clean clear information related to another candidate, which should be called
// when that candidate is selected as deliver set.
func (v *totalOrderingCandidateInfo) clean(otherCandidateIndex int) {
	v.winRecords[otherCandidateIndex] = nil
}

// recycle objects for later usage, this eases the loading of
// golangs' GC.
func (v *totalOrderingCandidateInfo) recycle(
	objCache *totalOrderingObjectCache) {

	if v.winRecords != nil {
		for _, win := range v.winRecords {
			objCache.recycleWinRecord(win)
		}
		objCache.recycleWinRecordContainer(v.winRecords)
	}
	if v.cachedHeightVector != nil {
		objCache.recycleHeightVector(v.cachedHeightVector)
	}
	objCache.recycleAckedStatus(v.ackedStatus)
}

// addBlock would update totalOrderingCandidateInfo, it's caller's duty
// to make sure the input block acutally acking the target block.
func (v *totalOrderingCandidateInfo) addBlock(
	b *types.Block, proposerIndex int) (err error) {

	rec := v.ackedStatus[proposerIndex]
	if rec.count == 0 {
		rec.minHeight = b.Height
		rec.count = 1
	} else {
		if b.Height < rec.minHeight {
			err = ErrNotValidDAG
			return
		}
		rec.count++
	}
	return
}

// getAckingNodeSetLength would generate the Acking Node Set and return its
// length. Only block height larger than
//
//    global minimum height + k
//
// would be taken into consideration, ex.
//
//  For some validator X:
//   - the global minimum acking height = 1,
//   - k = 1
//  then only block height >= 2 would be added to acking node set.
func (v *totalOrderingCandidateInfo) getAckingNodeSetLength(
	global *totalOrderingCandidateInfo,
	k uint64) (count uint64) {

	var rec *totalOrderingHeightRecord
	for idx, gRec := range global.ackedStatus {
		if gRec.count == 0 {
			continue
		}
		rec = v.ackedStatus[idx]
		if rec.count == 0 {
			continue
		}
		// This line would check if these two ranges would overlap:
		//  - (global minimum height + k, infinity)
		//  - (local minimum height, local minimum height + count - 1)
		if rec.minHeight+rec.count-1 >= gRec.minHeight+k {
			count++
		}
	}
	return
}

// updateAckingHeightVector would cached acking height vector.
//
// Only block height equals to (global minimum block height + k) would be
// taken into consideration.
func (v *totalOrderingCandidateInfo) updateAckingHeightVector(
	global *totalOrderingCandidateInfo,
	k uint64,
	dirtyValidatorIndexes []int,
	objCache *totalOrderingObjectCache) {

	var (
		idx       int
		gRec, rec *totalOrderingHeightRecord
	)

	// The reason not to merge the two loops is the iteration over map
	// is expensive when validator count is large, iterating over dirty
	// validators is cheaper.
	// TODO(mission): merge the code in this if/else if the performance won't be
	//                downgraded when adding a function for the shared part.
	if v.cachedHeightVector == nil {
		// Generate height vector from scratch.
		v.cachedHeightVector = objCache.requestHeightVector()
		for idx, gRec = range global.ackedStatus {
			if gRec.count <= k {
				continue
			}
			rec = v.ackedStatus[idx]
			if rec.count == 0 {
				v.cachedHeightVector[idx] = infinity
			} else if rec.minHeight <= gRec.minHeight+k {
				// This check is sufficient to make sure the block height:
				//
				//   gRec.minHeight + k
				//
				// would be included in this totalOrderingCandidateInfo.
				v.cachedHeightVector[idx] = gRec.minHeight + k
			} else {
				v.cachedHeightVector[idx] = infinity
			}
		}
	} else {
		// Return the cached one, only update dirty fields.
		for _, idx = range dirtyValidatorIndexes {
			gRec = global.ackedStatus[idx]
			if gRec.count == 0 || gRec.count <= k {
				v.cachedHeightVector[idx] = infinity
				continue
			}
			rec = v.ackedStatus[idx]
			if rec.count == 0 {
				v.cachedHeightVector[idx] = infinity
			} else if rec.minHeight <= gRec.minHeight+k {
				v.cachedHeightVector[idx] = gRec.minHeight + k
			} else {
				v.cachedHeightVector[idx] = infinity
			}
		}
	}
	return
}

// updateWinRecord setup win records between two candidates.
func (v *totalOrderingCandidateInfo) updateWinRecord(
	otherCandidateIndex int,
	other *totalOrderingCandidateInfo,
	dirtyValidatorIndexes []int,
	objCache *totalOrderingObjectCache) {

	var (
		idx    int
		height uint64
	)

	// The reason not to merge the two loops is the iteration over map
	// is expensive when validator count is large, iterating over dirty
	// validators is cheaper.
	// TODO(mission): merge the code in this if/else if add a function won't
	//                affect the performance.
	win := v.winRecords[otherCandidateIndex]
	if win == nil {
		win = objCache.requestWinRecord()
		v.winRecords[otherCandidateIndex] = win
		for idx, height = range v.cachedHeightVector {
			if height == infinity {
				continue
			}
			if other.cachedHeightVector[idx] == infinity {
				win.wins[idx] = 1
				win.count++
			}
		}
	} else {
		for _, idx = range dirtyValidatorIndexes {
			if v.cachedHeightVector[idx] == infinity {
				if win.wins[idx] == 1 {
					win.wins[idx] = 0
					win.count--
				}
				continue
			}
			if other.cachedHeightVector[idx] == infinity {
				if win.wins[idx] == 0 {
					win.wins[idx] = 1
					win.count++
				}
			} else {
				if win.wins[idx] == 1 {
					win.wins[idx] = 0
					win.count--
				}
			}
		}
	}
}

// totalOrderingGroupVector keeps global status of current pending set.
type totalOrderingGlobalVector struct {
	// blocks stores all blocks grouped by their proposers and
	// sorted by their block height.
	//
	// TODO(mission): the way we use this slice would make it reallocate frequently.
	blocks [][]*types.Block

	// cachedCandidateInfo is an totalOrderingCandidateInfo instance,
	// which is just used for actual candidates to calculate height vector.
	cachedCandidateInfo *totalOrderingCandidateInfo
}

func newTotalOrderingGlobalVector(
	validatorCount int) *totalOrderingGlobalVector {

	return &totalOrderingGlobalVector{
		blocks: make([][]*types.Block, validatorCount),
	}
}

func (global *totalOrderingGlobalVector) addBlock(
	b *types.Block, proposerIndex int) (err error) {

	blocksFromProposer := global.blocks[proposerIndex]
	if len(blocksFromProposer) > 0 {
		lastBlock := blocksFromProposer[len(blocksFromProposer)-1]
		if b.Height-lastBlock.Height != 1 {
			err = ErrNotValidDAG
			return
		}
	}
	global.blocks[proposerIndex] = append(blocksFromProposer, b)
	return
}

// updateCandidateInfo udpate cached candidate info.
func (global *totalOrderingGlobalVector) updateCandidateInfo(
	dirtyValidatorIndexes []int, objCache *totalOrderingObjectCache) {

	var (
		idx    int
		blocks []*types.Block
		info   *totalOrderingCandidateInfo
		rec    *totalOrderingHeightRecord
	)

	if global.cachedCandidateInfo == nil {
		info = newTotalOrderingCandidateInfo(common.Hash{}, objCache)
		for idx, blocks = range global.blocks {
			if len(blocks) == 0 {
				continue
			}
			rec = info.ackedStatus[idx]
			rec.minHeight = blocks[0].Height
			rec.count = uint64(len(blocks))
		}
		global.cachedCandidateInfo = info
	} else {
		info = global.cachedCandidateInfo
		for _, idx = range dirtyValidatorIndexes {
			blocks = global.blocks[idx]
			if len(blocks) == 0 {
				info.ackedStatus[idx].count = 0
				continue
			}
			rec = info.ackedStatus[idx]
			rec.minHeight = blocks[0].Height
			rec.count = uint64(len(blocks))
		}
	}
	return
}

// totalOrdering represent a process unit to handle total ordering
// for blocks.
type totalOrdering struct {
	// pendings stores blocks awaiting to be ordered.
	pendings map[common.Hash]*types.Block

	// k represents the k in 'k-level total ordering'.
	// In short, only block height equals to (global minimum height + k)
	// would be taken into consideration.
	k uint64

	// phi is a const to control how strong the leading preceding block
	// should be.
	phi uint64

	// validatorCount is the count of validator set.
	validatorCount uint64

	// globalVector group all pending blocks by proposers and
	// sort them by block height. This structure is helpful when:
	//
	//  - build global height vector
	//  - picking candidates next round
	globalVector *totalOrderingGlobalVector

	// candidates caches result of potential function during generating
	// preceding sets.
	candidates []*totalOrderingCandidateInfo

	// acked cache the 'block A acked by block B' relation by
	// keeping a record in acked[A.Hash][B.Hash]
	acked map[common.Hash]map[common.Hash]struct{}

	// dirtyValidatorIndexes records which validatorID that should be updated
	// for all cached status (win record, acking status).
	dirtyValidatorIndexes []int

	// objCache caches allocated objects, like map.
	objCache *totalOrderingObjectCache

	// validatorIndexMapping maps validatorID to an unique integer, which
	// could be used as slice index.
	validatorIndexMapping map[types.ValidatorID]int

	// candidateIndexMapping maps block hashes of candidates to an unique
	// integer, which could be used as slice index.
	candidateIndexMapping map[common.Hash]int

	// allocatedCandidateSlotIndexes records all used slot indexes in
	// candidates slice.
	allocatedCandidateSlotIndexes []int
}

func newTotalOrdering(
	k, phi uint64, validators types.ValidatorIDs) *totalOrdering {

	// Setup validatorID to index mapping.
	validatorIndexMapping := make(map[types.ValidatorID]int)
	for _, vID := range validators {
		validatorIndexMapping[vID] = len(validatorIndexMapping)
	}
	validatorCount := len(validators)
	return &totalOrdering{
		pendings:              make(map[common.Hash]*types.Block),
		k:                     k,
		phi:                   phi,
		validatorCount:        uint64(validatorCount),
		globalVector:          newTotalOrderingGlobalVector(validatorCount),
		dirtyValidatorIndexes: make([]int, 0, validatorCount),
		acked:                 make(map[common.Hash]map[common.Hash]struct{}),
		objCache:              newTotalOrderingObjectCache(validatorCount),
		validatorIndexMapping: validatorIndexMapping,
		candidateIndexMapping: make(map[common.Hash]int),
		candidates: make(
			[]*totalOrderingCandidateInfo, validatorCount),
		allocatedCandidateSlotIndexes: make([]int, 0, validatorCount),
	}
}

// buildBlockRelation populates the acked according their acking relationships.
// This function would update all blocks implcitly acked by input block
// recursively.
func (to *totalOrdering) buildBlockRelation(b *types.Block) {
	var (
		curBlock, nextBlock      *types.Block
		ack                      common.Hash
		acked                    map[common.Hash]struct{}
		exists, alreadyPopulated bool
		toCheck                  = []*types.Block{b}
	)
	for {
		if len(toCheck) == 0 {
			break
		}
		curBlock, toCheck = toCheck[len(toCheck)-1], toCheck[:len(toCheck)-1]
		for ack = range curBlock.Acks {
			if acked, exists = to.acked[ack]; !exists {
				acked = to.objCache.requestAckedVector()
				to.acked[ack] = acked
			}
			// This means we've walked this block already.
			if _, alreadyPopulated = acked[b.Hash]; alreadyPopulated {
				continue
			}
			acked[b.Hash] = struct{}{}
			// See if we need to go forward.
			if nextBlock, exists = to.pendings[ack]; !exists {
				continue
			} else {
				toCheck = append(toCheck, nextBlock)
			}
		}
	}
}

// clean would remove a block from working set. This behaviour
// would prevent our memory usage growing infinity.
func (to *totalOrdering) clean(h common.Hash) {
	to.objCache.recycleAckedVector(to.acked[h])
	delete(to.acked, h)
	delete(to.pendings, h)
	slotIndex := to.candidateIndexMapping[h]
	to.candidates[slotIndex].recycle(to.objCache)
	to.candidates[slotIndex] = nil
	delete(to.candidateIndexMapping, h)
	// Remove this candidate from allocated slot indexes.
	to.allocatedCandidateSlotIndexes = removeFromSortedIntSlice(
		to.allocatedCandidateSlotIndexes, slotIndex)
	// Clear records of this candidate from other candidates.
	for _, idx := range to.allocatedCandidateSlotIndexes {
		to.candidates[idx].clean(slotIndex)
	}
}

// updateVectors is a helper function to update all cached vectors.
func (to *totalOrdering) updateVectors(
	b *types.Block, proposerIndex int) (err error) {
	var (
		candidateHash  common.Hash
		candidateIndex int
		acked          bool
	)
	// Update global height vector
	err = to.globalVector.addBlock(b, proposerIndex)
	if err != nil {
		return
	}

	// Update acking status of candidates.
	for candidateHash, candidateIndex = range to.candidateIndexMapping {
		if _, acked = to.acked[candidateHash][b.Hash]; !acked {
			continue
		}
		if err = to.candidates[candidateIndex].addBlock(
			b, proposerIndex); err != nil {

			return
		}
	}
	return
}

// prepareCandidate is a helper function to
// build totalOrderingCandidateInfo for new candidate.
func (to *totalOrdering) prepareCandidate(
	candidate *types.Block, proposerIndex int) {

	var (
		info = newTotalOrderingCandidateInfo(
			candidate.Hash, to.objCache)
	)

	to.candidates[proposerIndex] = info
	to.candidateIndexMapping[candidate.Hash] = proposerIndex
	// Add index to slot to allocated list, make sure the modified list sorted.
	to.allocatedCandidateSlotIndexes = append(
		to.allocatedCandidateSlotIndexes, proposerIndex)
	sort.Ints(to.allocatedCandidateSlotIndexes)

	info.ackedStatus[proposerIndex] = &totalOrderingHeightRecord{
		minHeight: candidate.Height,
		count:     uint64(len(to.globalVector.blocks[proposerIndex])),
	}
	ackedsForCandidate, exists := to.acked[candidate.Hash]
	if !exists {
		// This candidate is acked by nobody.
		return
	}
	var rec *totalOrderingHeightRecord
	for idx, blocks := range to.globalVector.blocks {
		if idx == proposerIndex {
			continue
		}
		for i, b := range blocks {
			if _, acked := ackedsForCandidate[b.Hash]; !acked {
				continue
			}
			// If this block acks this candidate, all newer blocks
			// from the same validator also 'indirect' acks it.
			rec = info.ackedStatus[idx]
			rec.minHeight = b.Height
			rec.count = uint64(len(blocks) - i)
			break
		}
	}
	return
}

// isAckOnlyPrecedings is a helper function to check if a block
// only contain acks to delivered blocks.
func (to *totalOrdering) isAckOnlyPrecedings(b *types.Block) bool {
	for ack := range b.Acks {
		if _, pending := to.pendings[ack]; pending {
			return false
		}
	}
	return true
}

// output is a helper function to finish the delivery of
// deliverable preceding set.
func (to *totalOrdering) output(precedings map[common.Hash]struct{}) (ret []*types.Block) {
	for p := range precedings {
		// Remove the first element from corresponding blockVector.
		b := to.pendings[p]
		// TODO(mission): This way to use slice makes it reallocate frequently.
		blockProposerIndex := to.validatorIndexMapping[b.ProposerID]
		to.globalVector.blocks[blockProposerIndex] =
			to.globalVector.blocks[blockProposerIndex][1:]
		ret = append(ret, b)

		// Remove block relations.
		to.clean(p)
		to.dirtyValidatorIndexes = append(
			to.dirtyValidatorIndexes, blockProposerIndex)
	}
	sort.Sort(types.ByHash(ret))

	// Find new candidates from tip of globalVector of each validator.
	// The complexity here is O(N^2logN).
	// TODO(mission): only those tips that acking some blocks in
	//                the devliered set should be checked. This
	//                improvment related to the latency introduced by K.
	for _, blocks := range to.globalVector.blocks {
		if len(blocks) == 0 {
			continue
		}
		tip := blocks[0]
		if _, alreadyCandidate :=
			to.candidateIndexMapping[tip.Hash]; alreadyCandidate {
			continue
		}
		if !to.isAckOnlyPrecedings(tip) {
			continue
		}
		// Build totalOrderingCandidateInfo for new candidate.
		to.prepareCandidate(
			tip,
			to.validatorIndexMapping[tip.ProposerID])
	}
	return ret
}

// generateDeliverSet would:
//  - generate preceding set
//  - check if the preceding set deliverable by checking potential function
func (to *totalOrdering) generateDeliverSet() (
	delivered map[common.Hash]struct{}, early bool) {

	var (
		candidateIndex, otherCandidateIndex int
		info, otherInfo                     *totalOrderingCandidateInfo
		precedings                          = make(map[int]struct{})
	)

	to.globalVector.updateCandidateInfo(to.dirtyValidatorIndexes, to.objCache)
	globalInfo := to.globalVector.cachedCandidateInfo
	for _, candidateIndex = range to.allocatedCandidateSlotIndexes {
		to.candidates[candidateIndex].updateAckingHeightVector(
			globalInfo, to.k, to.dirtyValidatorIndexes, to.objCache)
	}

	// Update winning records for each candidate.
	// TODO(mission): It's not reasonable to
	//                request one routine for each candidate, the context
	//                switch rate would be high.
	var wg sync.WaitGroup
	wg.Add(len(to.allocatedCandidateSlotIndexes))
	for _, candidateIndex := range to.allocatedCandidateSlotIndexes {
		info = to.candidates[candidateIndex]
		go func(can int, canInfo *totalOrderingCandidateInfo) {
			for _, otherCandidateIndex := range to.allocatedCandidateSlotIndexes {
				if can == otherCandidateIndex {
					continue
				}
				canInfo.updateWinRecord(
					otherCandidateIndex,
					to.candidates[otherCandidateIndex],
					to.dirtyValidatorIndexes,
					to.objCache)
			}
			wg.Done()
		}(candidateIndex, info)
	}
	wg.Wait()

	// Reset dirty validators.
	to.dirtyValidatorIndexes = to.dirtyValidatorIndexes[:0]

	globalAnsLength := globalInfo.getAckingNodeSetLength(globalInfo, to.k)
CheckNextCandidateLoop:
	for _, candidateIndex = range to.allocatedCandidateSlotIndexes {
		info = to.candidates[candidateIndex]
		for _, otherCandidateIndex = range to.allocatedCandidateSlotIndexes {
			if candidateIndex == otherCandidateIndex {
				continue
			}
			otherInfo = to.candidates[otherCandidateIndex]
			if otherInfo.winRecords[candidateIndex].grade(
				to.validatorCount, to.phi, globalAnsLength) != 0 {

				continue CheckNextCandidateLoop
			}
		}
		precedings[candidateIndex] = struct{}{}
	}
	if len(precedings) == 0 {
		return
	}

	// internal is a helper function to verify internal stability.
	internal := func() bool {
		var (
			isPreceding, beaten bool
			p                   int
		)
		for _, candidateIndex = range to.allocatedCandidateSlotIndexes {
			if _, isPreceding = precedings[candidateIndex]; isPreceding {
				continue
			}
			beaten = false
			for p = range precedings {
				if beaten = to.candidates[p].winRecords[candidateIndex].grade(
					to.validatorCount, to.phi, globalAnsLength) == 1; beaten {
					break
				}
			}
			if !beaten {
				return false
			}
		}
		return true
	}

	// checkAHV is a helper function to verify external stability.
	// It would make sure some preceding block is strong enough
	// to lead the whole preceding set.
	checkAHV := func() bool {
		var (
			height, count uint64
			p             int
		)
		for p = range precedings {
			count = 0
			info = to.candidates[p]
			for _, height = range info.cachedHeightVector {
				if height != infinity {
					count++
					if count > to.phi {
						return true
					}
				}
			}
		}
		return false
	}

	// checkANS is a helper function to verify external stability.
	// It would make sure all preceding blocks are strong enough
	// to be delivered.
	checkANS := func() bool {
		var validatorAnsLength uint64
		for p := range precedings {
			validatorAnsLength = to.candidates[p].getAckingNodeSetLength(
				globalInfo, to.k)
			if uint64(validatorAnsLength) < to.validatorCount-to.phi {
				return false
			}
		}
		return true
	}

	// If all validators propose enough blocks, we should force
	// to deliver since the whole picture of the DAG is revealed.
	if globalAnsLength != to.validatorCount {
		// Check internal stability first.
		if !internal() {
			return
		}

		// The whole picture is not ready, we need to check if
		// exteranl stability is met, and we can deliver earlier.
		if checkAHV() && checkANS() {
			early = true
		} else {
			return
		}
	}
	delivered = make(map[common.Hash]struct{})
	for p := range precedings {
		delivered[to.candidates[p].hash] = struct{}{}
	}
	return
}

// processBlock is the entry point of totalOrdering.
func (to *totalOrdering) processBlock(b *types.Block) (
	delivered []*types.Block, early bool, err error) {
	// NOTE: I assume the block 'b' is already safe for total ordering.
	//       That means, all its acking blocks are during/after
	//       total ordering stage.

	blockProposerIndex, exists := to.validatorIndexMapping[b.ProposerID]
	if !exists {
		err = ErrValidatorNotRecognized
		return
	}

	to.pendings[b.Hash] = b
	to.buildBlockRelation(b)
	if err = to.updateVectors(b, blockProposerIndex); err != nil {
		return
	}
	if to.isAckOnlyPrecedings(b) {
		to.prepareCandidate(b, blockProposerIndex)
	}
	// Mark the proposer of incoming block as dirty.
	to.dirtyValidatorIndexes = append(
		to.dirtyValidatorIndexes, blockProposerIndex)
	hashes, early := to.generateDeliverSet()

	// output precedings
	delivered = to.output(hashes)
	return
}
