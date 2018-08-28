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

// ErrNotValidDAG would be reported when block subbmitted to totalOrdering
// didn't form a DAG.
var ErrNotValidDAG = fmt.Errorf("not a valid dag")

// totalOrderinWinRecord caches which validators this candidate
// wins another one based on their height vector.
type totalOrderingWinRecord map[types.ValidatorID]struct{}

// grade implements the 'grade' potential function described in white paper.
func (rec totalOrderingWinRecord) grade(
	validatorCount, phi uint64,
	globalAnsLength uint64) int {

	if uint64(len(rec)) >= phi {
		return 1
	} else if uint64(len(rec)) < phi-validatorCount+globalAnsLength {
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
	ackedStatus         []map[types.ValidatorID]*totalOrderingHeightRecord
	winRecordContainers []map[common.Hash]totalOrderingWinRecord
	heightVectors       []map[types.ValidatorID]uint64
	ackedVectors        []map[common.Hash]struct{}
	winRecordPool       sync.Pool
}

// newTotalOrderingObjectCache constructs an totalOrderingObjectCache
// instance.
func newTotalOrderingObjectCache() *totalOrderingObjectCache {
	return &totalOrderingObjectCache{
		winRecordPool: sync.Pool{
			New: func() interface{} {
				return make(totalOrderingWinRecord)
			},
		},
	}
}

// requestAckedStatus requests a structure to record acking status of one
// candidate (or a global view of acking status of pending set).
func (cache *totalOrderingObjectCache) requestAckedStatus() (
	acked map[types.ValidatorID]*totalOrderingHeightRecord) {

	if len(cache.ackedStatus) == 0 {
		acked = make(map[types.ValidatorID]*totalOrderingHeightRecord)
	} else {
		acked, cache.ackedStatus =
			cache.ackedStatus[len(cache.ackedStatus)-1],
			cache.ackedStatus[:len(cache.ackedStatus)-1]
		for k := range acked {
			delete(acked, k)
		}
	}
	return
}

// recycleAckedStatys recycles the structure to record acking status.
func (cache *totalOrderingObjectCache) recycleAckedStatus(
	acked map[types.ValidatorID]*totalOrderingHeightRecord) {

	cache.ackedStatus = append(cache.ackedStatus, acked)
}

// requestWinRecord requests an totalOrderingWinRecord instance.
func (cache *totalOrderingObjectCache) requestWinRecord() (
	win totalOrderingWinRecord) {

	win = cache.winRecordPool.Get().(totalOrderingWinRecord)
	for k := range win {
		delete(win, k)
	}
	return
}

// recycleWinRecord recycles an totalOrderingWinRecord instance.
func (cache *totalOrderingObjectCache) recycleWinRecord(
	win totalOrderingWinRecord) {

	cache.winRecordPool.Put(win)
}

// requestHeightVector requests a structure to record acking heights
// of one candidate.
func (cache *totalOrderingObjectCache) requestHeightVector() (
	hv map[types.ValidatorID]uint64) {

	if len(cache.heightVectors) == 0 {
		hv = make(map[types.ValidatorID]uint64)
	} else {
		hv, cache.heightVectors =
			cache.heightVectors[len(cache.heightVectors)-1],
			cache.heightVectors[:len(cache.heightVectors)-1]
		for k := range hv {
			delete(hv, k)
		}
	}
	return
}

// recycleHeightVector recycles an instance to record acking heights
// of one candidate.
func (cache *totalOrderingObjectCache) recycleHeightVector(
	hv map[types.ValidatorID]uint64) {

	cache.heightVectors = append(cache.heightVectors, hv)
}

// requestWinRecordContainer requests a map of totalOrderingWinRecord.
func (cache *totalOrderingObjectCache) requestWinRecordContainer() (
	con map[common.Hash]totalOrderingWinRecord) {

	if len(cache.winRecordContainers) == 0 {
		con = make(map[common.Hash]totalOrderingWinRecord)
	} else {
		con, cache.winRecordContainers =
			cache.winRecordContainers[len(cache.winRecordContainers)-1],
			cache.winRecordContainers[:len(cache.winRecordContainers)-1]
		for k := range con {
			delete(con, k)
		}
	}
	return
}

// recycleWinRecordContainer recycles a map of totalOrderingWinRecord.
func (cache *totalOrderingObjectCache) recycleWinRecordContainer(
	con map[common.Hash]totalOrderingWinRecord) {

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
	ackedStatus        map[types.ValidatorID]*totalOrderingHeightRecord
	cachedHeightVector map[types.ValidatorID]uint64
	winRecords         map[common.Hash]totalOrderingWinRecord
}

// newTotalOrderingCandidateInfo constructs an totalOrderingCandidateInfo
// instance.
func newTotalOrderingCandidateInfo(
	objCache *totalOrderingObjectCache) *totalOrderingCandidateInfo {

	return &totalOrderingCandidateInfo{
		ackedStatus: objCache.requestAckedStatus(),
		winRecords:  objCache.requestWinRecordContainer(),
	}
}

// clean clear information related to another candidate, which should be called
// when that candidate is selected as deliver set.
func (v *totalOrderingCandidateInfo) clean(otherCandidate common.Hash) {
	delete(v.winRecords, otherCandidate)
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
func (v *totalOrderingCandidateInfo) addBlock(b *types.Block) (err error) {
	rec, exists := v.ackedStatus[b.ProposerID]
	if !exists {
		v.ackedStatus[b.ProposerID] = &totalOrderingHeightRecord{
			minHeight: b.Height,
			count:     1,
		}
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

	var (
		rec    *totalOrderingHeightRecord
		exists bool
	)

	for vID, gRec := range global.ackedStatus {
		rec, exists = v.ackedStatus[vID]
		if !exists {
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
	dirtyValidators types.ValidatorIDs,
	objCache *totalOrderingObjectCache) {

	var (
		vID       types.ValidatorID
		gRec, rec *totalOrderingHeightRecord
		exists    bool
	)

	// The reason not to merge the two loops is the iteration over map
	// is expensive when validator count is large, iterating over dirty
	// validators is cheaper.
	// TODO(mission): merge the code in this if/else if the performance won't be
	//                downgraded when adding a function for the shared part.
	if v.cachedHeightVector == nil {
		// Generate height vector from scratch.
		v.cachedHeightVector = objCache.requestHeightVector()
		for vID, gRec = range global.ackedStatus {
			if gRec.count <= k {
				delete(v.cachedHeightVector, vID)
				continue
			}
			rec, exists = v.ackedStatus[vID]
			if !exists {
				v.cachedHeightVector[vID] = infinity
			} else if rec.minHeight <= gRec.minHeight+k {
				// This check is sufficient to make sure the block height:
				//
				//   gRec.minHeight + k
				//
				// would be included in this totalOrderingCandidateInfo.
				v.cachedHeightVector[vID] = gRec.minHeight + k
			} else {
				v.cachedHeightVector[vID] = infinity
			}
		}
	} else {
		// Return the cached one, only update dirty fields.
		for _, vID = range dirtyValidators {
			gRec, exists = global.ackedStatus[vID]
			if !exists {
				continue
			}
			rec, exists = v.ackedStatus[vID]
			if gRec.count <= k {
				delete(v.cachedHeightVector, vID)
				continue
			} else if !exists {
				v.cachedHeightVector[vID] = infinity
			} else if rec.minHeight <= gRec.minHeight+k {
				v.cachedHeightVector[vID] = gRec.minHeight + k
			} else {
				v.cachedHeightVector[vID] = infinity
			}
		}
	}
	return
}

// updateWinRecord setup win records between two candidates.
func (v *totalOrderingCandidateInfo) updateWinRecord(
	otherCandidate common.Hash,
	other *totalOrderingCandidateInfo,
	dirtyValidators types.ValidatorIDs,
	objCache *totalOrderingObjectCache) {

	var (
		vID        types.ValidatorID
		hTo, hFrom uint64
		exists     bool
	)

	// The reason not to merge the two loops is the iteration over map
	// is expensive when validator count is large, iterating over dirty
	// validators is cheaper.
	// TODO(mission): merge the code in this if/else if add a function won't
	//                affect the performance.
	win, exists := v.winRecords[otherCandidate]
	if !exists {
		win = objCache.requestWinRecord()
		v.winRecords[otherCandidate] = win
		for vID, hFrom = range v.cachedHeightVector {
			hTo, exists = other.cachedHeightVector[vID]
			if !exists {
				continue
			}
			if hFrom != infinity && hTo == infinity {
				win[vID] = struct{}{}
			}
		}
	} else {
		for _, vID = range dirtyValidators {
			hFrom, exists = v.cachedHeightVector[vID]
			if !exists {
				delete(win, vID)
				return
			}
			hTo, exists = other.cachedHeightVector[vID]
			if !exists {
				delete(win, vID)
				return
			}
			if hFrom != infinity && hTo == infinity {
				win[vID] = struct{}{}
			} else {
				delete(win, vID)
			}
		}
	}
}

// totalOrderingGroupVector keeps global status of current pending set.
type totalOrderingGlobalVector struct {
	// blocks stores all blocks grouped by their proposers and
	// sorted by their block height.
	//
	// TODO: the way we use this slice would make it reallocate frequently.
	blocks map[types.ValidatorID][]*types.Block

	// cachedCandidateInfo is an totalOrderingCandidateInfo instance,
	// which is just used for actual candidates to calculate height vector.
	cachedCandidateInfo *totalOrderingCandidateInfo
}

func newTotalOrderingGlobalVector() *totalOrderingGlobalVector {
	return &totalOrderingGlobalVector{
		blocks: make(map[types.ValidatorID][]*types.Block),
	}
}

func (global *totalOrderingGlobalVector) addBlock(b *types.Block) (err error) {
	blocksFromProposer := global.blocks[b.ProposerID]
	if len(blocksFromProposer) > 0 {
		lastBlock := blocksFromProposer[len(blocksFromProposer)-1]
		if b.Height-lastBlock.Height != 1 {
			err = ErrNotValidDAG
			return
		}
	}
	global.blocks[b.ProposerID] = append(blocksFromProposer, b)
	return
}

// updateCandidateInfo udpate cached candidate info.
func (global *totalOrderingGlobalVector) updateCandidateInfo(
	dirtyValidators types.ValidatorIDs, objCache *totalOrderingObjectCache) {

	var (
		vID    types.ValidatorID
		blocks []*types.Block
		info   *totalOrderingCandidateInfo
	)

	if global.cachedCandidateInfo == nil {
		info = newTotalOrderingCandidateInfo(objCache)
		for vID, blocks = range global.blocks {
			if len(blocks) == 0 {
				continue
			}
			info.ackedStatus[vID] = &totalOrderingHeightRecord{
				minHeight: blocks[0].Height,
				count:     uint64(len(blocks)),
			}
		}
		global.cachedCandidateInfo = info
	} else {
		info = global.cachedCandidateInfo
		for _, vID = range dirtyValidators {
			blocks = global.blocks[vID]
			if len(blocks) == 0 {
				delete(info.ackedStatus, vID)
				continue
			}
			info.ackedStatus[vID] = &totalOrderingHeightRecord{
				minHeight: blocks[0].Height,
				count:     uint64(len(blocks)),
			}
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
	candidates map[common.Hash]*totalOrderingCandidateInfo

	// acked cache the 'block A acked by block B' relation by
	// keeping a record in acked[A.Hash][B.Hash]
	acked map[common.Hash]map[common.Hash]struct{}

	// dirtyValidators records which validatorID that should be updated for
	// all cached status (win record, acking status).
	dirtyValidators types.ValidatorIDs

	// objCache caches allocated objects, like map.
	objCache *totalOrderingObjectCache
}

func newTotalOrdering(k, phi, validatorCount uint64) *totalOrdering {
	return &totalOrdering{
		candidates:      make(map[common.Hash]*totalOrderingCandidateInfo),
		pendings:        make(map[common.Hash]*types.Block),
		k:               k,
		phi:             phi,
		validatorCount:  validatorCount,
		globalVector:    newTotalOrderingGlobalVector(),
		dirtyValidators: make(types.ValidatorIDs, 0, int(validatorCount)),
		acked:           make(map[common.Hash]map[common.Hash]struct{}),
		objCache:        newTotalOrderingObjectCache(),
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
	to.candidates[h].recycle(to.objCache)
	delete(to.candidates, h)
	for _, info := range to.candidates {
		info.clean(h)
	}
}

// updateVectors is a helper function to update all cached vectors.
func (to *totalOrdering) updateVectors(b *types.Block) (err error) {
	var (
		candidate common.Hash
		info      *totalOrderingCandidateInfo
		acked     bool
	)
	// Update global height vector
	err = to.globalVector.addBlock(b)
	if err != nil {
		return
	}

	// Update acking status of candidates.
	for candidate, info = range to.candidates {
		if _, acked = to.acked[candidate][b.Hash]; !acked {
			continue
		}
		if err = info.addBlock(b); err != nil {
			return
		}
	}
	return
}

// prepareCandidate is a helper function to
// build totalOrderingCandidateInfo for new candidate.
func (to *totalOrdering) prepareCandidate(
	candidate *types.Block) (info *totalOrderingCandidateInfo) {

	info = newTotalOrderingCandidateInfo(to.objCache)
	info.ackedStatus[candidate.ProposerID] = &totalOrderingHeightRecord{
		minHeight: candidate.Height,
		count:     uint64(len(to.globalVector.blocks[candidate.ProposerID])),
	}
	ackedsForCandidate, exists := to.acked[candidate.Hash]
	if !exists {
		// This candidate is acked by nobody.
		return
	}
	for vID, blocks := range to.globalVector.blocks {
		if vID == candidate.ProposerID {
			continue
		}
		for i, b := range blocks {
			if _, acked := ackedsForCandidate[b.Hash]; !acked {
				continue
			}
			// If this block acks this candidate, all newer blocks
			// from the same validator also 'indirect' acks it.
			info.ackedStatus[vID] = &totalOrderingHeightRecord{
				minHeight: b.Height,
				count:     uint64(len(blocks) - i),
			}
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
		to.globalVector.blocks[b.ProposerID] =
			to.globalVector.blocks[b.ProposerID][1:]
		ret = append(ret, b)

		// Remove block relations.
		to.clean(p)
		to.dirtyValidators = append(to.dirtyValidators, b.ProposerID)
	}
	sort.Sort(types.ByHash(ret))

	// Find new candidates from tip of globalVector of each validator.
	// The complexity here is O(N^2logN).
	for _, blocks := range to.globalVector.blocks {
		if len(blocks) == 0 {
			continue
		}
		tip := blocks[0]
		if _, alreadyCandidate :=
			to.candidates[tip.Hash]; alreadyCandidate {
			continue
		}
		if !to.isAckOnlyPrecedings(tip) {
			continue
		}
		// Build totalOrderingCandidateInfo for new candidate.
		to.candidates[tip.Hash] = to.prepareCandidate(tip)
	}
	return ret
}

// generateDeliverSet would:
//  - generate preceding set
//  - check if the preceding set deliverable by checking potential function
func (to *totalOrdering) generateDeliverSet() (
	delivered map[common.Hash]struct{}, early bool) {

	var (
		candidate, otherCandidate common.Hash
		info, otherInfo           *totalOrderingCandidateInfo
		precedings                = make(map[common.Hash]struct{})
	)

	to.globalVector.updateCandidateInfo(to.dirtyValidators, to.objCache)
	globalInfo := to.globalVector.cachedCandidateInfo
	for _, info = range to.candidates {
		info.updateAckingHeightVector(
			globalInfo, to.k, to.dirtyValidators, to.objCache)
	}

	// Update winning records for each candidate.
	var wg sync.WaitGroup
	wg.Add(len(to.candidates))
	for candidate, info := range to.candidates {
		go func(can common.Hash, canInfo *totalOrderingCandidateInfo) {
			for otherCandidate, otherInfo := range to.candidates {
				if can == otherCandidate {
					continue
				}
				canInfo.updateWinRecord(
					otherCandidate, otherInfo, to.dirtyValidators, to.objCache)
			}
			wg.Done()
		}(candidate, info)
	}
	wg.Wait()

	// Reset dirty validators.
	to.dirtyValidators = to.dirtyValidators[:0]

	globalAnsLength := globalInfo.getAckingNodeSetLength(globalInfo, to.k)
CheckNextCandidateLoop:
	for candidate = range to.candidates {
		for otherCandidate, otherInfo = range to.candidates {
			if candidate == otherCandidate {
				continue
			}
			if otherInfo.winRecords[candidate].grade(
				to.validatorCount, to.phi, globalAnsLength) != 0 {

				continue CheckNextCandidateLoop
			}
		}
		precedings[candidate] = struct{}{}
	}
	if len(precedings) == 0 {
		return
	}

	// internal is a helper function to verify internal stability.
	internal := func() bool {
		var (
			isPreceding, beaten bool
			p                   common.Hash
		)
		for candidate = range to.candidates {
			if _, isPreceding = precedings[candidate]; isPreceding {
				continue
			}
			beaten = false
			for p = range precedings {
				if beaten = to.candidates[p].winRecords[candidate].grade(
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
			height uint64
			p      common.Hash
			count  uint64
			status *totalOrderingCandidateInfo
		)
		for p = range precedings {
			count = 0
			status = to.candidates[p]
			for _, height = range status.cachedHeightVector {
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
	delivered = precedings
	return
}

// processBlock is the entry point of totalOrdering.
func (to *totalOrdering) processBlock(b *types.Block) (
	delivered []*types.Block, early bool, err error) {
	// NOTE: I assume the block 'b' is already safe for total ordering.
	//       That means, all its acking blocks are during/after
	//       total ordering stage.

	to.pendings[b.Hash] = b
	to.buildBlockRelation(b)
	if err = to.updateVectors(b); err != nil {
		return
	}
	if to.isAckOnlyPrecedings(b) {
		to.candidates[b.Hash] = to.prepareCandidate(b)
	}
	// Mark the proposer of incoming block as dirty.
	to.dirtyValidators = append(to.dirtyValidators, b.ProposerID)
	hashes, early := to.generateDeliverSet()

	// output precedings
	delivered = to.output(hashes)
	return
}
