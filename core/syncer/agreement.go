// Copyright 2018 The dexon-consensus Authors
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

package syncer

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Struct agreement implements struct of BA (Byzantine Agreement) protocol
// needed in syncer, which only receives agreement results.
type agreement struct {
	chainTip          uint64
	cache             *utils.NodeSetCache
	tsigVerifierCache *core.TSigVerifierCache
	inputChan         chan interface{}
	outputChan        chan<- *types.Block
	pullChan          chan<- common.Hash
	blocks            map[types.Position]map[common.Hash]*types.Block
	agreementResults  map[common.Hash][]byte
	latestCRSRound    uint64
	pendingAgrs       map[uint64]map[common.Hash]*types.AgreementResult
	pendingBlocks     map[uint64]map[common.Hash]*types.Block
	logger            common.Logger
	confirmedBlocks   map[common.Hash]struct{}
	ctx               context.Context
	ctxCancel         context.CancelFunc
}

// newAgreement creates a new agreement instance.
func newAgreement(chainTip uint64,
	ch chan<- *types.Block, pullChan chan<- common.Hash,
	cache *utils.NodeSetCache, verifier *core.TSigVerifierCache,
	logger common.Logger) *agreement {
	a := &agreement{
		chainTip:          chainTip,
		cache:             cache,
		tsigVerifierCache: verifier,
		inputChan:         make(chan interface{}, 1000),
		outputChan:        ch,
		pullChan:          pullChan,
		blocks:            make(map[types.Position]map[common.Hash]*types.Block),
		agreementResults:  make(map[common.Hash][]byte),
		logger:            logger,
		pendingAgrs: make(
			map[uint64]map[common.Hash]*types.AgreementResult),
		pendingBlocks: make(
			map[uint64]map[common.Hash]*types.Block),
		confirmedBlocks: make(map[common.Hash]struct{}),
	}
	a.ctx, a.ctxCancel = context.WithCancel(context.Background())
	return a
}

// run starts the agreement, this does not start a new routine, go a new
// routine explicitly in the caller.
func (a *agreement) run() {
	defer a.ctxCancel()
	for {
		select {
		case val, ok := <-a.inputChan:
			if !ok {
				// InputChan is closed by network when network ends.
				return
			}
			switch v := val.(type) {
			case *types.Block:
				if v.Position.Round >= core.DKGDelayRound && v.IsFinalized() {
					a.processFinalizedBlock(v)
				} else {
					a.processBlock(v)
				}
			case *types.AgreementResult:
				a.processAgreementResult(v)
			case uint64:
				a.processNewCRS(v)
			}
		}
	}
}

func (a *agreement) processBlock(b *types.Block) {
	if _, exist := a.confirmedBlocks[b.Hash]; exist {
		return
	}
	if rand, exist := a.agreementResults[b.Hash]; exist {
		if len(b.Randomness) == 0 {
			b.Randomness = rand
		}
		a.confirm(b)
	} else {
		if _, exist := a.blocks[b.Position]; !exist {
			a.blocks[b.Position] = make(map[common.Hash]*types.Block)
		}
		a.blocks[b.Position][b.Hash] = b
	}
}

func (a *agreement) processFinalizedBlock(block *types.Block) {
	// Cache those results that CRS is not ready yet.
	if _, exists := a.confirmedBlocks[block.Hash]; exists {
		a.logger.Trace("finalized block already confirmed", "block", block)
		return
	}
	if block.Position.Round > a.latestCRSRound {
		pendingsForRound, exists := a.pendingBlocks[block.Position.Round]
		if !exists {
			pendingsForRound = make(map[common.Hash]*types.Block)
			a.pendingBlocks[block.Position.Round] = pendingsForRound
		}
		pendingsForRound[block.Hash] = block
		a.logger.Trace("finalized block cached", "block", block)
		return
	}
	if err := utils.VerifyBlockSignature(block); err != nil {
		return
	}
	verifier, ok, err := a.tsigVerifierCache.UpdateAndGet(
		block.Position.Round)
	if err != nil {
		a.logger.Error("error verifying block randomness",
			"block", block,
			"error", err)
		return
	}
	if !ok {
		a.logger.Error("cannot verify block randomness", "block", block)
		return
	}
	if !verifier.VerifySignature(block.Hash, crypto.Signature{
		Type:      "bls",
		Signature: block.Randomness,
	}) {
		a.logger.Error("incorrect block randomness", "block", block)
		return
	}
	a.confirm(block)
}

