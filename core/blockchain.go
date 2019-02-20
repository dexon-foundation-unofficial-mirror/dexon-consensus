// Copyright 2019 The dexon-consensus Authors
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
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors for sanity check error.
var (
	ErrBlockFromOlderPosition         = errors.New("block from older position")
	ErrNotGenesisBlock                = errors.New("not a genesis block")
	ErrIsGenesisBlock                 = errors.New("is a genesis block")
	ErrIncorrectParentHash            = errors.New("incorrect parent hash")
	ErrInvalidBlockHeight             = errors.New("invalid block height")
	ErrInvalidRoundID                 = errors.New("invalid round id")
	ErrNotFollowTipPosition           = errors.New("not follow tip position")
	ErrDuplicatedPendingBlock         = errors.New("duplicated pending block")
	ErrRetrySanityCheckLater          = errors.New("retry sanity check later")
	ErrRoundNotIncreasing             = errors.New("round not increasing")
	ErrRoundNotSwitch                 = errors.New("round not switch")
	ErrIncorrectBlockRandomnessResult = errors.New(
		"incorrect block randomness result")
)

type pendingBlockRecord struct {
	position types.Position
	block    *types.Block
}

type pendingBlockRecords []pendingBlockRecord

func (pb *pendingBlockRecords) insert(p pendingBlockRecord) error {
	idx := sort.Search(len(*pb), func(i int) bool {
		return !(*pb)[i].position.Older(p.position)
	})
	switch idx {
	case len(*pb):
		*pb = append(*pb, p)
	default:
		if (*pb)[idx].position.Equal(p.position) {
			return ErrDuplicatedPendingBlock
		}
		// Insert the value to that index.
		*pb = append((*pb), pendingBlockRecord{})
		copy((*pb)[idx+1:], (*pb)[idx:])
		(*pb)[idx] = p
	}
	return nil
}

func (pb pendingBlockRecords) searchByHeight(h uint64) (
	pendingBlockRecord, bool) {
	idx := sort.Search(len(pb), func(i int) bool {
		return pb[i].position.Height >= h
	})
	if idx == len(pb) || pb[idx].position.Height != h {
		return pendingBlockRecord{}, false
	}
	return pb[idx], true
}

func (pb pendingBlockRecords) searchByPosition(p types.Position) (
	pendingBlockRecord, bool) {
	idx := sort.Search(len(pb), func(i int) bool {
		return !pb[i].block.Position.Older(p)
	})
	if idx == len(pb) || !pb[idx].position.Equal(p) {
		return pendingBlockRecord{}, false
	}
	return pb[idx], true
}

type blockChainConfig struct {
	roundBasedConfig

	minBlockInterval time.Duration
}

func (c *blockChainConfig) fromConfig(round uint64, config *types.Config) {
	c.minBlockInterval = config.MinBlockInterval
	c.setupRoundBasedFields(round, config)
}

func newBlockChainConfig(prev blockChainConfig, config *types.Config) (
	c blockChainConfig) {
	c = blockChainConfig{}
	c.fromConfig(prev.roundID+1, config)
	c.setRoundBeginHeight(prev.roundEndHeight)
	return
}

type tsigVerifierGetter interface {
	UpdateAndGet(uint64) (TSigVerifier, bool, error)
}

type blockChain struct {
	lock                sync.RWMutex
	ID                  types.NodeID
	lastConfirmed       *types.Block
	lastDelivered       *types.Block
	signer              *utils.Signer
	vGetter             tsigVerifierGetter
	app                 Application
	logger              common.Logger
	pendingRandomnesses map[types.Position]*types.BlockRandomnessResult
	configs             []blockChainConfig
	pendingBlocks       pendingBlockRecords
	confirmedBlocks     types.BlocksByPosition
	dMoment             time.Time
}

