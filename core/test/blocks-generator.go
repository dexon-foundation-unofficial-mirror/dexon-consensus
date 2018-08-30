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
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// ErrParentNotAcked would be raised when some block doesn't
// ack its parent block.
var ErrParentNotAcked = errors.New("parent is not acked")

// validatorStatus is a state holder for each validator
// during generating blocks.
type validatorStatus struct {
	blocks           []*types.Block
	lastAckingHeight map[types.ValidatorID]uint64
}

type hashBlockFn func(types.BlockConverter) (common.Hash, error)

// getAckedBlockHash would randomly pick one block between
// last acked one to current head.
func (vs *validatorStatus) getAckedBlockHash(
	ackedVID types.ValidatorID,
	ackedValidator *validatorStatus,
	randGen *rand.Rand) (
	hash common.Hash, ok bool) {

	baseAckingHeight, exists := vs.lastAckingHeight[ackedVID]
	if exists {
		// Do not ack the same block(height) twice.
		baseAckingHeight++
	}
	totalBlockCount := uint64(len(ackedValidator.blocks))
	if totalBlockCount <= baseAckingHeight {
		// There is no new block to ack.
		return
	}
	ackableRange := totalBlockCount - baseAckingHeight
	height := uint64((randGen.Uint64() % ackableRange) + baseAckingHeight)
	vs.lastAckingHeight[ackedVID] = height
	hash = ackedValidator.blocks[height].Hash
	ok = true
	return
}

// validatorSetStatus is a state holder for all validators
// during generating blocks.
type validatorSetStatus struct {
	status       map[types.ValidatorID]*validatorStatus
	validatorIDs []types.ValidatorID
	randGen      *rand.Rand
	hashBlock    hashBlockFn
}

func newValidatorSetStatus(vIDs []types.ValidatorID, hashBlock hashBlockFn) *validatorSetStatus {
	status := make(map[types.ValidatorID]*validatorStatus)
	for _, vID := range vIDs {
		status[vID] = &validatorStatus{
			blocks:           []*types.Block{},
			lastAckingHeight: make(map[types.ValidatorID]uint64),
		}
	}
	return &validatorSetStatus{
		status:       status,
		validatorIDs: vIDs,
		randGen:      rand.New(rand.NewSource(time.Now().UnixNano())),
		hashBlock:    hashBlock,
	}
}

// findIncompleteValidators is a helper to check which validator
// doesn't generate enough blocks.
func (vs *validatorSetStatus) findIncompleteValidators(
	blockCount int) (vIDs []types.ValidatorID) {

	for vID, status := range vs.status {
		if len(status.blocks) < blockCount {
			vIDs = append(vIDs, vID)
		}
	}
	return
}

// prepareAcksForNewBlock collects acks for one block.
func (vs *validatorSetStatus) prepareAcksForNewBlock(
	proposerID types.ValidatorID, ackingCount int) (
	acks map[common.Hash]struct{}, err error) {

	acks = make(map[common.Hash]struct{})
	if len(vs.status[proposerID].blocks) == 0 {
		// The 'Acks' filed of genesis blocks would always be empty.
		return
	}
	// Pick validatorIDs to be acked.
	ackingVIDs := map[types.ValidatorID]struct{}{
		proposerID: struct{}{}, // Acking parent block is always required.
	}
	if ackingCount > 0 {
		ackingCount-- // We would always include ack to parent block.
	}
	for _, i := range vs.randGen.Perm(len(vs.validatorIDs))[:ackingCount] {
		ackingVIDs[vs.validatorIDs[i]] = struct{}{}
	}
	// Generate acks.
	for vID := range ackingVIDs {
		ack, ok := vs.status[proposerID].getAckedBlockHash(
			vID, vs.status[vID], vs.randGen)
		if !ok {
			if vID == proposerID {
				err = ErrParentNotAcked
			}
			continue
		}
		acks[ack] = struct{}{}
	}
	return
}

