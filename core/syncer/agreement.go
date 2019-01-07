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

package syncer

import (
	"context"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Struct agreement implements struct of BA (Byzantine Agreement) protocol
// needed in syncer, which only receives agreement results.
type agreement struct {
	cache            *utils.NodeSetCache
	inputChan        chan interface{}
	outputChan       chan<- *types.Block
	pullChan         chan<- common.Hash
	blocks           map[types.Position]map[common.Hash]*types.Block
	agreementResults map[common.Hash]struct{}
	latestCRSRound   uint64
	pendings         map[uint64]map[common.Hash]*types.AgreementResult
	logger           common.Logger
	confirmedBlocks  map[common.Hash]struct{}
	ctx              context.Context
	ctxCancel        context.CancelFunc
}

// newAgreement creates a new agreement instance.
func newAgreement(ch chan<- *types.Block, pullChan chan<- common.Hash,
	cache *utils.NodeSetCache, logger common.Logger) *agreement {
	a := &agreement{
		cache:            cache,
		inputChan:        make(chan interface{}, 1000),
		outputChan:       ch,
		pullChan:         pullChan,
		blocks:           make(map[types.Position]map[common.Hash]*types.Block),
		agreementResults: make(map[common.Hash]struct{}),
		logger:           logger,
		pendings: make(
			map[uint64]map[common.Hash]*types.AgreementResult),
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
				a.processBlock(v)
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
	if _, exist := a.agreementResults[b.Hash]; exist {
		a.confirm(b)
	} else {
		if _, exist := a.blocks[b.Position]; !exist {
			a.blocks[b.Position] = make(map[common.Hash]*types.Block)
		}
		a.blocks[b.Position][b.Hash] = b
	}
}

func (a *agreement) processAgreementResult(r *types.AgreementResult) {
	// Cache those results that CRS is not ready yet.
	if _, exists := a.confirmedBlocks[r.BlockHash]; exists {
		a.logger.Trace("agreement result already confirmed", "result", r)
		return
	}
	if r.Position.Round > a.latestCRSRound {
		pendingsForRound, exists := a.pendings[r.Position.Round]
		if !exists {
			pendingsForRound = make(map[common.Hash]*types.AgreementResult)
			a.pendings[r.Position.Round] = pendingsForRound
		}
		pendingsForRound[r.BlockHash] = r
		a.logger.Trace("agreement result cached", "result", r)
		return
	}
	if err := core.VerifyAgreementResult(r, a.cache); err != nil {
		a.logger.Error("agreement result verification failed",
			"result", r,
			"error", err)
		return
	}
	if r.IsEmptyBlock {
		b := &types.Block{
			Position: r.Position,
		}
		// Empty blocks should be confirmed directly, they won't be sent over
		// the wire.
		a.confirm(b)
		return
	}
	if bs, exist := a.blocks[r.Position]; exist {
		if b, exist := bs[r.BlockHash]; exist {
			a.confirm(b)
			return
		}
	}
	a.agreementResults[r.BlockHash] = struct{}{}
loop:
	for {
		select {
		case a.pullChan <- r.BlockHash:
			break loop
		case <-a.ctx.Done():
			a.logger.Error("pull request is not sent",
				"position", &r.Position,
				"hash", r.BlockHash.String()[:6])
			return
		case <-time.After(500 * time.Millisecond):
			a.logger.Debug("pull request is unable to send",
				"position", &r.Position,
				"hash", r.BlockHash.String()[:6])
		}
	}
}

func (a *agreement) processNewCRS(round uint64) {
	if round <= a.latestCRSRound {
		return
	}
	// Verify all pending results.
	for r := a.latestCRSRound + 1; r <= round; r++ {
		pendingsForRound := a.pendings[r]
		if pendingsForRound == nil {
			continue
		}
		delete(a.pendings, r)
		for _, res := range pendingsForRound {
			if err := core.VerifyAgreementResult(res, a.cache); err != nil {
				a.logger.Error("invalid agreement result", "result", res)
				continue
			}
			a.logger.Error("flush agreement result", "result", res)
			a.processAgreementResult(res)
			break
		}
	}
	a.latestCRSRound = round
}

// confirm notifies consensus the confirmation of a block in BA.
func (a *agreement) confirm(b *types.Block) {
	if _, exist := a.confirmedBlocks[b.Hash]; !exist {
		delete(a.blocks, b.Position)
		delete(a.agreementResults, b.Hash)
	loop:
		for {
			select {
			case a.outputChan <- b:
				break loop
			case <-a.ctx.Done():
				a.logger.Error("confirmed block is not sent", "block", b)
				return
			case <-time.After(500 * time.Millisecond):
				a.logger.Debug("agreement output channel is full", "block", b)
			}
		}
		a.confirmedBlocks[b.Hash] = struct{}{}
	}
}