func newBlockChain(nID types.NodeID, dMoment time.Time, initBlock *types.Block,
	initConfig blockChainConfig, app Application, vGetter tsigVerifierGetter,
	signer *utils.Signer, logger common.Logger) *blockChain {
	if initBlock != nil {
		if initConfig.roundID != initBlock.Position.Round {
			panic(fmt.Errorf("incompatible config/block %s %d",
				initBlock, initConfig.roundID))
		}
	} else {
		if initConfig.roundID != 0 {
			panic(fmt.Errorf("genesis config should from round 0 %d",
				initConfig.roundID))
		}
	}
	return &blockChain{
		ID:            nID,
		lastConfirmed: initBlock,
		lastDelivered: initBlock,
		signer:        signer,
		vGetter:       vGetter,
		app:           app,
		logger:        logger,
		configs:       []blockChainConfig{initConfig},
		dMoment:       dMoment,
		pendingRandomnesses: make(
			map[types.Position]*types.BlockRandomnessResult),
	}
}

func (bc *blockChain) appendConfig(round uint64, config *types.Config) error {
	expectedRound := uint64(len(bc.configs))
	if bc.lastConfirmed != nil {
		expectedRound += bc.lastConfirmed.Position.Round
	}
	if round != expectedRound {
		return ErrRoundNotIncreasing
	}
	bc.configs = append(bc.configs, newBlockChainConfig(
		bc.configs[len(bc.configs)-1], config))
	return nil
}

func (bc *blockChain) proposeBlock(position types.Position,
	proposeTime time.Time) (b *types.Block, err error) {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	return bc.prepareBlock(position, proposeTime, false)
}

func (bc *blockChain) extractBlocks() (ret []*types.Block) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	for len(bc.confirmedBlocks) > 0 {
		c := bc.confirmedBlocks[0]
		if c.Position.Round > 0 && len(c.Finalization.Randomness) == 0 {
			break
		}
		c, bc.confirmedBlocks = bc.confirmedBlocks[0], bc.confirmedBlocks[1:]
		// TODO(mission): remove these duplicated field if we fully converted
		//                to single chain.
		c.Finalization.ParentHash = c.ParentHash
		c.Finalization.Timestamp = c.Timestamp
		// It's a workaround, the height for application is one-based.
		c.Finalization.Height = c.Position.Height + 1
		ret = append(ret, c)
		bc.lastDelivered = c
	}
	return
}

func (bc *blockChain) sanityCheck(b *types.Block) error {
	if b.IsEmpty() {
		panic(fmt.Errorf("pass empty block to sanity check: %s", b))
	}
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	if bc.lastConfirmed == nil {
		// It should be a genesis block.
		if !b.IsGenesis() {
			return ErrNotGenesisBlock
		}
		// TODO(mission): Do we have to check timestamp of genesis block?
		return nil
	}
	if b.IsGenesis() {
		return ErrIsGenesisBlock
	}
	if b.Position.Height != bc.lastConfirmed.Position.Height+1 {
		if b.Position.Height > bc.lastConfirmed.Position.Height {
			return ErrRetrySanityCheckLater
		}
		return ErrInvalidBlockHeight
	}
	tipConfig := bc.tipConfig()
	if tipConfig.isLastBlock(bc.lastConfirmed) {
		if b.Position.Round != bc.lastConfirmed.Position.Round+1 {
			return ErrRoundNotSwitch
		}
	} else {
		if b.Position.Round != bc.lastConfirmed.Position.Round {
			return ErrInvalidRoundID
		}
	}
	if !b.ParentHash.Equal(bc.lastConfirmed.Hash) {
		return ErrIncorrectParentHash
	}
	if err := utils.VerifyBlockSignature(b); err != nil {
		return err
	}
	return nil
}

