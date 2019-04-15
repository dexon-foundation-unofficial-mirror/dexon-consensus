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
	"bytes"
	"errors"
	"fmt"
	"math"
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
	ErrBlockFromOlderPosition   = errors.New("block from older position")
	ErrNotGenesisBlock          = errors.New("not a genesis block")
	ErrIsGenesisBlock           = errors.New("is a genesis block")
	ErrIncorrectParentHash      = errors.New("incorrect parent hash")
	ErrInvalidBlockHeight       = errors.New("invalid block height")
	ErrInvalidRoundID           = errors.New("invalid round id")
	ErrInvalidTimestamp         = errors.New("invalid timestamp")
	ErrNotFollowTipPosition     = errors.New("not follow tip position")
	ErrDuplicatedPendingBlock   = errors.New("duplicated pending block")
	ErrRetrySanityCheckLater    = errors.New("retry sanity check later")
	ErrRoundNotSwitch           = errors.New("round not switch")
	ErrIncorrectAgreementResult = errors.New(
		"incorrect block randomness result")
	ErrMissingRandomness = errors.New("missing block randomness")
)

const notReadyHeight uint64 = math.MaxUint64

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
			// Allow to overwrite pending block record for empty blocks, we may
			// need to pull that block from others when its parent is not found
			// locally.
			if (*pb)[idx].block == nil && p.block != nil {
				(*pb)[idx].block = p.block
				return nil
			}
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
	utils.RoundBasedConfig

	minBlockInterval time.Duration
}

func (c *blockChainConfig) fromConfig(round uint64, config *types.Config) {
	c.minBlockInterval = config.MinBlockInterval
	c.SetupRoundBasedFields(round, config)
}

func newBlockChainConfig(prev blockChainConfig, config *types.Config) (
	c blockChainConfig) {
	c = blockChainConfig{}
	c.fromConfig(prev.RoundID()+1, config)
	c.AppendTo(prev.RoundBasedConfig)
	return
}

type tsigVerifierGetter interface {
	UpdateAndGet(uint64) (TSigVerifier, bool, error)
	Purge(uint64)
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
	pendingRandomnesses map[types.Position][]byte
	configs             []blockChainConfig
	pendingBlocks       pendingBlockRecords
	confirmedBlocks     types.BlocksByPosition
	dMoment             time.Time

	// Do not access this variable besides processAgreementResult.
	lastPosition types.Position
}

func newBlockChain(nID types.NodeID, dMoment time.Time, initBlock *types.Block,
	app Application, vGetter tsigVerifierGetter, signer *utils.Signer,
	logger common.Logger) *blockChain {
	return &blockChain{
		ID:            nID,
		lastConfirmed: initBlock,
		lastDelivered: initBlock,
		signer:        signer,
		vGetter:       vGetter,
		app:           app,
		logger:        logger,
		dMoment:       dMoment,
		pendingRandomnesses: make(
			map[types.Position][]byte),
	}
}

func (bc *blockChain) notifyRoundEvents(evts []utils.RoundEventParam) error {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	apply := func(e utils.RoundEventParam) error {
		if len(bc.configs) > 0 {
			lastCfg := bc.configs[len(bc.configs)-1]
			if e.BeginHeight != lastCfg.RoundEndHeight() {
				return ErrInvalidBlockHeight
			}
			if lastCfg.RoundID() == e.Round {
				bc.configs[len(bc.configs)-1].ExtendLength()
			} else if lastCfg.RoundID()+1 == e.Round {
				bc.configs = append(bc.configs, newBlockChainConfig(
					lastCfg, e.Config))
			} else {
				return ErrInvalidRoundID
			}
		} else {
			c := blockChainConfig{}
			c.fromConfig(e.Round, e.Config)
			c.SetRoundBeginHeight(e.BeginHeight)
			if bc.lastConfirmed == nil {
				if c.RoundID() != 0 {
					panic(fmt.Errorf(
						"genesis config should from round 0, but %d",
						c.RoundID()))
				}
			} else {
				if c.RoundID() != bc.lastConfirmed.Position.Round {
					panic(fmt.Errorf("incompatible config/block round %s %d",
						bc.lastConfirmed, c.RoundID()))
				}
				if !c.Contains(bc.lastConfirmed.Position.Height) {
					panic(fmt.Errorf(
						"unmatched round-event with block %s %d %d %d",
						bc.lastConfirmed, e.Round, e.Reset, e.BeginHeight))
				}
			}
			bc.configs = append(bc.configs, c)
		}
		if e.Reset != 0 {
			bc.vGetter.Purge(e.Round + 1)
		}
		return nil
	}
	for _, e := range evts {
		if err := apply(e); err != nil {
			return err
		}
	}
	return nil
}