func (a *agreement) processAgreementResult(r *types.AgreementResult) {
	// Cache those results that CRS is not ready yet.
	if _, exists := a.confirmedBlocks[r.BlockHash]; exists {
		a.logger.Trace("Agreement result already confirmed", "result", r)
		return
	}
	if r.Position.Round > a.latestCRSRound {
		pendingsForRound, exists := a.pendingAgrs[r.Position.Round]
		if !exists {
			pendingsForRound = make(map[common.Hash]*types.AgreementResult)
			a.pendingAgrs[r.Position.Round] = pendingsForRound
		}
		pendingsForRound[r.BlockHash] = r
		a.logger.Trace("Agreement result cached", "result", r)
		return
	}
	if err := core.VerifyAgreementResult(r, a.cache); err != nil {
		a.logger.Error("Agreement result verification failed",
			"result", r,
			"error", err)
		return
	}
	if r.Position.Round >= core.DKGDelayRound {
		verifier, ok, err := a.tsigVerifierCache.UpdateAndGet(r.Position.Round)
		if err != nil {
			a.logger.Error("error verifying agreement result randomness",
				"result", r,
				"error", err)
			return
		}
		if !ok {
			a.logger.Error("cannot verify agreement result randomness", "result", r)
			return
		}
		if !verifier.VerifySignature(r.BlockHash, crypto.Signature{
			Type:      "bls",
			Signature: r.Randomness,
		}) {
			a.logger.Error("incorrect agreement result randomness", "result", r)
			return
		}
	} else {
		// Special case for rounds before DKGDelayRound.
		if bytes.Compare(r.Randomness, core.NoRand) != 0 {
			a.logger.Error("incorrect agreement result randomness", "result", r)
			return
		}
	}
	if r.IsEmptyBlock {
		b := &types.Block{
			Position:   r.Position,
			Randomness: r.Randomness,
		}
		// Empty blocks should be confirmed directly, they won't be sent over
		// the wire.
		a.confirm(b)
		return
	}
	if bs, exist := a.blocks[r.Position]; exist {
		if b, exist := bs[r.BlockHash]; exist {
			b.Randomness = r.Randomness
			a.confirm(b)
			return
		}
	}
	a.agreementResults[r.BlockHash] = r.Randomness
loop:
	for {
		select {
		case a.pullChan <- r.BlockHash:
			break loop
		case <-a.ctx.Done():
			a.logger.Error("Pull request is not sent",
				"position", &r.Position,
				"hash", r.BlockHash.String()[:6])
			return
		case <-time.After(500 * time.Millisecond):
			a.logger.Debug("Pull request is unable to send",
				"position", &r.Position,
				"hash", r.BlockHash.String()[:6])
		}
	}
}

func (a *agreement) processNewCRS(round uint64) {
	if round <= a.latestCRSRound {
		return
	}
	prevRound := a.latestCRSRound + 1
	a.latestCRSRound = round
	// Verify all pending results.
	for r := prevRound; r <= a.latestCRSRound; r++ {
		pendingsForRound := a.pendingAgrs[r]
		if pendingsForRound == nil {
			continue
		}
		delete(a.pendingAgrs, r)
		for _, res := range pendingsForRound {
			if err := core.VerifyAgreementResult(res, a.cache); err != nil {
				a.logger.Error("Invalid agreement result",
					"result", res,
					"error", err)
				continue
			}
			a.logger.Error("Flush agreement result", "result", res)
			a.processAgreementResult(res)
			break
		}
	}
}

// confirm notifies consensus the confirmation of a block in BA.
func (a *agreement) confirm(b *types.Block) {
	if !b.IsFinalized() {
		panic(fmt.Errorf("confirm a block %s without randomness", b))
	}
	if _, exist := a.confirmedBlocks[b.Hash]; !exist {
		delete(a.blocks, b.Position)
		delete(a.agreementResults, b.Hash)
	loop:
		for {
			select {
			case a.outputChan <- b:
				break loop
			case <-a.ctx.Done():
				a.logger.Error("Confirmed block is not sent", "block", b)
				return
			case <-time.After(500 * time.Millisecond):
				a.logger.Debug("Agreement output channel is full", "block", b)
			}
		}
		a.confirmedBlocks[b.Hash] = struct{}{}
	}
	if b.Position.Height > a.chainTip+1 {
		if _, exist := a.confirmedBlocks[b.ParentHash]; !exist {
			a.pullChan <- b.ParentHash
		}
	}
}
