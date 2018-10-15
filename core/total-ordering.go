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
	// ErrChainIDNotRecognized means the chain is unknown to this module.
	ErrChainIDNotRecognized = fmt.Errorf("chain ID not recognized")
)

// totalOrderingConfig is the configuration for total ordering.
type totalOrderingConfig struct {
	roundBasedConfig
	// k represents the k in 'k-level total ordering'.
	// In short, only block height equals to (global minimum height + k)
	// would be taken into consideration.
	k uint64
	// phi is a const to control how strong the leading preceding block
	// should be.
	phi uint64
	// chainNum is the count of chains.
	numChains uint32
	// Is round cutting required?
	isFlushRequired bool
}

func (config *totalOrderingConfig) fromConfig(
	roundID uint64, cfg *types.Config) {
	config.k = uint64(cfg.K)
	config.numChains = cfg.NumChains
	config.phi = uint64(float32(cfg.NumChains-1)*cfg.PhiRatio + 1)
	config.setupRoundBasedFields(roundID, cfg)
}

func newGenesisTotalOrderingConfig(
	dMoment time.Time, config *types.Config) *totalOrderingConfig {
	c := &totalOrderingConfig{}
	c.fromConfig(0, config)
	c.setRoundBeginTime(dMoment)
	return c
}

func newTotalOrderingConfig(
	prev *totalOrderingConfig, cur *types.Config) *totalOrderingConfig {
	c := &totalOrderingConfig{}
	c.fromConfig(prev.roundID+1, cur)
	c.setRoundBeginTime(prev.roundEndTime)
	prev.isFlushRequired = c.k != prev.k ||
		c.phi != prev.phi ||
		c.numChains != prev.numChains
	return c
}

// totalOrderingWinRecord caches which chains this candidate
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

func newTotalOrderingWinRecord(chainNum uint32) (
	rec *totalOrderingWinRecord) {
	rec = &totalOrderingWinRecord{}
	rec.reset()
	rec.wins = make([]int8, chainNum)
	return
}

// grade implements the 'grade' potential function described in white paper.
func (rec *totalOrderingWinRecord) grade(
	chainNum uint32, phi uint64, globalAnsLength uint64) int {
	if uint64(rec.count) >= phi {
		return 1
	} else if uint64(rec.count) < phi-uint64(chainNum)+globalAnsLength {
		return 0
	} else {
		return -1
	}
}

// totalOrderingHeightRecord records two things:
//  - the minimum heiht of block from that chain acking this block.
//  - the count of blocks from that chain acking this block.
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
	chainNum            uint32
}

// newTotalOrderingObjectCache constructs an totalOrderingObjectCache
// instance.
func newTotalOrderingObjectCache(chainNum uint32) *totalOrderingObjectCache {
	return &totalOrderingObjectCache{
		winRecordPool: sync.Pool{
			New: func() interface{} {
				return newTotalOrderingWinRecord(chainNum)
			},
		},
		chainNum: chainNum,
	}
}

