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
	"errors"
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

const (
	// TotalOrderingModeError returns mode error.
	TotalOrderingModeError uint32 = iota
	// TotalOrderingModeNormal returns mode normal.
	TotalOrderingModeNormal
	// TotalOrderingModeEarly returns mode early.
	TotalOrderingModeEarly
	// TotalOrderingModeFlush returns mode flush.
	TotalOrderingModeFlush
)

var (
	// ErrNotValidDAG would be reported when block subbmitted to totalOrdering
	// didn't form a DAG.
	ErrNotValidDAG = errors.New("not a valid dag")
	// ErrFutureRoundDelivered means some blocks from later rounds are
	// delivered, this means program error.
	ErrFutureRoundDelivered = errors.New("future round delivered")
	// ErrBlockFromPastRound means we receive some block from past round.
	ErrBlockFromPastRound = errors.New("block from past round")
	// ErrTotalOrderingHangs means total ordering hangs somewhere.
	ErrTotalOrderingHangs = errors.New("total ordering hangs")
	// ErrForwardAck means a block acking some blocks from newer round.
	ErrForwardAck = errors.New("forward ack")
	// ErrUnexpected means general (I'm lazy) errors.
	ErrUnexpected = errors.New("unexpected")
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

func newTotalOrderingWinRecord(numChains uint32) (
	rec *totalOrderingWinRecord) {
	rec = &totalOrderingWinRecord{}
	rec.reset()
	rec.wins = make([]int8, numChains)
	return
}

// grade implements the 'grade' potential function described in white paper.
func (rec *totalOrderingWinRecord) grade(
	numChains uint32, phi uint64, globalAnsLength uint64) int {
	if uint64(rec.count) >= phi {
		return 1
	} else if uint64(rec.count) < phi-uint64(numChains)+globalAnsLength {
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
	numChains           uint32
}

// newTotalOrderingObjectCache constructs an totalOrderingObjectCache
// instance.
func newTotalOrderingObjectCache(numChains uint32) *totalOrderingObjectCache {
	return &totalOrderingObjectCache{
		winRecordPool: sync.Pool{
			New: func() interface{} {
				return newTotalOrderingWinRecord(numChains)
			},
		},
		numChains: numChains,
	}
}

// resize makes sure internal storage of totalOrdering instance can handle
// maximum possible numChains in future configs.
func (cache *totalOrderingObjectCache) resize(numChains uint32) {
	// Basically, everything in cache needs to be cleaned.
	if cache.numChains >= numChains {
		return
	}
	cache.ackedStatus = nil
	cache.heightVectors = nil
	cache.winRecordContainers = nil
	cache.ackedVectors = nil
	cache.numChains = numChains
	cache.winRecordPool = sync.Pool{
		New: func() interface{} {
			return newTotalOrderingWinRecord(numChains)
		},
	}
}

// requestAckedStatus requests a structure to record acking status of one
// candidate (or a global view of acking status of pending set).
func (cache *totalOrderingObjectCache) requestAckedStatus() (
	acked []*totalOrderingHeightRecord) {
	if len(cache.ackedStatus) == 0 {
		acked = make([]*totalOrderingHeightRecord, cache.numChains)
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
	// If the recycled objects supports lower numChains than we required,
	// don't recycle it.
	if uint32(len(acked)) != cache.numChains {
		return
	}
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
	// If the recycled objects supports lower numChains than we required,
	// don't recycle it.
	if uint32(len(win.wins)) != cache.numChains {
		return
	}
	cache.winRecordPool.Put(win)
}

// requestHeightVector requests a structure to record acking heights
// of one candidate.
func (cache *totalOrderingObjectCache) requestHeightVector() (hv []uint64) {
	if len(cache.heightVectors) == 0 {
		hv = make([]uint64, cache.numChains)
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
	// If the recycled objects supports lower numChains than we required,
	// don't recycle it.
	if uint32(len(hv)) != cache.numChains {
		return
	}
	cache.heightVectors = append(cache.heightVectors, hv)
}

// requestWinRecordContainer requests a map of totalOrderingWinRecord.
func (cache *totalOrderingObjectCache) requestWinRecordContainer() (
	con []*totalOrderingWinRecord) {
	if len(cache.winRecordContainers) == 0 {
		con = make([]*totalOrderingWinRecord, cache.numChains)
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
	// If the recycled objects supports lower numChains than we required,
	// don't recycle it.
	if uint32(len(con)) != cache.numChains {
		return
	}
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
	global *totalOrderingCandidateInfo,
	k uint64,
	numChains uint32) (count uint64) {
	var rec *totalOrderingHeightRecord
	for idx, gRec := range global.ackedStatus[:numChains] {
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
	objCache *totalOrderingObjectCache,
	numChains uint32) {
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
		for idx, height = range v.cachedHeightVector[:numChains] {
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

// totalOrderingBreakpoint is a record to store the height discontinuity
// on a chain.
type totalOrderingBreakpoint struct {
	roundID uint64
	// height of last block in previous round.
	lastHeight uint64
}

// totalOrderingGroupVector keeps global status of current pending set.
type totalOrderingGlobalVector struct {
	// blocks stores all blocks grouped by their proposers and
	// sorted by their block height.
	//
	// TODO(mission): the way we use this slice would make it reallocate
	//                frequently.
	blocks [][]*types.Block

	// breakpoints caches rounds for chains that blocks' height on them are
	// not continuous. Ex.
	//   ChainID   Round   Height
	//         1       0        0
	//         1       0        1
	//         1       1        2
	//         1       1        3
	//         1       1        4
	//         1       3        0   <- a breakpoint for round 3 would be cached
	//                                 for chain 1 as (roundID=1, lastHeight=4).
	breakpoints [][]*totalOrderingBreakpoint

	// curRound caches the last round ID used to purge breakpoints.
	curRound uint64

	// tips records the last seen block for each chain.
	tips []*types.Block

	// cachedCandidateInfo is an totalOrderingCandidateInfo instance,
	// which is just used for actual candidates to calculate height vector.
	cachedCandidateInfo *totalOrderingCandidateInfo
}

func newTotalOrderingGlobalVector(numChains uint32) *totalOrderingGlobalVector {
	return &totalOrderingGlobalVector{
		blocks:      make([][]*types.Block, numChains),
		tips:        make([]*types.Block, numChains),
		breakpoints: make([][]*totalOrderingBreakpoint, numChains),
	}
}

func (global *totalOrderingGlobalVector) resize(numChains uint32) {
	if len(global.blocks) >= int(numChains) {
		return
	}
	// Resize blocks.
	newBlocks := make([][]*types.Block, numChains)
	copy(newBlocks, global.blocks)
	global.blocks = newBlocks
	// Resize breakpoints.
	newBreakPoints := make([][]*totalOrderingBreakpoint, numChains)
	copy(newBreakPoints, global.breakpoints)
	global.breakpoints = newBreakPoints
	// Resize tips.
	newTips := make([]*types.Block, numChains)
	copy(newTips, global.tips)
	global.tips = newTips
}

func (global *totalOrderingGlobalVector) switchRound(roundID uint64) {
	if global.curRound+1 != roundID {
		panic(ErrUnexpected)
	}
	global.curRound = roundID
	for chainID, bs := range global.breakpoints {
		if len(bs) == 0 {
			continue
		}
		if bs[0].roundID == roundID {
			global.breakpoints[chainID] = bs[1:]
		}
	}
}

func (global *totalOrderingGlobalVector) prepareHeightRecord(
	candidate *types.Block,
	info *totalOrderingCandidateInfo,
	acked map[common.Hash]struct{}) {
	var (
		chainID     = candidate.Position.ChainID
		breakpoints = global.breakpoints[chainID]
		breakpoint  *totalOrderingBreakpoint
		rec         *totalOrderingHeightRecord
	)
	// Setup height record for own chain.
	rec = &totalOrderingHeightRecord{
		minHeight: candidate.Position.Height,
	}
	if len(breakpoints) == 0 {
		rec.count = uint64(len(global.blocks[chainID]))
	} else {
		rec.count = breakpoints[0].lastHeight - candidate.Position.Height + 1
	}
	info.ackedStatus[chainID] = rec
	if acked == nil {
		return
	}
	for idx, blocks := range global.blocks {
		if idx == int(candidate.Position.ChainID) {
			continue
		}
		breakpoint = nil
		if len(global.breakpoints[idx]) > 0 {
			breakpoint = global.breakpoints[idx][0]
		}
		for i, b := range blocks {
			if breakpoint != nil && b.Position.Round >= breakpoint.roundID {
				break
			}
			if _, acked := acked[b.Hash]; !acked {
				continue
			}
			// If this block acks this candidate, all newer blocks
			// from the same chain also 'indirect' acks it.
			rec = info.ackedStatus[idx]
			rec.minHeight = b.Position.Height
			if breakpoint == nil {
				rec.count = uint64(len(blocks) - i)
			} else {
				rec.count = breakpoint.lastHeight - b.Position.Height + 1
			}
			break
		}
	}

}

func (global *totalOrderingGlobalVector) addBlock(
	b *types.Block) (pos int, pending bool, err error) {
	curPosition := b.Position
	tip := global.tips[curPosition.ChainID]
	pos = len(global.blocks[curPosition.ChainID])
	if tip != nil {
		// Perform light weight sanity check based on tip.
		lastPosition := tip.Position
		if lastPosition.Round > curPosition.Round {
			err = ErrNotValidDAG
			return
		}
		if DiffUint64(lastPosition.Round, curPosition.Round) > 1 {
			if curPosition.Height != 0 {
				err = ErrNotValidDAG
				return
			}
			// Add breakpoint.
			global.breakpoints[curPosition.ChainID] = append(
				global.breakpoints[curPosition.ChainID],
				&totalOrderingBreakpoint{
					roundID:    curPosition.Round,
					lastHeight: lastPosition.Height,
				})
		} else {
			if curPosition.Height != lastPosition.Height+1 {
				err = ErrNotValidDAG
				return
			}
		}
	} else {
		if curPosition.Round < global.curRound {
			err = ErrBlockFromPastRound
			return
		}
		if curPosition.Round > global.curRound {
			// Add breakpoint.
			global.breakpoints[curPosition.ChainID] = append(
				global.breakpoints[curPosition.ChainID],
				&totalOrderingBreakpoint{
					roundID:    curPosition.Round,
					lastHeight: 0,
				})
		}
	}
	breakpoints := global.breakpoints[b.Position.ChainID]
	pending = len(breakpoints) > 0 && breakpoints[0].roundID <= b.Position.Round
	global.blocks[b.Position.ChainID] = append(
		global.blocks[b.Position.ChainID], b)
	global.tips[b.Position.ChainID] = b
	return
}

// updateCandidateInfo udpate cached candidate info.
func (global *totalOrderingGlobalVector) updateCandidateInfo(
	dirtyChainIDs []int, objCache *totalOrderingObjectCache) {
	var (
		idx        int
		blocks     []*types.Block
		block      *types.Block
		info       *totalOrderingCandidateInfo
		rec        *totalOrderingHeightRecord
		breakpoint *totalOrderingBreakpoint
	)
	if global.cachedCandidateInfo == nil {
		info = newTotalOrderingCandidateInfo(common.Hash{}, objCache)
		for idx, blocks = range global.blocks {
			if len(blocks) == 0 {
				continue
			}
			rec = info.ackedStatus[idx]
			if len(global.breakpoints[idx]) > 0 {
				breakpoint = global.breakpoints[idx][0]
				block = blocks[0]
				if block.Position.Round >= breakpoint.roundID {
					continue
				}
				rec.minHeight = block.Position.Height
				rec.count = breakpoint.lastHeight - block.Position.Height + 1
			} else {
				rec.minHeight = blocks[0].Position.Height
				rec.count = uint64(len(blocks))
			}
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
			if len(global.breakpoints[idx]) > 0 {
				breakpoint = global.breakpoints[idx][0]
				block = blocks[0]
				if block.Position.Round >= breakpoint.roundID {
					continue
				}
				rec.minHeight = block.Position.Height
				rec.count = breakpoint.lastHeight - block.Position.Height + 1
			} else {
				rec.minHeight = blocks[0].Position.Height
				rec.count = uint64(len(blocks))
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

	// The round of config used when performing total ordering.
	curRound uint64

	// duringFlush is a flag to switch the flush mode and normal mode.
	duringFlush bool

	// flushReadyChains checks if the last block of that chain arrived. Once
	// last blocks from all chains in current config are arrived, we can
	// perform flush.
	flushReadyChains map[uint32]struct{}

	// flush is a map to record which blocks are already flushed.
	flushed map[uint32]struct{}

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
	candidateChainMapping map[uint32]common.Hash

	// candidateChainIDs records chain ID of all candidates.
	candidateChainIDs []uint32

	// configs keeps configuration for each round in continuous way.
	configs []*totalOrderingConfig
}

// newTotalOrdering constructs an totalOrdering instance.
func newTotalOrdering(config *totalOrderingConfig) *totalOrdering {
	globalVector := newTotalOrderingGlobalVector(config.numChains)
	objCache := newTotalOrderingObjectCache(config.numChains)
	candidates := make([]*totalOrderingCandidateInfo, config.numChains)
	to := &totalOrdering{
		pendings:              make(map[common.Hash]*types.Block),
		globalVector:          globalVector,
		dirtyChainIDs:         make([]int, 0, config.numChains),
		acked:                 make(map[common.Hash]map[common.Hash]struct{}),
		objCache:              objCache,
		candidateChainMapping: make(map[uint32]common.Hash),
		candidates:            candidates,
		candidateChainIDs:     make([]uint32, 0, config.numChains),
		curRound:              config.roundID,
	}
	to.configs = []*totalOrderingConfig{config}
	return to
}

// appendConfig add new configs for upcoming rounds. If you add a config for
// round R, next time you can only add the config for round R+1.
func (to *totalOrdering) appendConfig(
	round uint64, config *types.Config) error {
	if round != uint64(len(to.configs))+to.configs[0].roundID {
		return ErrRoundNotIncreasing
	}
	to.configs = append(
		to.configs,
		newTotalOrderingConfig(to.configs[len(to.configs)-1], config))
	// Resize internal structures.
	to.globalVector.resize(config.NumChains)
	to.objCache.resize(config.NumChains)
	if int(config.NumChains) > len(to.candidates) {
		newCandidates := make([]*totalOrderingCandidateInfo, config.NumChains)
		copy(newCandidates, to.candidates)
		to.candidates = newCandidates
	}
	return nil
}

func (to *totalOrdering) switchRound() {
	to.curRound++
	to.globalVector.switchRound(to.curRound)
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
		if curBlock.Position.Round > b.Position.Round {
			// It's illegal for a block to acking some block from future
			// round, this rule should be promised before delivering to
			// total ordering.
			panic(ErrForwardAck)
		}
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
	delete(to.candidateChainMapping, chainID)
	// Remove this candidate from candidate IDs.
	to.candidateChainIDs =
		removeFromSortedUint32Slice(to.candidateChainIDs, chainID)
	// Clear records of this candidate from other candidates.
	for _, idx := range to.candidateChainIDs {
		to.candidates[idx].clean(chainID)
	}
}

// updateVectors is a helper function to update all cached vectors.
func (to *totalOrdering) updateVectors(b *types.Block) (pos int, err error) {
	var (
		candidateHash common.Hash
		chainID       uint32
		acked         bool
		pending       bool
	)
	// Update global height vector
	if pos, pending, err = to.globalVector.addBlock(b); err != nil {
		return
	}
	if to.duringFlush {
		// It makes no sense to calculate potential functions of total ordering
		// when flushing would be happened.
		return
	}
	if pending {
		// The chain of this block contains breakpoints, which means their
		// height are not continuous. This implementation of DEXON total
		// ordering algorithm assumes the height of blocks in working set should
		// be continuous.
		//
		// To workaround this issue, when block arrived after breakpoints,
		// their information would not be contributed to current working set.
		// This mechanism works because we switch rounds by flushing and
		// reset the whole working set.
		return
	}
	// Update acking status of candidates.
	for chainID, candidateHash = range to.candidateChainMapping {
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
		info    = newTotalOrderingCandidateInfo(candidate.Hash, to.objCache)
		chainID = candidate.Position.ChainID
	)
	to.candidates[chainID] = info
	to.candidateChainMapping[chainID] = candidate.Hash
	// Add index to slot to allocated list, make sure the modified list sorted.
	to.candidateChainIDs = append(to.candidateChainIDs, chainID)
	sort.Slice(to.candidateChainIDs, func(i, j int) bool {
		return to.candidateChainIDs[i] < to.candidateChainIDs[j]
	})
	to.globalVector.prepareHeightRecord(
		candidate, info, to.acked[candidate.Hash])
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
func (to *totalOrdering) output(
	precedings map[common.Hash]struct{},
	numChains uint32) (ret []*types.Block) {
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
	for chainID, blocks := range to.globalVector.blocks[:numChains] {
		if len(blocks) == 0 {
			continue
		}
		if _, picked := to.candidateChainMapping[uint32(chainID)]; picked {
			continue
		}
		if !to.isAckOnlyPrecedings(blocks[0]) {
			continue
		}
		// Build totalOrderingCandidateInfo for new candidate.
		to.prepareCandidate(blocks[0])
	}
	return ret
}

// generateDeliverSet would:
//  - generate preceding set
//  - check if the preceding set deliverable by checking potential function
func (to *totalOrdering) generateDeliverSet() (
	delivered map[common.Hash]struct{}, mode uint32) {
	var (
		chainID, otherChainID uint32
		info, otherInfo       *totalOrderingCandidateInfo
		precedings            = make(map[uint32]struct{})
		cfg                   = to.configs[to.curRound-to.configs[0].roundID]
	)
	mode = TotalOrderingModeNormal
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
					to.objCache,
					cfg.numChains)
			}
			wg.Done()
		}(chainID, info)
	}
	wg.Wait()
	// Reset dirty chains.
	to.dirtyChainIDs = to.dirtyChainIDs[:0]
	// TODO(mission): ANS should be bound by current numChains.
	globalAnsLength := globalInfo.getAckingNodeSetLength(
		globalInfo, cfg.k, cfg.numChains)
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
				globalInfo, cfg.k, cfg.numChains)
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
			mode = TotalOrderingModeEarly
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

// flushBlocks flushes blocks.
func (to *totalOrdering) flushBlocks(
	b *types.Block) (flushed []*types.Block, mode uint32, err error) {
	cfg := to.configs[to.curRound-to.configs[0].roundID]
	mode = TotalOrderingModeFlush
	if cfg.isValidLastBlock(b) {
		to.flushReadyChains[b.Position.ChainID] = struct{}{}
	}
	// Flush blocks until last blocks from all chains are arrived.
	if len(to.flushReadyChains) < int(cfg.numChains) {
		return
	}
	if len(to.flushReadyChains) > int(cfg.numChains) {
		// This line should never be reached.
		err = ErrFutureRoundDelivered
		return
	}
	// Dump all blocks in this round.
	for {
		if len(to.flushed) == int(cfg.numChains) {
			break
		}
		// Dump all candidates without checking potential function.
		flushedHashes := make(map[common.Hash]struct{})
		for _, chainID := range to.candidateChainIDs {
			candidateBlock := to.pendings[to.candidates[chainID].hash]
			if candidateBlock.Position.Round > to.curRound {
				continue
			}
			flushedHashes[candidateBlock.Hash] = struct{}{}
		}
		if len(flushedHashes) == 0 {
			err = ErrTotalOrderingHangs
			return
		}
		flushedBlocks := to.output(flushedHashes, cfg.numChains)
		for _, b := range flushedBlocks {
			if !cfg.isValidLastBlock(b) {
				continue
			}
			to.flushed[b.Position.ChainID] = struct{}{}
		}
		flushed = append(flushed, flushedBlocks...)
	}
	// Switch back to normal mode: delivered by DEXON total ordering algorithm.
	to.duringFlush = false
	to.flushed = make(map[uint32]struct{})
	to.flushReadyChains = make(map[uint32]struct{})
	// Clean all cached intermediate stats.
	for idx := range to.candidates {
		if to.candidates[idx] == nil {
			continue
		}
		to.candidates[idx].recycle(to.objCache)
		to.candidates[idx] = nil
	}
	to.dirtyChainIDs = nil
	to.candidateChainMapping = make(map[uint32]common.Hash)
	to.candidateChainIDs = nil
	to.globalVector.cachedCandidateInfo = nil
	to.switchRound()
	// Force to pick new candidates.
	numChains := to.configs[to.curRound-to.configs[0].roundID].numChains
	to.output(map[common.Hash]struct{}{}, numChains)
	return
}

// deliverBlocks delivers blocks by DEXON total ordering algorithm.
func (to *totalOrdering) deliverBlocks() (
	delivered []*types.Block, mode uint32, err error) {
	hashes, mode := to.generateDeliverSet()
	cfg := to.configs[to.curRound-to.configs[0].roundID]
	// output precedings
	delivered = to.output(hashes, cfg.numChains)
	// Check if any block in delivered set are the last block in this round
	// of that chain. If yes, flush or round-switching would be performed.
	for _, b := range delivered {
		if b.Position.Round > to.curRound {
			err = ErrFutureRoundDelivered
			return
		}
		if !cfg.isValidLastBlock(b) {
			continue
		}
		if cfg.isFlushRequired {
			// Switch to flush mode.
			to.duringFlush = true
			to.flushReadyChains = make(map[uint32]struct{})
			to.flushed = make(map[uint32]struct{})
		} else {
			// Switch round directly.
			to.switchRound()
		}
		break
	}
	if to.duringFlush {
		// Make sure last blocks from all chains are marked as 'flushed'.
		for _, b := range delivered {
			if !cfg.isValidLastBlock(b) {
				continue
			}
			to.flushed[b.Position.ChainID] = struct{}{}
		}
		// Some last blocks for the round to be flushed might not be delivered
		// yet.
		for _, tip := range to.globalVector.tips[:cfg.numChains] {
			if tip.Position.Round > to.curRound || cfg.isValidLastBlock(tip) {
				to.flushReadyChains[tip.Position.ChainID] = struct{}{}
			}
		}
	}
	return
}

// processBlock is the entry point of totalOrdering.
func (to *totalOrdering) processBlock(
	b *types.Block) ([]*types.Block, uint32, error) {
	// NOTE: I assume the block 'b' is already safe for total ordering.
	//       That means, all its acking blocks are during/after
	//       total ordering stage.
	cfg := to.configs[to.curRound-to.configs[0].roundID]
	to.pendings[b.Hash] = b
	to.buildBlockRelation(b)
	pos, err := to.updateVectors(b)
	if err != nil {
		return nil, uint32(0), err
	}
	// Mark the proposer of incoming block as dirty.
	if b.Position.ChainID < cfg.numChains {
		to.dirtyChainIDs = append(to.dirtyChainIDs, int(b.Position.ChainID))
		_, picked := to.candidateChainMapping[b.Position.ChainID]
		if pos == 0 && !picked {
			if to.isAckOnlyPrecedings(b) {
				to.prepareCandidate(b)
			}
		}
	}
	if to.duringFlush {
		return to.flushBlocks(b)
	}
	return to.deliverBlocks()
}
