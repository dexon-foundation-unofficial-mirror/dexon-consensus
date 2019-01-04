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

package core

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
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
	// ErrInvalidDAG is reported when block subbmitted to totalOrdering
	// didn't form a DAG.
	ErrInvalidDAG = errors.New("invalid dag")
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
	// ErrTotalOrderingPhiRatio means invalid phi ratio
	ErrTotalOrderingPhiRatio = errors.New("invalid total ordering phi ratio")
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

	numChains       uint32
	isFlushRequired bool
}

func (config *totalOrderingConfig) fromConfig(round uint64, cfg *types.Config) {
	config.k = uint64(cfg.K)
	config.numChains = cfg.NumChains
	config.phi = uint64(float32(cfg.NumChains-1)*cfg.PhiRatio + 1)
	config.setupRoundBasedFields(round, cfg)
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

// totalOrderingWinRecord caches the comparison of candidates calculated by
// their height vector.
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

func newTotalOrderingWinRecord(numChains uint32) *totalOrderingWinRecord {
	return &totalOrderingWinRecord{
		wins:  make([]int8, numChains),
		count: 0,
	}
}

// grade implements the 'grade' potential function in algorithm.
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

// totalOrderingHeightRecord records:
// - the minimum height of block which acks this block.
// - the count of blocks acking this block.
type totalOrderingHeightRecord struct{ minHeight, count uint64 }

// totalOrderingCache caches objects for reuse and not being colloected by GC.
// Each cached target has "get-" and "put-" functions for getting and reusing
// of objects.
type totalOrderingCache struct {
	ackedStatus   [][]*totalOrderingHeightRecord
	heightVectors [][]uint64
	winRecords    [][]*totalOrderingWinRecord
	winRecordPool sync.Pool
	ackedVectors  []map[common.Hash]struct{}
	numChains     uint32
}

// newTotalOrderingObjectCache constructs an totalOrderingCache instance.
func newTotalOrderingObjectCache(numChains uint32) *totalOrderingCache {
	return &totalOrderingCache{
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
func (cache *totalOrderingCache) resize(numChains uint32) {
	// Basically, everything in cache needs to be cleaned.
	if cache.numChains >= numChains {
		return
	}
	cache.ackedStatus = nil
	cache.heightVectors = nil
	cache.winRecords = nil
	cache.ackedVectors = nil
	cache.numChains = numChains
	cache.winRecordPool = sync.Pool{
		New: func() interface{} {
			return newTotalOrderingWinRecord(numChains)
		},
	}
}

func (cache *totalOrderingCache) getAckedStatus() (
	acked []*totalOrderingHeightRecord) {

	if len(cache.ackedStatus) == 0 {
		acked = make([]*totalOrderingHeightRecord, cache.numChains)
		for idx := range acked {
			acked[idx] = &totalOrderingHeightRecord{count: 0}
		}
	} else {
		acked = cache.ackedStatus[len(cache.ackedStatus)-1]
		cache.ackedStatus = cache.ackedStatus[:len(cache.ackedStatus)-1]
		// Reset acked status.
		for idx := range acked {
			acked[idx].count = 0
		}
	}
	return
}

func (cache *totalOrderingCache) putAckedStatus(
	acked []*totalOrderingHeightRecord) {
	// If the recycled objects supports lower numChains than we required,
	// don't recycle it.
	if uint32(len(acked)) != cache.numChains {
		return
	}
	cache.ackedStatus = append(cache.ackedStatus, acked)
}

func (cache *totalOrderingCache) getWinRecord() (
	win *totalOrderingWinRecord) {
	win = cache.winRecordPool.Get().(*totalOrderingWinRecord)
	win.reset()
	return
}

func (cache *totalOrderingCache) putWinRecord(win *totalOrderingWinRecord) {
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

func (cache *totalOrderingCache) getHeightVector() (hv []uint64) {
	if len(cache.heightVectors) == 0 {
		hv = make([]uint64, cache.numChains)
	} else {
		hv = cache.heightVectors[len(cache.heightVectors)-1]
		cache.heightVectors = cache.heightVectors[:len(cache.heightVectors)-1]
	}
	for idx := range hv {
		hv[idx] = infinity
	}
	return
}

func (cache *totalOrderingCache) putHeightVector(hv []uint64) {
	if uint32(len(hv)) != cache.numChains {
		return
	}
	cache.heightVectors = append(cache.heightVectors, hv)
}

func (cache *totalOrderingCache) getWinRecords() (w []*totalOrderingWinRecord) {
	if len(cache.winRecords) == 0 {
		w = make([]*totalOrderingWinRecord, cache.numChains)
	} else {
		w = cache.winRecords[len(cache.winRecords)-1]
		cache.winRecords = cache.winRecords[:len(cache.winRecords)-1]
		for idx := range w {
			w[idx] = nil
		}
	}
	return
}

func (cache *totalOrderingCache) putWinRecords(w []*totalOrderingWinRecord) {
	if uint32(len(w)) != cache.numChains {
		return
	}
	cache.winRecords = append(cache.winRecords, w)
}

func (cache *totalOrderingCache) getAckedVector() (
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

func (cache *totalOrderingCache) putAckedVector(
	acked map[common.Hash]struct{}) {
	if acked != nil {
		cache.ackedVectors = append(cache.ackedVectors, acked)
	}
}

// totalOrderingCandidateInfo stores proceding status for a candidate including
// - acked status as height records, which keeps the number of blocks from other
//   chains acking this candidate.
// - cached height vector, which valids height based on K-level used for
//   comparison in 'grade' function.
// - cached result of grade function to other candidates.
//
// Height Record:
//   When block A acks block B, all blocks proposed from the same proposer as
//   block A with higher height also acks block B. Thus records below is needed
//   - the minimum height of acking block from that proposer
//   - count of acking blocks from that proposer
//   to repsent the acking status for block A.
type totalOrderingCandidateInfo struct {
	ackedStatus        []*totalOrderingHeightRecord
	cachedHeightVector []uint64
	winRecords         []*totalOrderingWinRecord
	hash               common.Hash
}

// newTotalOrderingCandidateInfo creates an totalOrderingCandidateInfo instance.
func newTotalOrderingCandidateInfo(
	candidateHash common.Hash,
	objCache *totalOrderingCache) *totalOrderingCandidateInfo {
	return &totalOrderingCandidateInfo{
		ackedStatus: objCache.getAckedStatus(),
		winRecords:  objCache.getWinRecords(),
		hash:        candidateHash,
	}
}

// clean clears information related to another candidate, which should be called
// when that candidate is selected in deliver set.
func (v *totalOrderingCandidateInfo) clean(otherCandidateChainID uint32) {
	v.winRecords[otherCandidateChainID] = nil
}

// recycle recycles objects for later usage, this eases GC's work.
func (v *totalOrderingCandidateInfo) recycle(objCache *totalOrderingCache) {
	if v.winRecords != nil {
		for _, win := range v.winRecords {
			objCache.putWinRecord(win)
		}
		objCache.putWinRecords(v.winRecords)
	}
	if v.cachedHeightVector != nil {
		objCache.putHeightVector(v.cachedHeightVector)
	}
	objCache.putAckedStatus(v.ackedStatus)
}

// addBlock would update totalOrderingCandidateInfo, it's caller's duty
// to make sure the input block acutally acking the target block.
func (v *totalOrderingCandidateInfo) addBlock(b *types.Block) error {
	rec := v.ackedStatus[b.Position.ChainID]
	if rec.count == 0 {
		rec.minHeight = b.Position.Height
		rec.count = 1
	} else {
		if b.Position.Height <= rec.minHeight {
			return ErrInvalidDAG
		}
		rec.count++
	}
	return nil
}

// getAckingNodeSetLength returns the size of acking node set. Only heights
// larger than "global minimum height + k" are counted. For example, global
// minimum acking height is 1 and k is 1, only block heights which is larger or
// equal to 2 are added into acking node set.
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
	objCache *totalOrderingCache) {

	var (
		idx       int
		gRec, rec *totalOrderingHeightRecord
	)
	// The reason for not merging two loops is that the performance impact of map
	// iteration is large if the size is large. Iteration of dirty chains is
	// faster the map.
	// TODO(mission): merge the code in this if/else if the performance won't be
	//                downgraded when adding a function for the shared part.
	if v.cachedHeightVector == nil {
		// Generate height vector from scratch.
		v.cachedHeightVector = objCache.getHeightVector()
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

// updateWinRecord setups win records from two candidates.
func (v *totalOrderingCandidateInfo) updateWinRecord(
	otherChainID uint32,
	other *totalOrderingCandidateInfo,
	dirtyChainIDs []int,
	objCache *totalOrderingCache,
	numChains uint32) {
	var (
		idx    int
		height uint64
	)
	// The reason not to merge two loops is that the iteration of map is
	// expensive when chain count is large, iterating of dirty chains is cheaper.
	// TODO(mission): merge the code in this if/else if adding a function won't
	// affect the performance.
	win := v.winRecords[otherChainID]
	if win == nil {
		win = objCache.getWinRecord()
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

// totalOrderingBreakpoint is a record of height discontinuity on a chain
type totalOrderingBreakpoint struct {
	roundID uint64
	// height of last block.
	lastHeight uint64
}

// totalOrderingGroupVector keeps global status of current pending set.
type totalOrderingGlobalVector struct {
	// blocks stores all blocks grouped by their proposers and sorted by height.
	// TODO(mission): slice used here reallocates frequently.
	blocks [][]*types.Block

	// breakpoints stores rounds for chains that blocks' height on them are
	// not consecutive, for example in chain i
	// Round  Height
	//     0       0
	//     0       1
	//     1       2
	//     1       3
	//     1       4
	//     2       -  <- a config change of chain number occured
	//     2       -
	//     3       -
	//     3       -
	//     4       0  <- a breakpoint for round 3 is cached here
	//     5       -
	//     5       -
	//     6       0  <- breakpoint again
	// breakpoints[i][0] == &totalOrderingBreakpoint{roundID: 4, lastHeight: 4}
	// breakpoints[i][1] == &totalOrderingBreakpoint{roundID: 6, lastHeight: 0}
	breakpoints [][]*totalOrderingBreakpoint

	// curRound stores the last round ID used for purging breakpoints.
	curRound uint64

	// tips records the last seen block for each chain.
	tips []*types.Block

	// Only ackedStatus in cachedCandidateInfo is used.
	cachedCandidateInfo *totalOrderingCandidateInfo
}

func newTotalOrderingGlobalVector(
	initRound uint64, numChains uint32) *totalOrderingGlobalVector {
	return &totalOrderingGlobalVector{
		blocks:      make([][]*types.Block, numChains),
		tips:        make([]*types.Block, numChains),
		breakpoints: make([][]*totalOrderingBreakpoint, numChains),
		curRound:    initRound,
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
		// If no breakpoint, count is the amount of blocks.
		rec.count = uint64(len(global.blocks[chainID]))
	} else {
		// If there are breakpoints, only the first counts.
		rec.count = breakpoints[0].lastHeight - candidate.Position.Height + 1
	}
	info.ackedStatus[chainID] = rec
	if acked == nil {
		return
	}
	for idx, blocks := range global.blocks {
		if idx == int(chainID) {
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
			// If this block acks the candidate, all newer blocks from the same chain
			// also 'indirectly' acks the candidate.
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
	b *types.Block) (isOldest bool, pending bool, err error) {
	// isOldest implies the block is the oldest in global vector
	chainID := b.Position.ChainID
	tip := global.tips[chainID]
	isOldest = len(global.blocks[chainID]) == 0
	if tip != nil {
		// Perform light weight sanity check based on tip.
		if tip.Position.Round > b.Position.Round {
			err = ErrInvalidDAG
			return
		}
		if DiffUint64(tip.Position.Round, b.Position.Round) > 1 {
			if b.Position.Height != 0 {
				err = ErrInvalidDAG
				return
			}
			// Add breakpoint.
			global.breakpoints[chainID] = append(
				global.breakpoints[chainID],
				&totalOrderingBreakpoint{
					roundID:    b.Position.Round,
					lastHeight: tip.Position.Height,
				})
		} else {
			if b.Position.Height != tip.Position.Height+1 {
				err = ErrInvalidDAG
				return
			}
		}
	} else {
		if b.Position.Round < global.curRound {
			err = ErrBlockFromPastRound
			return
		}
		if b.Position.Round > global.curRound {
			// Add breakpoint.
			bp := &totalOrderingBreakpoint{
				roundID:    b.Position.Round,
				lastHeight: 0,
			}
			global.breakpoints[chainID] = append(global.breakpoints[chainID], bp)
		}
	}
	bps := global.breakpoints[chainID]
	pending = len(bps) > 0 && bps[0].roundID <= b.Position.Round
	global.blocks[chainID] = append(global.blocks[chainID], b)
	global.tips[chainID] = b
	return
}

// updateCandidateInfo udpates cached candidate info.
func (global *totalOrderingGlobalVector) updateCandidateInfo(
	dirtyChainIDs []int, objCache *totalOrderingCache) {
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

// totalOrdering represent a process unit to handle total ordering for blocks.
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

	// flushed is a map of flushed blocks.
	flushed map[uint32]struct{}

	// globalVector group all pending blocks by proposers and
	// sort them by block height. This structure is helpful when:
	//
	//  - build global height vector
	//  - picking candidates next round
	globalVector *totalOrderingGlobalVector

	// candidates caches result of potential function during generating preceding
	// set.
	candidates []*totalOrderingCandidateInfo

	// acked stores the 'block A acked by block B' by acked[A.Hash][B.Hash]
	acked map[common.Hash]map[common.Hash]struct{}

	// dirtyChainIDs stores chainIDs that is "dirty", i.e. needed updating all
	// cached statuses (win record, acking status).
	dirtyChainIDs []int

	// objCache caches allocated objects, like map.
	objCache *totalOrderingCache

	// candidateChainMapping keeps a mapping from candidate's hash to
	// their chain IDs.
	candidateChainMapping map[uint32]common.Hash

	// candidateChainIDs records chain ID of all candidates.
	candidateChainIDs []uint32

	// configs keeps configuration for each round in continuous way.
	configs []*totalOrderingConfig
}

// newTotalOrdering constructs an totalOrdering instance.
func newTotalOrdering(
	dMoment time.Time, round uint64, cfg *types.Config) *totalOrdering {
	config := &totalOrderingConfig{}
	config.fromConfig(round, cfg)
	config.setRoundBeginTime(dMoment)
	candidates := make([]*totalOrderingCandidateInfo, config.numChains)
	to := &totalOrdering{
		pendings:              make(map[common.Hash]*types.Block),
		dirtyChainIDs:         make([]int, 0, config.numChains),
		acked:                 make(map[common.Hash]map[common.Hash]struct{}),
		objCache:              newTotalOrderingObjectCache(config.numChains),
		candidateChainMapping: make(map[uint32]common.Hash),
		candidates:            candidates,
		candidateChainIDs:     make([]uint32, 0, config.numChains),
		curRound:              config.roundID,
		globalVector: newTotalOrderingGlobalVector(
			config.roundID, config.numChains),
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
	if config.PhiRatio < 0.5 || config.PhiRatio > 1.0 {
		return ErrTotalOrderingPhiRatio
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

// buildBlockRelation update all its indirect acks recursively.
func (to *totalOrdering) buildBlockRelation(b *types.Block) {
	var (
		curBlock, nextBlock      *types.Block
		ack                      common.Hash
		acked                    map[common.Hash]struct{}
		exists, alreadyPopulated bool
		toCheck                  = []*types.Block{b}
	)
	for len(toCheck) != 0 {
		curBlock, toCheck = toCheck[len(toCheck)-1], toCheck[:len(toCheck)-1]
		if curBlock.Position.Round > b.Position.Round {
			// It's illegal for a block to ack some blocks in future round.
			panic(ErrForwardAck)
		}
		for _, ack = range curBlock.Acks {
			if acked, exists = to.acked[ack]; !exists {
				acked = to.objCache.getAckedVector()
				to.acked[ack] = acked
			}
			// Check if the block is handled.
			if _, alreadyPopulated = acked[b.Hash]; alreadyPopulated {
				continue
			}
			acked[b.Hash] = struct{}{}
			// See if we need to do this recursively.
			if nextBlock, exists = to.pendings[ack]; exists {
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
	to.objCache.putAckedVector(to.acked[h])
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
func (to *totalOrdering) updateVectors(
	b *types.Block) (isOldest bool, err error) {
	var (
		candidateHash common.Hash
		chainID       uint32
		acked         bool
		pending       bool
	)
	// Update global height vector
	if isOldest, pending, err = to.globalVector.addBlock(b); err != nil {
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
		// This works because forward acking blocks are rejected.
		return
	}
	// Update candidates' acking status.
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

// prepareCandidate builds totalOrderingCandidateInfo for a new candidate.
func (to *totalOrdering) prepareCandidate(b *types.Block) {
	var (
		info    = newTotalOrderingCandidateInfo(b.Hash, to.objCache)
		chainID = b.Position.ChainID
	)
	to.candidates[chainID] = info
	to.candidateChainMapping[chainID] = b.Hash
	// Add index to slot to allocated list, make sure the modified list is sorted.
	to.candidateChainIDs = append(to.candidateChainIDs, chainID)
	sort.Slice(to.candidateChainIDs, func(i, j int) bool {
		return to.candidateChainIDs[i] < to.candidateChainIDs[j]
	})
	to.globalVector.prepareHeightRecord(b, info, to.acked[b.Hash])
	return
}

// isCandidate checks if a block only contains acks to delivered blocks.
func (to *totalOrdering) isCandidate(b *types.Block) bool {
	for _, ack := range b.Acks {
		if _, exists := to.pendings[ack]; exists {
			return false
		}
	}
	return true
}

// output finishes the delivery of preceding set.
func (to *totalOrdering) output(
	precedings map[common.Hash]struct{},
	numChains uint32) (ret []*types.Block) {

	for p := range precedings {
		// Remove the first element from corresponding blockVector.
		b := to.pendings[p]
		chainID := b.Position.ChainID
		// TODO(mission): frequent reallocation here.
		to.globalVector.blocks[chainID] = to.globalVector.blocks[chainID][1:]
		ret = append(ret, b)
		// Remove block relations.
		to.clean(b)
		to.dirtyChainIDs = append(to.dirtyChainIDs, int(chainID))
	}
	sort.Sort(types.ByHash(ret))
	// Find new candidates from global vector's tips.
	// The complexity here is O(N^2logN).
	// TODO(mission): only tips which acking some blocks in the devliered set
	// should be checked. This improvement related to the latency introduced by K.
	for chainID, blocks := range to.globalVector.blocks[:numChains] {
		if len(blocks) == 0 {
			continue
		}
		if _, picked := to.candidateChainMapping[uint32(chainID)]; picked {
			continue
		}
		if !to.isCandidate(blocks[0]) {
			continue
		}
		// Build totalOrderingCandidateInfo for new candidate.
		to.prepareCandidate(blocks[0])
	}
	return
}

// generateDeliverSet generates preceding set and checks if the preceding set
// is deliverable by potential function.
func (to *totalOrdering) generateDeliverSet() (
	delivered map[common.Hash]struct{}, mode uint32) {

	var (
		chainID, otherChainID uint32
		info, otherInfo       *totalOrderingCandidateInfo
		precedings            = make(map[uint32]struct{})
		cfg                   = to.getCurrentConfig()
	)
	mode = TotalOrderingModeNormal
	to.globalVector.updateCandidateInfo(to.dirtyChainIDs, to.objCache)
	globalInfo := to.globalVector.cachedCandidateInfo
	for _, chainID = range to.candidateChainIDs {
		to.candidates[chainID].updateAckingHeightVector(
			globalInfo, cfg.k, to.dirtyChainIDs, to.objCache)
	}
	// Update winning records for each candidate.
	// TODO(mission): It's not reasonable to request one routine for each
	// candidate, the context switch rate would be high.
	var wg sync.WaitGroup
	wg.Add(len(to.candidateChainIDs))
	for _, chainID := range to.candidateChainIDs {
		info = to.candidates[chainID]
		go func(can uint32, canInfo *totalOrderingCandidateInfo) {
			defer wg.Done()
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
		}(chainID, info)
	}
	wg.Wait()
	// Reset dirty chains.
	to.dirtyChainIDs = to.dirtyChainIDs[:0]
	// TODO(mission): ANS should be bounded by current numChains.
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
			// TODO(mission): grade should be bounded by current numChains.
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
func (to *totalOrdering) flushBlocks() (
	flushed []*types.Block, mode uint32, err error) {
	mode = TotalOrderingModeFlush
	cfg := to.getCurrentConfig()

	// Flush blocks until last blocks from all chains appeared.
	if len(to.flushReadyChains) < int(cfg.numChains) {
		return
	}
	if len(to.flushReadyChains) > int(cfg.numChains) {
		// This case should never be occured.
		err = ErrFutureRoundDelivered
		return
	}
	// Dump all blocks in this round.
	for len(to.flushed) != int(cfg.numChains) {
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
			if cfg.isLastBlock(b) {
				to.flushed[b.Position.ChainID] = struct{}{}
			}
		}
		flushed = append(flushed, flushedBlocks...)
	}
	// Switch back to non-flushing mode.
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
	// Force picking new candidates.
	numChains := to.getCurrentConfig().numChains
	to.output(map[common.Hash]struct{}{}, numChains)
	return
}

// deliverBlocks delivers blocks by DEXON total ordering algorithm.
func (to *totalOrdering) deliverBlocks() (
	delivered []*types.Block, mode uint32, err error) {

	hashes, mode := to.generateDeliverSet()
	cfg := to.getCurrentConfig()
	// Output precedings.
	delivered = to.output(hashes, cfg.numChains)
	// Check if any block in delivered set is the last block in this round, if
	// there is, perform flush or round-switch.
	for _, b := range delivered {
		if b.Position.Round > to.curRound {
			err = ErrFutureRoundDelivered
			return
		}
		if !cfg.isLastBlock(b) {
			continue
		}
		// Code reaches here if a last block is processed. This triggers
		// "duringFlush" mode if config changes.
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
		// Collect last blocks until all last blocks appears and function
		// flushBlocks will be called.
		for _, b := range delivered {
			if cfg.isLastBlock(b) {
				to.flushed[b.Position.ChainID] = struct{}{}
			}
		}
		// Some last blocks for the round to be flushed might not be delivered
		// yet.
		for _, tip := range to.globalVector.tips[:cfg.numChains] {
			if tip.Position.Round > to.curRound || cfg.isLastBlock(tip) {
				to.flushReadyChains[tip.Position.ChainID] = struct{}{}
			}
		}
	}
	return
}

func (to *totalOrdering) getCurrentConfig() *totalOrderingConfig {
	cfgIdx := to.curRound - to.configs[0].roundID
	if cfgIdx >= uint64(len(to.configs)) {
		panic(fmt.Errorf("total ordering config is not ready: %v, %v, %v",
			to.curRound, to.configs[0].roundID, len(to.configs)))
	}
	return to.configs[cfgIdx]
}

// addBlock adds a block to the working set of total ordering module.
func (to *totalOrdering) addBlock(b *types.Block) error {
	// NOTE: Block b is assumed to be in topologically sorted, i.e., all its
	// acking blocks are during or after total ordering stage.
	cfg := to.getCurrentConfig()
	to.pendings[b.Hash] = b
	to.buildBlockRelation(b)
	isOldest, err := to.updateVectors(b)
	if err != nil {
		return err
	}
	// Mark the proposer of incoming block as dirty.
	if b.Position.ChainID < cfg.numChains {
		to.dirtyChainIDs = append(to.dirtyChainIDs, int(b.Position.ChainID))
		_, exists := to.candidateChainMapping[b.Position.ChainID]
		if isOldest && !exists && to.isCandidate(b) {
			// isOldest means b is the oldest block in global vector, and isCandidate
			// is still needed here due to round change. For example:
			// o  o  o  <- genesis block for round change, isCandidate returns true
			// |  |        but isOldest is false
			// o  o
			// |  |
			// o  o  o  <- isOldest is true but isCandidate returns false
			// |  | /
			// o  o
			to.prepareCandidate(b)
		}
	}
	if to.duringFlush && cfg.isLastBlock(b) {
		to.flushReadyChains[b.Position.ChainID] = struct{}{}
	}
	return nil
}

// extractBlocks check if there is any deliverable set.
func (to *totalOrdering) extractBlocks() ([]*types.Block, uint32, error) {
	if to.duringFlush {
		return to.flushBlocks()
	}
	return to.deliverBlocks()
}