// addEmptyBlock is called when an empty block is confirmed by BA.
func (bc *blockChain) addEmptyBlock(position types.Position) (
	*types.Block, error) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	add := func() *types.Block {
		emptyB, err := bc.prepareBlock(position, time.Time{}, true)
		if err != nil || emptyB == nil {
			// This helper is expected to be called when an empty block is ready
			// to be confirmed.
			panic(err)
		}
		bc.confirmBlock(emptyB)
		bc.checkIfBlocksConfirmed()
		return emptyB
	}
	if bc.lastConfirmed != nil {
		if !position.Newer(bc.lastConfirmed.Position) {
			bc.logger.Warn("Dropping empty block: older than tip",
				"position", &position,
				"last-confirmed", bc.lastConfirmed)
			return nil, ErrBlockFromOlderPosition
		}
		if bc.lastConfirmed.Position.Height+1 == position.Height {
			return add(), nil
		}
	} else if position.Height == 0 && position.Round == 0 {
		return add(), nil
	}
	bc.addPendingBlockRecord(pendingBlockRecord{position, nil})
	return nil, nil
}

// addBlock should be called when the block is confirmed by BA, we won't perform
// sanity check against this block, it's ok to add block with skipping height.
func (bc *blockChain) addBlock(b *types.Block) error {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	confirmed := false
	if bc.lastConfirmed != nil {
		if !b.Position.Newer(bc.lastConfirmed.Position) {
			bc.logger.Warn("Dropping block: older than tip",
				"block", b, "last-confirmed", bc.lastConfirmed)
			return nil
		}
		if bc.lastConfirmed.Position.Height+1 == b.Position.Height {
			confirmed = true
		}
	} else if b.IsGenesis() {
		confirmed = true
	}
	if !confirmed {
		bc.addPendingBlockRecord(pendingBlockRecord{b.Position, b})
	} else {
		bc.confirmBlock(b)
		bc.checkIfBlocksConfirmed()
	}
	return nil
}

func (bc *blockChain) addRandomness(r *types.BlockRandomnessResult) error {
	if func() bool {
		bc.lock.RLock()
		defer bc.lock.RUnlock()
		if bc.lastDelivered != nil &&
			bc.lastDelivered.Position.Newer(r.Position) {
			return true
		}
		_, exists := bc.pendingRandomnesses[r.Position]
		if exists {
			return true
		}
		b := bc.findPendingBlock(r.Position)
		return b != nil && len(b.Finalization.Randomness) > 0
	}() {
		return nil
	}
	ok, err := bc.verifyRandomness(r.BlockHash, r.Position.Round, r.Randomness)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectBlockRandomnessResult
	}
	bc.lock.Lock()
	defer bc.lock.Unlock()
	if b := bc.findPendingBlock(r.Position); b != nil {
		if !r.BlockHash.Equal(b.Hash) {
			panic(fmt.Errorf("mismathed randomness: %s %s", b, r))
		}
		b.Finalization.Randomness = r.Randomness
	} else {
		bc.pendingRandomnesses[r.Position] = r
	}
	return nil
}

// TODO(mission): remove this method after removing the strong binding between
//                BA and blockchain.
func (bc *blockChain) tipRound() uint64 {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	if bc.lastConfirmed == nil {
		return 0
	}
	offset, tipConfig := uint64(0), bc.tipConfig()
	if tipConfig.isLastBlock(bc.lastConfirmed) {
		offset++
	}
	return bc.lastConfirmed.Position.Round + offset
}

// TODO(mission): the pulling should be done inside of blockchain, then we don't
//                have to expose this method.
func (bc *blockChain) confirmed(h uint64) bool {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	if bc.lastConfirmed != nil && bc.lastConfirmed.Position.Height >= h {
		return true
	}
	r, found := bc.pendingBlocks.searchByHeight(h)
	if !found {
		return false
	}
	return r.block != nil
}

// TODO(mission): this method can be removed after refining the relation between
//                BA and block storage.
func (bc *blockChain) nextBlock() (uint64, time.Time) {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	// It's ok to access tip config directly without checking the existence of
	// lastConfirmed block in the scenario of "nextBlock" method.
	tip, config := bc.lastConfirmed, bc.configs[0]
	if tip == nil {
		return 0, bc.dMoment
	}
	return tip.Position.Height + 1, tip.Timestamp.Add(config.minBlockInterval)
}