func (bc *blockChain) proposeBlock(position types.Position,
	proposeTime time.Time, isEmpty bool) (b *types.Block, err error) {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	return bc.prepareBlock(position, proposeTime, isEmpty)
}

func (bc *blockChain) extractBlocks() (ret []*types.Block) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	for len(bc.confirmedBlocks) > 0 {
		c := bc.confirmedBlocks[0]
		if c.Position.Round >= DKGDelayRound &&
			len(c.Randomness) == 0 &&
			!bc.setRandomnessFromPending(c) {
			break
		}
		c, bc.confirmedBlocks = bc.confirmedBlocks[0], bc.confirmedBlocks[1:]
		ret = append(ret, c)
		bc.lastDelivered = c
	}
	return
}

func (bc *blockChain) sanityCheck(b *types.Block) error {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	if bc.lastConfirmed == nil {
		// It should be a genesis block.
		if !b.IsGenesis() {
			return ErrNotGenesisBlock
		}
		if b.Timestamp.Before(bc.dMoment.Add(bc.configs[0].minBlockInterval)) {
			return ErrInvalidTimestamp
		}
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
	if tipConfig.IsLastBlock(bc.lastConfirmed) {
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
	if b.Timestamp.Before(bc.lastConfirmed.Timestamp.Add(
		tipConfig.minBlockInterval)) {
		return ErrInvalidTimestamp
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
	} else if position.Height == types.GenesisHeight && position.Round == 0 {
		return add(), nil
	} else {
		return nil, ErrInvalidBlockHeight
	}
	return nil, bc.addPendingBlockRecord(pendingBlockRecord{position, nil})
}

// addBlock should be called when the block is confirmed by BA, we won't perform
// sanity check against this block, it's ok to add block with skipping height.
func (bc *blockChain) addBlock(b *types.Block) error {
	if b.Position.Round >= DKGDelayRound &&
		len(b.Randomness) == 0 &&
		!bc.setRandomnessFromPending(b) {
		return ErrMissingRandomness
	}
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
	delete(bc.pendingRandomnesses, b.Position)
	if !confirmed {
		return bc.addPendingBlockRecord(pendingBlockRecord{b.Position, b})
	}
	bc.confirmBlock(b)
	bc.checkIfBlocksConfirmed()
	return nil
}

func (bc *blockChain) tipRound() uint64 {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	if bc.lastConfirmed == nil {
		return 0
	}
	offset, tipConfig := uint64(0), bc.tipConfig()
	if tipConfig.IsLastBlock(bc.lastConfirmed) {
		offset++
	}
	return bc.lastConfirmed.Position.Round + offset
}

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

func (bc *blockChain) nextBlock() (uint64, time.Time) {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	// It's ok to access tip config directly without checking the existence of
	// lastConfirmed block in the scenario of "nextBlock" method.
	tip, config := bc.lastConfirmed, bc.configs[0]
	if tip == nil {
		return types.GenesisHeight, bc.dMoment
	}
	if tip != bc.lastDelivered {
		// If tip is not delivered, we should not proceed to next block.
		return notReadyHeight, time.Time{}
	}
	return tip.Position.Height + 1, tip.Timestamp.Add(config.minBlockInterval)
}

func (bc *blockChain) pendingBlocksWithoutRandomness() []*types.Block {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	blocks := make([]*types.Block, 0)
	for _, b := range bc.confirmedBlocks {
		if b.Position.Round < DKGDelayRound ||
			len(b.Randomness) > 0 ||
			bc.setRandomnessFromPending(b) {
			continue
		}
		blocks = append(blocks, b)
	}
	for _, r := range bc.pendingBlocks {
		if r.position.Round < DKGDelayRound {
			continue
		}
		if r.block != nil &&
			len(r.block.Randomness) == 0 &&
			!bc.setRandomnessFromPending(r.block) {
			blocks = append(blocks, r.block)
		}
	}
	return blocks
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

func (bc *blockChain) addPendingBlockRecord(p pendingBlockRecord) error {
	if err := bc.pendingBlocks.insert(p); err != nil {
		if err == ErrDuplicatedPendingBlock {
			// We need to ignore this error because BA might confirm duplicated
			// blocks in position.
			err = nil
		}
		return err
	}
	return nil
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
	for bc.configs[0].RoundID() < bc.lastConfirmed.Position.Round {
		bc.configs = bc.configs[1:]
	}
	if bc.configs[0].RoundID() != bc.lastConfirmed.Position.Round {
		panic(fmt.Errorf("mismatched tip config: %d %d",
			bc.configs[0].RoundID(), bc.lastConfirmed.Position.Round))
	}
}

func (bc *blockChain) verifyRandomness(
	blockHash common.Hash, round uint64, randomness []byte) (bool, error) {
	if round < DKGDelayRound {
		return bytes.Compare(randomness, NoRand) == 0, nil
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
	b = &types.Block{Position: position, Timestamp: proposeTime}
	tip := bc.lastConfirmed
	// Make sure we can propose a block at expected position for callers.
	if tip == nil {
		if bc.configs[0].RoundID() != uint64(0) {
			panic(fmt.Errorf(
				"Genesis config should be ready when preparing genesis: %d",
				bc.configs[0].RoundID()))
		}
		// It should be the case for genesis block.
		if !position.Equal(types.Position{Height: types.GenesisHeight}) {
			b, err = nil, ErrNotGenesisBlock
			return
		}
		minExpectedTime := bc.dMoment.Add(bc.configs[0].minBlockInterval)
		if empty {
			b.Timestamp = minExpectedTime
		} else {
			bc.logger.Debug("Calling genesis Application.PreparePayload")
			if b.Payload, err = bc.app.PreparePayload(b.Position); err != nil {
				b = nil
				return
			}
			bc.logger.Debug("Calling genesis Application.PrepareWitness")
			if b.Witness, err = bc.app.PrepareWitness(0); err != nil {
				b = nil
				return
			}
			if proposeTime.Before(minExpectedTime) {
				b.Timestamp = minExpectedTime
			}
		}
	} else {
		tipConfig := bc.tipConfig()
		if tip.Position.Height+1 != position.Height {
			b, err = nil, ErrNotFollowTipPosition
			return
		}
		if tipConfig.IsLastBlock(tip) {
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
		minExpectedTime := tip.Timestamp.Add(bc.configs[0].minBlockInterval)
		b.ParentHash = tip.Hash
		if !empty {
			bc.logger.Debug("Calling Application.PreparePayload",
				"position", b.Position)
			if b.Payload, err = bc.app.PreparePayload(b.Position); err != nil {
				b = nil
				return
			}
			bc.logger.Debug("Calling Application.PrepareWitness",
				"height", tip.Witness.Height)
			if b.Witness, err = bc.app.PrepareWitness(
				tip.Witness.Height); err != nil {
				b = nil
				return
			}
			if b.Timestamp.Before(minExpectedTime) {
				b.Timestamp = minExpectedTime
			}
		} else {
			b.Witness.Height = tip.Witness.Height
			b.Witness.Data = make([]byte, len(tip.Witness.Data))
			copy(b.Witness.Data, tip.Witness.Data)
			b.Timestamp = minExpectedTime
		}
	}
	if empty {
		if b.Hash, err = utils.HashBlock(b); err != nil {
			b = nil
			return
		}
	} else {
		if err = bc.signer.SignBlock(b); err != nil {
			b = nil
			return
		}
	}
	return
}

func (bc *blockChain) tipConfig() blockChainConfig {
	if bc.lastConfirmed == nil {
		panic(fmt.Errorf("attempting to access config without tip"))
	}
	if bc.lastConfirmed.Position.Round != bc.configs[0].RoundID() {
		panic(fmt.Errorf("inconsist config and tip: %d %d",
			bc.lastConfirmed.Position.Round, bc.configs[0].RoundID()))
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
	bc.confirmedBlocks = append(bc.confirmedBlocks, b)
	bc.purgeConfig()
}

func (bc *blockChain) setRandomnessFromPending(b *types.Block) bool {
	if r, exist := bc.pendingRandomnesses[b.Position]; exist {
		b.Randomness = r
		delete(bc.pendingRandomnesses, b.Position)
		return true
	}
	return false
}

func (bc *blockChain) processAgreementResult(result *types.AgreementResult) error {
	if result.Position.Round < DKGDelayRound {
		return nil
	}
	if !result.Position.Newer(bc.lastPosition) {
		return ErrSkipButNoError
	}
	ok, err := bc.verifyRandomness(
		result.BlockHash, result.Position.Round, result.Randomness)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectAgreementResult
	}
	bc.lock.Lock()
	defer bc.lock.Unlock()
	if !result.Position.Newer(bc.lastDelivered.Position) {
		return nil
	}
	bc.pendingRandomnesses[result.Position] = result.Randomness
	bc.lastPosition = bc.lastDelivered.Position
	return nil
}

func (bc *blockChain) addBlockRandomness(pos types.Position, rand []byte) {
	if pos.Round < DKGDelayRound {
		return
	}
	bc.lock.Lock()
	defer bc.lock.Unlock()
	if !pos.Newer(bc.lastDelivered.Position) {
		return
	}
	bc.pendingRandomnesses[pos] = rand
}
