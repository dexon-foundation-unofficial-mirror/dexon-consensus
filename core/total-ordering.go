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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

const (
	infinity uint64 = math.MaxUint64
)

// ErrNotValidDAG would be reported when block subbmitted to totalOrdering
// didn't form a DAG.
var ErrNotValidDAG = fmt.Errorf("not a valid dag")

// ackingStatusVector describes the acking status, either globally or just
// for one candidate.
//
// When block A acks block B, all blocks proposed from the same proposer
// as block A with higher height would also acks block B. Therefore,
// we just need to record:
//  - the minimum height of acking block from that proposer
//  - count of acking blocks from that proposer
// to repsent the acking status for block A.
type ackingStatusVector map[types.ValidatorID]*struct{ minHeight, count uint64 }

// addBlock would update ackingStatusVector, it's caller's duty
// to make sure the input block acutally acking the target block.
func (v ackingStatusVector) addBlock(b *types.Block) (err error) {
	rec, exists := v[b.ProposerID]
	if !exists {
		v[b.ProposerID] = &struct {
			minHeight, count uint64
		}{
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

// getAckingNodeSet would generate the Acking Node Set.
// Only block height larger than
//
//    global minimum height + k
//
// would be taken into consideration, ex.
//
//  For some validator X:
//   - the global minimum acking height = 1,
//   - k = 1
//  then only block height >= 2 would be added to acking node set.
func (v ackingStatusVector) getAckingNodeSet(
	global ackingStatusVector, k uint64) map[types.ValidatorID]struct{} {

	ret := make(map[types.ValidatorID]struct{})
	for vID, gRec := range global {
		rec, exists := v[vID]
		if !exists {
			continue
		}

		// This line would check if these two ranges would overlap:
		//  - (global minimum height + k, infinity)
		//  - (local minimum height, local minimum height + count - 1)
		if rec.minHeight+rec.count-1 >= gRec.minHeight+k {
			ret[vID] = struct{}{}
		}
	}
	return ret
}

// getAckingHeightVector would convert 'ackingStatusVector' to
// Acking Height Vector.
//
// Only block height equals to (global minimum block height + k) would be
// taken into consideration.
func (v ackingStatusVector) getAckingHeightVector(
	global ackingStatusVector, k uint64) map[types.ValidatorID]uint64 {

	ret := make(map[types.ValidatorID]uint64)
	for vID, gRec := range global {
		rec, exists := v[vID]

		if gRec.count <= k {
			continue
		} else if !exists {
			ret[vID] = infinity
		} else if rec.minHeight <= gRec.minHeight+k {
			// This check is sufficient to make sure the block height:
			//
			//   gRec.minHeight + k
			//
			// would be included in this ackingStatusVector.
			ret[vID] = gRec.minHeight + k
		} else {
			ret[vID] = infinity
		}
	}
	return ret
}

// blockVector stores all blocks grouped by their proposers and
// sorted by their block height.
type blockVector map[types.ValidatorID][]*types.Block

func (v blockVector) addBlock(b *types.Block) (err error) {
	blocksFromProposer := v[b.ProposerID]
	if len(blocksFromProposer) > 0 {
		lastBlock := blocksFromProposer[len(blocksFromProposer)-1]
		if b.Height-lastBlock.Height != 1 {
			err = ErrNotValidDAG
			return
		}
	}
	v[b.ProposerID] = append(blocksFromProposer, b)
	return
}

// getAckingStatusVector would convert a blockVector to
// ackingStatusVectorAckingStatus.
func (v blockVector) getAckingStatusVector() ackingStatusVector {
	ret := ackingStatusVector{}
	for vID, vec := range v {
		if len(vec) == 0 {
			continue
		}
		ret[vID] = &struct {
			minHeight, count uint64
		}{
			minHeight: vec[0].Height,
			count:     uint64(len(vec)),
		}
	}
	return ret
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
	globalVector blockVector

	// candidateAckingStatusVectors caches ackingStatusVector of candidates.
	candidateAckingStatusVectors map[common.Hash]ackingStatusVector

	// acked cache the 'block A acked by block B' relation by
	// keeping a record in acked[A.Hash][B.Hash]
	acked map[common.Hash]map[common.Hash]struct{}
}

func newTotalOrdering(k, phi, validatorCount uint64) *totalOrdering {
	return &totalOrdering{
		candidateAckingStatusVectors: make(map[common.Hash]ackingStatusVector),
		pendings:                     make(map[common.Hash]*types.Block),
		k:                            k,
		phi:                          phi,
		validatorCount:               validatorCount,
		globalVector:                 blockVector{},
		acked:                        make(map[common.Hash]map[common.Hash]struct{}),
	}
}

// buildBlockRelation populates the acked according their acking relationships.
func (to *totalOrdering) buildBlockRelation(b *types.Block) {
	// populateAcked would update all blocks implcitly acked
	// by input block recursively.
	var populateAcked func(bx, target *types.Block)
	populateAcked = func(bx, target *types.Block) {
		for ack := range bx.Acks {
			acked, exists := to.acked[ack]
			if !exists {
				acked = make(map[common.Hash]struct{})
				to.acked[ack] = acked
			}

			// This means we've walked this block already.
			if _, alreadyPopulated := acked[target.Hash]; alreadyPopulated {
				continue
			}
			acked[target.Hash] = struct{}{}

			// See if we need to go forward.
			if nextBlock, exists := to.pendings[ack]; !exists {
				continue
			} else {
				populateAcked(nextBlock, target)
			}
		}
	}
	populateAcked(b, b)
}

// clean would remove a block from working set. This behaviour
// would prevent our memory usage growing infinity.
func (to *totalOrdering) clean(h common.Hash) {
	delete(to.acked, h)
	delete(to.pendings, h)
	delete(to.candidateAckingStatusVectors, h)
}

// updateVectors is a helper function to update all cached vectors.
func (to *totalOrdering) updateVectors(b *types.Block) (err error) {
	// Update global height vector
	err = to.globalVector.addBlock(b)
	if err != nil {
		return
	}

	// Update acking status of candidates.
	for candidate, vector := range to.candidateAckingStatusVectors {
		if _, acked := to.acked[candidate][b.Hash]; !acked {
			continue
		}
		if err = vector.addBlock(b); err != nil {
			return
		}
	}
	return
}

// grade implements the 'grade' potential function described in white paper.
func (to *totalOrdering) grade(
	hvFrom, hvTo map[types.ValidatorID]uint64,
	globalAns map[types.ValidatorID]struct{}) int {

	count := uint64(0)
	for vID, hFrom := range hvFrom {
		hTo, exists := hvTo[vID]
		if !exists {
			continue
		}

		if hFrom != infinity && hTo == infinity {
			count++
		}
	}

	if count >= to.phi {
		return 1
	} else if count < to.phi-to.validatorCount+uint64(len(globalAns)) {
		return 0
	} else {
		return -1
	}
}

// buildAckingStatusVectorForNewCandidate is a helper function to
// build ackingStatusVector for new candidate.
func (to *totalOrdering) buildAckingStatusVectorForNewCandidate(
	candidate *types.Block) (hVec ackingStatusVector) {

	blocks := to.globalVector[candidate.ProposerID]
	hVec = ackingStatusVector{
		candidate.ProposerID: &struct {
			minHeight, count uint64
		}{
			minHeight: candidate.Height,
			count:     uint64(len(blocks)),
		},
	}

	ackedsForCandidate, exists := to.acked[candidate.Hash]
	if !exists {
		// This candidate is acked by nobody.
		return
	}

	for vID, blocks := range to.globalVector {
		if vID == candidate.ProposerID {
			continue
		}

		for i, b := range blocks {
			if _, acked := ackedsForCandidate[b.Hash]; !acked {
				continue
			}

			// If this block acks this candidate, all newer blocks
			// from the same validator also 'indirect' acks it.
			hVec[vID] = &struct {
				minHeight, count uint64
			}{
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
		to.globalVector[b.ProposerID] = to.globalVector[b.ProposerID][1:]
		ret = append(ret, b)

		// Remove block relations.
		to.clean(p)
	}
	sort.Sort(types.ByHash(ret))

	// Find new candidates from tip of globalVector of each validator.
	// The complexity here is O(N^2logN).
	for _, blocks := range to.globalVector {
		if len(blocks) == 0 {
			continue
		}

		tip := blocks[0]
		if _, alreadyCandidate :=
			to.candidateAckingStatusVectors[tip.Hash]; alreadyCandidate {
			continue
		}

		if !to.isAckOnlyPrecedings(tip) {
			continue
		}

		// Build ackingStatusVector for new candidate.
		to.candidateAckingStatusVectors[tip.Hash] =
			to.buildAckingStatusVectorForNewCandidate(tip)
	}
	return ret
}

// generateDeliverSet would:
//  - generate preceding set
//  - check if the preceding set deliverable by checking potential function
func (to *totalOrdering) generateDeliverSet() (
	delivered map[common.Hash]struct{}, early bool) {

	globalAckingStatusVector := to.globalVector.getAckingStatusVector()
	ahvs := map[common.Hash]map[types.ValidatorID]uint64{}
	for candidate, v := range to.candidateAckingStatusVectors {
		ahvs[candidate] = v.getAckingHeightVector(globalAckingStatusVector, to.k)
	}

	globalAns := globalAckingStatusVector.getAckingNodeSet(
		globalAckingStatusVector, to.k)
	precedings := make(map[common.Hash]struct{})

CheckNextCandidateLoop:
	for candidate := range to.candidateAckingStatusVectors {
		for otherCandidate := range to.candidateAckingStatusVectors {
			if candidate == otherCandidate {
				continue
			}
			if to.grade(ahvs[otherCandidate], ahvs[candidate], globalAns) != 0 {
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
		for candidate := range to.candidateAckingStatusVectors {
			if _, isPreceding := precedings[candidate]; isPreceding {
				continue
			}

			beaten := false
			for p := range precedings {
				if beaten =
					to.grade(ahvs[p], ahvs[candidate], globalAns) == 1; beaten {
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
		for p := range precedings {
			count := uint64(0)
			for _, v := range ahvs[p] {
				if v != infinity {
					count++
				}
			}

			if count > to.phi {
				return true
			}
		}
		return false
	}

	// checkANS is a helper function to verify external stability.
	// It would make sure all preceding blocks are strong enough
	// to be delivered.
	checkANS := func() bool {
		for p := range precedings {
			validatorAns := to.candidateAckingStatusVectors[p].getAckingNodeSet(
				globalAckingStatusVector, to.k)
			if uint64(len(validatorAns)) < to.validatorCount-to.phi {
				return false
			}
		}

		return true
	}

	// Check internal stability first.
	if !internal() {
		return
	}

	// If all validators propose enough blocks, we should force
	// to deliver since the whole picture of the DAG is revealed.
	if uint64(len(globalAns)) != to.validatorCount {
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

	// Incremental part.
	to.pendings[b.Hash] = b
	to.buildBlockRelation(b)
	if err = to.updateVectors(b); err != nil {
		return
	}
	if to.isAckOnlyPrecedings(b) {
		to.candidateAckingStatusVectors[b.Hash] =
			to.buildAckingStatusVectorForNewCandidate(b)
	}

	// Not-Incremental part (yet).
	//  - generate ahv for each candidate
	//  - generate ans for each candidate
	//  - generate global ans
	//  - find preceding set
	hashes, early := to.generateDeliverSet()

	// output precedings
	delivered = to.output(hashes)
	return
}