func (bc *blockChain) pendingBlocksWithoutRandomness() (hashes common.Hashes) {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	for _, b := range bc.confirmedBlocks {
		if b.Position.Round == 0 || len(b.Finalization.Randomness) > 0 {
			continue
		}
		hashes = append(hashes, b.Hash)
	}
	for _, r := range bc.pendingBlocks {
		if r.position.Round == 0 {
			continue
		}
		if r.block != nil && len(r.block.Finalization.Randomness) == 0 {
			hashes = append(hashes, r.block.Hash)
		}
	}
	return
}

func (bc *blockChain) lastDeliveredBlock() *types.Block {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	return bc.lastDelivered
}

func (bc *blockChain) lastPendingBlock() *types.Block {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	if len(bc.confirmedBlocks) == 0 {
		return nil
	}
	return bc.confirmedBlocks[0]
}

func (bc *blockChain) processFinalizedBlock(b *types.Block) error {
	return bc.addRandomness(&types.BlockRandomnessResult{
		BlockHash:  b.Hash,
		Position:   b.Position,
		Randomness: b.Finalization.Randomness,
	})
}

/////////////////////////////////////////////
//
// internal helpers
//
/////////////////////////////////////////////

// findPendingBlock is a helper to find a block in either pending or confirmed
// state by position.
func (bc *blockChain) findPendingBlock(p types.Position) *types.Block {
	if idx := sort.Search(len(bc.confirmedBlocks), func(i int) bool {
		return !bc.confirmedBlocks[i].Position.Older(p)
	}); idx != len(bc.confirmedBlocks) &&
		bc.confirmedBlocks[idx].Position.Equal(p) {
		return bc.confirmedBlocks[idx]
	}
	pendingRec, _ := bc.pendingBlocks.searchByPosition(p)
	return pendingRec.block
}

func (bc *blockChain) addPendingBlockRecord(p pendingBlockRecord) {
	if err := bc.pendingBlocks.insert(p); err != nil {
		panic(err)
	}
	if p.block != nil {
		bc.setRandomnessFromPending(p.block)
	}
}

func (bc *blockChain) checkIfBlocksConfirmed() {
	var err error
	for len(bc.pendingBlocks) > 0 {
		if bc.pendingBlocks[0].position.Height <
			bc.lastConfirmed.Position.Height+1 {
			panic(fmt.Errorf("unexpected case %s %s", bc.lastConfirmed,
				bc.pendingBlocks[0].position))
		}
		if bc.pendingBlocks[0].position.Height >
			bc.lastConfirmed.Position.Height+1 {
			break
		}
		var pending pendingBlockRecord
		pending, bc.pendingBlocks = bc.pendingBlocks[0], bc.pendingBlocks[1:]
		nextTip := pending.block
		if nextTip == nil {
			if nextTip, err = bc.prepareBlock(
				pending.position, time.Time{}, true); err != nil {
				// It should not be error when prepare empty block for correct
				// position.
				panic(err)
			}
		}
		bc.confirmBlock(nextTip)
	}
}

func (bc *blockChain) purgeConfig() {
	for bc.configs[0].roundID < bc.lastConfirmed.Position.Round {
		bc.configs = bc.configs[1:]
	}
	if bc.configs[0].roundID != bc.lastConfirmed.Position.Round {
		panic(fmt.Errorf("mismatched tip config: %d %d",
			bc.configs[0].roundID, bc.lastConfirmed.Position.Round))
	}
}