// proposeBlock propose new block and update validator status.
func (vs *validatorSetStatus) proposeBlock(
	proposerID types.ValidatorID,
	acks map[common.Hash]struct{}) (*types.Block, error) {

	status := vs.status[proposerID]
	parentHash := common.Hash{}
	if len(status.blocks) > 0 {
		parentHash = status.blocks[len(status.blocks)-1].Hash
	}

	ts := map[types.ValidatorID]time.Time{}
	for vid := range vs.status {
		ts[vid] = time.Time{}
	}
	newBlock := &types.Block{
		ProposerID: proposerID,
		ParentHash: parentHash,
		Height:     uint64(len(status.blocks)),
		Acks:       acks,
		Timestamps: ts,
		// TODO(mission.liao): Generate timestamp.
	}
	var err error
	newBlock.Hash, err = vs.hashBlock(newBlock)
	if err != nil {
		return nil, err
	}
	status.blocks = append(status.blocks, newBlock)
	return newBlock, nil
}

// normalAckingCountGenerator would randomly pick acking count
// by a normal distribution.
func normalAckingCountGenerator(
	validatorCount int, mean, deviation float64) func() int {

	return func() int {
		var expected float64
		for {
			expected = rand.NormFloat64()*deviation + mean
			if expected >= 0 && expected <= float64(validatorCount) {
				break
			}
		}
		return int(math.Ceil(expected))
	}
}

// MaxAckingCountGenerator return generator which returns
// fixed maximum acking count.
func MaxAckingCountGenerator(count int) func() int {
	return func() int { return count }
}

// generateValidatorPicker is a function generator, which would generate
// a function to randomly pick one validator ID from a slice of validator ID.
func generateValidatorPicker() func([]types.ValidatorID) types.ValidatorID {
	privateRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	return func(vIDs []types.ValidatorID) types.ValidatorID {
		return vIDs[privateRand.Intn(len(vIDs))]
	}
}

// BlocksGenerator could generate blocks forming valid DAGs.
type BlocksGenerator struct {
	validatorPicker func([]types.ValidatorID) types.ValidatorID
	hashBlock       hashBlockFn
}

// NewBlocksGenerator constructs BlockGenerator.
func NewBlocksGenerator(validatorPicker func(
	[]types.ValidatorID) types.ValidatorID,
	hashBlock hashBlockFn) *BlocksGenerator {

	if validatorPicker == nil {
		validatorPicker = generateValidatorPicker()
	}
	return &BlocksGenerator{
		validatorPicker: validatorPicker,
		hashBlock:       hashBlock,
	}
}

// Generate is the entry point to generate blocks. The caller is responsible
// to provide a function to generate count of acked block for each new block.
// The prototype of ackingCountGenerator is a function returning 'int'.
// For example, if you need to generate a group of blocks and each of them
// has maximum 2 acks.
//   func () int { return 2 }
// The default ackingCountGenerator would randomly pick a number based on
// the validatorCount you provided with a normal distribution.
func (gen *BlocksGenerator) Generate(
	validatorCount int,
	blockCount int,
	ackingCountGenerator func() int,
	writer blockdb.Writer) (
	validators types.ValidatorIDs, err error) {

	if ackingCountGenerator == nil {
		ackingCountGenerator = normalAckingCountGenerator(
			validatorCount,
			float64(validatorCount/2),
			float64(validatorCount/4+1))
	}
	validators = types.ValidatorIDs{}
	for i := 0; i < validatorCount; i++ {
		validators = append(
			validators, types.ValidatorID{Hash: common.NewRandomHash()})
	}
	status := newValidatorSetStatus(validators, gen.hashBlock)

	// We would record the smallest height of block that could be acked
	// from each validator's point-of-view.
	toAck := make(map[types.ValidatorID]map[types.ValidatorID]uint64)
	for _, vID := range validators {
		toAck[vID] = make(map[types.ValidatorID]uint64)
	}

	for {
		// Find validators that doesn't propose enough blocks and
		// pick one from them randomly.
		notYet := status.findIncompleteValidators(blockCount)
		if len(notYet) == 0 {
			break
		}

		// Propose a new block.
		var (
			proposerID = gen.validatorPicker(notYet)
			acks       map[common.Hash]struct{}
		)
		acks, err = status.prepareAcksForNewBlock(
			proposerID, ackingCountGenerator())
		if err != nil {
			return
		}
		var newBlock *types.Block
		newBlock, err = status.proposeBlock(proposerID, acks)
		if err != nil {
			return
		}

		// Persist block to db.
		err = writer.Put(*newBlock)
		if err != nil {
			return
		}
	}
	return
}