// requestAckedStatus requests a structure to record acking status of one
// candidate (or a global view of acking status of pending set).
func (cache *totalOrderingObjectCache) requestAckedStatus() (
	acked []*totalOrderingHeightRecord) {
	if len(cache.ackedStatus) == 0 {
		acked = make([]*totalOrderingHeightRecord, cache.chainNum)
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
func (cache *totalOrderingObjectCache) requestHeightVector() (hv []uint64) {
	if len(cache.heightVectors) == 0 {
		hv = make([]uint64, cache.chainNum)
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
func (cache *totalOrderingObjectCache) recycleHeightVector(hv []uint64) {
	cache.heightVectors = append(cache.heightVectors, hv)
}

// requestWinRecordContainer requests a map of totalOrderingWinRecord.
func (cache *totalOrderingObjectCache) requestWinRecordContainer() (
	con []*totalOrderingWinRecord) {
	if len(cache.winRecordContainers) == 0 {
		con = make([]*totalOrderingWinRecord, cache.chainNum)
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
//    one chain acking this candidate.
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
func (v *totalOrderingCandidateInfo) clean(otherCandidateChainID uint32) {
	v.winRecords[otherCandidateChainID] = nil
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
	rec := v.ackedStatus[b.Position.ChainID]
	if rec.count == 0 {
		rec.minHeight = b.Position.Height
		rec.count = 1
	} else {
		if b.Position.Height < rec.minHeight {
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
//  For some chain X:
//   - the global minimum acking height = 1,
//   - k = 1
//  then only block height >= 2 would be added to acking node set.
func (v *totalOrderingCandidateInfo) getAckingNodeSetLength(
	global *totalOrderingCandidateInfo, k uint64) (count uint64) {
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
	dirtyChainIDs []int,
	objCache *totalOrderingObjectCache) {
	var (
		idx       int
		gRec, rec *totalOrderingHeightRecord
	)
	// The reason not to merge the two loops is the iteration over map
	// is expensive when chain count is large, iterating over dirty
	// chains is cheaper.
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
		for _, idx = range dirtyChainIDs {
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
	otherChainID uint32,
	other *totalOrderingCandidateInfo,
	dirtyChainIDs []int,
	objCache *totalOrderingObjectCache) {
	var (
		idx    int
		height uint64
	)
	// The reason not to merge the two loops is the iteration over map
	// is expensive when chain count is large, iterating over dirty
	// chains is cheaper.
	// TODO(mission): merge the code in this if/else if add a function won't
	//                affect the performance.
	win := v.winRecords[otherChainID]
	if win == nil {
		win = objCache.requestWinRecord()
		v.winRecords[otherChainID] = win
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
		for _, idx = range dirtyChainIDs {
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
	chainNum uint32) *totalOrderingGlobalVector {
	return &totalOrderingGlobalVector{
		blocks: make([][]*types.Block, chainNum),
	}
}

func (global *totalOrderingGlobalVector) addBlock(b *types.Block) (err error) {
	blocksFromChain := global.blocks[b.Position.ChainID]
	if len(blocksFromChain) > 0 {
		lastBlock := blocksFromChain[len(blocksFromChain)-1]
		if b.Position.Height-lastBlock.Position.Height != 1 {
			err = ErrNotValidDAG
			return
		}
	}
	global.blocks[b.Position.ChainID] = append(blocksFromChain, b)
	return
}

// updateCandidateInfo udpate cached candidate info.
func (global *totalOrderingGlobalVector) updateCandidateInfo(
	dirtyChainIDs []int, objCache *totalOrderingObjectCache) {
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
			rec.minHeight = blocks[0].Position.Height
			rec.count = uint64(len(blocks))
		}
		global.cachedCandidateInfo = info
	} else {
		info = global.cachedCandidateInfo
		for _, idx = range dirtyChainIDs {
			blocks = global.blocks[idx]
			if len(blocks) == 0 {
				info.ackedStatus[idx].count = 0
				continue
			}
			rec = info.ackedStatus[idx]
			rec.minHeight = blocks[0].Position.Height
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

	// The round of config used when performing total ordering.
	curRound uint64

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

	// dirtyChainIDs records which chainID that should be updated
	// for all cached status (win record, acking status).
	dirtyChainIDs []int

	// objCache caches allocated objects, like map.
	objCache *totalOrderingObjectCache

	// candidateChainMapping keeps a mapping from candidate's hash to
	// their chain IDs.
	candidateChainMapping map[common.Hash]uint32

	// candidateChainIDs records chain ID of all candidates.
	candidateChainIDs []uint32

	// configs keeps configuration for each round in continuous way.
	configs []*totalOrderingConfig
}

// newTotalOrdering constructs an totalOrdering instance.
func newTotalOrdering(genesisConfig *totalOrderingConfig) *totalOrdering {
	globalVector := newTotalOrderingGlobalVector(genesisConfig.numChains)
	objCache := newTotalOrderingObjectCache(genesisConfig.numChains)
	candidates := make([]*totalOrderingCandidateInfo, genesisConfig.numChains)
	to := &totalOrdering{
		pendings:              make(map[common.Hash]*types.Block),
		globalVector:          globalVector,
		dirtyChainIDs:         make([]int, 0, genesisConfig.numChains),
		acked:                 make(map[common.Hash]map[common.Hash]struct{}),
		objCache:              objCache,
		candidateChainMapping: make(map[common.Hash]uint32),
		candidates:            candidates,
		candidateChainIDs:     make([]uint32, 0, genesisConfig.numChains),
	}
	to.configs = []*totalOrderingConfig{genesisConfig}
	return to
}

// appendConfig add new configs for upcoming rounds. If you add a config for
// round R, next time you can only add the config for round R+1.
func (to *totalOrdering) appendConfig(
	round uint64, config *types.Config) error {
	if round != uint64(len(to.configs)) {
		return ErrRoundNotIncreasing
	}
	to.configs = append(
		to.configs,
		newTotalOrderingConfig(to.configs[len(to.configs)-1], config))
	return nil
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
		for _, ack = range curBlock.Acks {
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

// clean a block from working set. This behaviour would prevent
// our memory usage growing infinity.
func (to *totalOrdering) clean(b *types.Block) {
	var (
		h       = b.Hash
		chainID = b.Position.ChainID
	)
	to.objCache.recycleAckedVector(to.acked[h])
	delete(to.acked, h)
	delete(to.pendings, h)
	to.candidates[chainID].recycle(to.objCache)
	to.candidates[chainID] = nil
	delete(to.candidateChainMapping, h)
	// Remove this candidate from candidate IDs.
	to.candidateChainIDs =
		removeFromSortedUint32Slice(to.candidateChainIDs, chainID)
	// Clear records of this candidate from other candidates.
	for _, idx := range to.candidateChainIDs {
		to.candidates[idx].clean(chainID)
	}
}

// updateVectors is a helper function to update all cached vectors.
func (to *totalOrdering) updateVectors(b *types.Block) (err error) {
	var (
		candidateHash common.Hash
		chainID       uint32
		acked         bool
	)
	// Update global height vector
	if err = to.globalVector.addBlock(b); err != nil {
		return
	}
	// Update acking status of candidates.
	for candidateHash, chainID = range to.candidateChainMapping {
		if _, acked = to.acked[candidateHash][b.Hash]; !acked {
			continue
		}
		if err = to.candidates[chainID].addBlock(b); err != nil {
			return
		}
	}
	return
}

// prepareCandidate is a helper function to
// build totalOrderingCandidateInfo for new candidate.
func (to *totalOrdering) prepareCandidate(candidate *types.Block) {
	var (
		info = newTotalOrderingCandidateInfo(
			candidate.Hash, to.objCache)
		chainID = candidate.Position.ChainID
	)
	to.candidates[chainID] = info
	to.candidateChainMapping[candidate.Hash] = chainID
	// Add index to slot to allocated list, make sure the modified list sorted.
	to.candidateChainIDs = append(to.candidateChainIDs, chainID)
	sort.Slice(to.candidateChainIDs, func(i, j int) bool {
		return to.candidateChainIDs[i] < to.candidateChainIDs[j]
	})
	info.ackedStatus[chainID] = &totalOrderingHeightRecord{
		minHeight: candidate.Position.Height,
		count:     uint64(len(to.globalVector.blocks[chainID])),
	}
	ackedsForCandidate, exists := to.acked[candidate.Hash]
	if !exists {
		// This candidate is acked by nobody.
		return
	}
	var rec *totalOrderingHeightRecord
	for idx, blocks := range to.globalVector.blocks {
		if idx == int(chainID) {
			continue
		}
		for i, b := range blocks {
			if _, acked := ackedsForCandidate[b.Hash]; !acked {
				continue
			}
			// If this block acks this candidate, all newer blocks
			// from the same chain also 'indirect' acks it.
			rec = info.ackedStatus[idx]
			rec.minHeight = b.Position.Height
			rec.count = uint64(len(blocks) - i)
			break
		}
	}
	return
}

// isAckOnlyPrecedings is a helper function to check if a block
// only contain acks to delivered blocks.
func (to *totalOrdering) isAckOnlyPrecedings(b *types.Block) bool {
	for _, ack := range b.Acks {
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
		chainID := b.Position.ChainID
		// TODO(mission): This way to use slice makes it reallocate frequently.
		to.globalVector.blocks[int(chainID)] =
			to.globalVector.blocks[int(chainID)][1:]
		ret = append(ret, b)
		// Remove block relations.
		to.clean(b)
		to.dirtyChainIDs = append(to.dirtyChainIDs, int(chainID))
	}
	sort.Sort(types.ByHash(ret))
	// Find new candidates from tip of globalVector of each chain.
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
			to.candidateChainMapping[tip.Hash]; alreadyCandidate {
			continue
		}
		if !to.isAckOnlyPrecedings(tip) {
			continue
		}
		// Build totalOrderingCandidateInfo for new candidate.
		to.prepareCandidate(tip)
	}
	return ret
}

// generateDeliverSet would:
//  - generate preceding set
//  - check if the preceding set deliverable by checking potential function
func (to *totalOrdering) generateDeliverSet() (
	delivered map[common.Hash]struct{}, early bool) {
	var (
		chainID, otherChainID uint32
		info, otherInfo       *totalOrderingCandidateInfo
		precedings            = make(map[uint32]struct{})
		cfg                   = to.configs[to.curRound]
	)
	to.globalVector.updateCandidateInfo(to.dirtyChainIDs, to.objCache)
	globalInfo := to.globalVector.cachedCandidateInfo
	for _, chainID = range to.candidateChainIDs {
		to.candidates[chainID].updateAckingHeightVector(
			globalInfo, cfg.k, to.dirtyChainIDs, to.objCache)
	}
	// Update winning records for each candidate.
	// TODO(mission): It's not reasonable to
	//                request one routine for each candidate, the context
	//                switch rate would be high.
	var wg sync.WaitGroup
	wg.Add(len(to.candidateChainIDs))
	for _, chainID := range to.candidateChainIDs {
		info = to.candidates[chainID]
		go func(can uint32, canInfo *totalOrderingCandidateInfo) {
			for _, otherChainID := range to.candidateChainIDs {
				if can == otherChainID {
					continue
				}
				canInfo.updateWinRecord(
					otherChainID,
					to.candidates[otherChainID],
					to.dirtyChainIDs,
					to.objCache)
			}
			wg.Done()
		}(chainID, info)
	}
	wg.Wait()
	// Reset dirty chains.
	to.dirtyChainIDs = to.dirtyChainIDs[:0]
	// TODO(mission): ANS should be bound by current numChains.
	globalAnsLength := globalInfo.getAckingNodeSetLength(globalInfo, cfg.k)
CheckNextCandidateLoop:
	for _, chainID = range to.candidateChainIDs {
		info = to.candidates[chainID]
		for _, otherChainID = range to.candidateChainIDs {
			if chainID == otherChainID {
				continue
			}
			otherInfo = to.candidates[otherChainID]
			// TODO(mission): grade should be bound by current numChains.
			if otherInfo.winRecords[chainID].grade(
				cfg.numChains, cfg.phi, globalAnsLength) != 0 {
				continue CheckNextCandidateLoop
			}
		}
		precedings[chainID] = struct{}{}
	}
	if len(precedings) == 0 {
		return
	}
	// internal is a helper function to verify internal stability.
	internal := func() bool {
		var (
			isPreceding, beaten bool
			p                   uint32
		)
		for _, chainID = range to.candidateChainIDs {
			if _, isPreceding = precedings[chainID]; isPreceding {
				continue
			}
			beaten = false
			for p = range precedings {
				// TODO(mission): grade should be bound by current numChains.
				if beaten = to.candidates[p].winRecords[chainID].grade(
					cfg.numChains, cfg.phi, globalAnsLength) == 1; beaten {
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
			p             uint32
		)
		for p = range precedings {
			count = 0
			info = to.candidates[p]
			for _, height = range info.cachedHeightVector {
				if height != infinity {
					count++
					if count > cfg.phi {
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
		var chainAnsLength uint64
		for p := range precedings {
			// TODO(mission): ANS should be bound by current numChains.
			chainAnsLength = to.candidates[p].getAckingNodeSetLength(
				globalInfo, cfg.k)
			if uint64(chainAnsLength) < uint64(cfg.numChains)-cfg.phi {
				return false
			}
		}
		return true
	}
	// If all chains propose enough blocks, we should force
	// to deliver since the whole picture of the DAG is revealed.
	if globalAnsLength != uint64(cfg.numChains) {
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
	cfg := to.configs[to.curRound]
	if b.Position.ChainID >= cfg.numChains {
		err = ErrChainIDNotRecognized
		return
	}
	to.pendings[b.Hash] = b
	to.buildBlockRelation(b)
	if err = to.updateVectors(b); err != nil {
		return
	}
	if to.isAckOnlyPrecedings(b) {
		to.prepareCandidate(b)
	}
	// Mark the proposer of incoming block as dirty.
	to.dirtyChainIDs = append(to.dirtyChainIDs, int(b.Position.ChainID))
	hashes, early := to.generateDeliverSet()

	// output precedings
	delivered = to.output(hashes)
	return
}