func (bc *blockChain) verifyRandomness(
	blockHash common.Hash, round uint64, randomness []byte) (bool, error) {
	if round == 0 {
		return len(randomness) == 0, nil
	}
	v, ok, err := bc.vGetter.UpdateAndGet(round)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrTSigNotReady
	}
	return v.VerifySignature(blockHash, crypto.Signature{
		Type:      "bls",
		Signature: randomness}), nil
}

func (bc *blockChain) prepareBlock(position types.Position,
	proposeTime time.Time, empty bool) (b *types.Block, err error) {
	// TODO(mission): refine timestamp.
	b = &types.Block{Position: position, Timestamp: proposeTime}
	tip := bc.lastConfirmed
	// Make sure we can propose a block at expected position for callers.
	if tip == nil {
		// The case for genesis block.
		if !position.Equal(types.Position{}) {
			b, err = nil, ErrNotGenesisBlock
			return
		} else if empty {
			b.Timestamp = bc.dMoment
		}
	} else {
		tipConfig := bc.tipConfig()
		if tip.Position.Height+1 != position.Height {
			b, err = nil, ErrNotFollowTipPosition
			return
		}
		if tipConfig.isLastBlock(tip) {
			if tip.Position.Round+1 != position.Round {
				b, err = nil, ErrRoundNotSwitch
				return
			}
		} else {
			if tip.Position.Round != position.Round {
				b, err = nil, ErrInvalidRoundID
				return
			}
		}
		b.ParentHash = tip.Hash
		if !empty {
			bc.logger.Debug("Calling Application.PreparePayload",
				"position", b.Position)
			if b.Payload, err = bc.app.PreparePayload(b.Position); err != nil {
				return
			}
			bc.logger.Debug("Calling Application.PrepareWitness",
				"height", tip.Witness.Height)
			if b.Witness, err = bc.app.PrepareWitness(
				tip.Witness.Height); err != nil {
				return
			}
			if !b.Timestamp.After(tip.Timestamp) {
				b.Timestamp = tip.Timestamp.Add(tipConfig.minBlockInterval)
			}
		} else {
			b.Witness.Height = tip.Witness.Height
			b.Witness.Data = make([]byte, len(tip.Witness.Data))
			copy(b.Witness.Data, tip.Witness.Data)
			b.Timestamp = tip.Timestamp.Add(tipConfig.minBlockInterval)
		}
	}
	if empty {
		if b.Hash, err = utils.HashBlock(b); err != nil {
			b = nil
			return
		}
	} else {
		if err = bc.signer.SignBlock(b); err != nil {
			return
		}
	}
	return
}

func (bc *blockChain) tipConfig() blockChainConfig {
	if bc.lastConfirmed == nil {
		panic(fmt.Errorf("attempting to access config without tip"))
	}
	if bc.lastConfirmed.Position.Round != bc.configs[0].roundID {
		panic(fmt.Errorf("inconsist config and tip: %d %d",
			bc.lastConfirmed.Position.Round, bc.configs[0].roundID))
	}
	return bc.configs[0]
}

func (bc *blockChain) confirmBlock(b *types.Block) {
	if bc.lastConfirmed != nil &&
		bc.lastConfirmed.Position.Height+1 != b.Position.Height {
		panic(fmt.Errorf("confirmed blocks not continuous in height: %s %s",
			bc.lastConfirmed, b))
	}
	bc.logger.Debug("Calling Application.BlockConfirmed", "block", b)
	bc.app.BlockConfirmed(*b)
	bc.lastConfirmed = b
	bc.setRandomnessFromPending(b)
	bc.confirmedBlocks = append(bc.confirmedBlocks, b)
	bc.purgeConfig()
}

func (bc *blockChain) setRandomnessFromPending(b *types.Block) {
	if r, exist := bc.pendingRandomnesses[b.Position]; exist {
		if !r.BlockHash.Equal(b.Hash) {
			panic(fmt.Errorf("mismathed randomness: %s %s", b, r))
		}
		b.Finalization.Randomness = r.Randomness
		delete(bc.pendingRandomnesses, b.Position)
	}
}
