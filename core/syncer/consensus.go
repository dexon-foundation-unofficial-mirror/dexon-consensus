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

package syncer

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

var (
	// ErrAlreadySynced is reported when syncer is synced.
	ErrAlreadySynced = fmt.Errorf("already synced")
	// ErrNotSynced is reported when syncer is not synced yet.
	ErrNotSynced = fmt.Errorf("not synced yet")
	// ErrGenesisBlockReached is reported when genesis block reached.
	ErrGenesisBlockReached = fmt.Errorf("genesis block reached")
	// ErrInvalidBlockOrder is reported when SyncBlocks receives unordered
	// blocks.
	ErrInvalidBlockOrder = fmt.Errorf("invalid block order")
	// ErrInvalidSyncingHeight raised when the blocks to sync is not following
	// the compaction chain tip in database.
	ErrInvalidSyncingHeight = fmt.Errorf("invalid syncing height")
)

// Consensus is for syncing consensus module.
type Consensus struct {
	db           db.Database
	gov          core.Governance
	dMoment      time.Time
	logger       common.Logger
	app          core.Application
	prv          crypto.PrivateKey
	network      core.Network
	nodeSetCache *utils.NodeSetCache
	tsigVerifier *core.TSigVerifierCache

	blocks            types.BlocksByPosition
	agreementModule   *agreement
	agreementRoundCut uint64
	heightEvt         *common.Event
	roundEvt          *utils.RoundEvent

	// lock for accessing all fields.
	lock               sync.RWMutex
	duringBuffering    bool
	latestCRSRound     uint64
	waitGroup          sync.WaitGroup
	agreementWaitGroup sync.WaitGroup
	pullChan           chan common.Hash
	receiveChan        chan *types.Block
	ctx                context.Context
	ctxCancel          context.CancelFunc
	syncedLastBlock    *types.Block
	syncedConsensus    *core.Consensus
	syncedSkipNext     bool
	dummyCancel        context.CancelFunc
	dummyFinished      <-chan struct{}
	dummyMsgBuffer     []types.Msg
	initChainTipHeight uint64
}

// NewConsensus creates an instance for Consensus (syncer consensus).
func NewConsensus(
	initHeight uint64,
	dMoment time.Time,
	app core.Application,
	gov core.Governance,
	db db.Database,
	network core.Network,
	prv crypto.PrivateKey,
	logger common.Logger) *Consensus {

	con := &Consensus{
		dMoment:      dMoment,
		app:          app,
		gov:          gov,
		db:           db,
		network:      network,
		nodeSetCache: utils.NewNodeSetCache(gov),
		tsigVerifier: core.NewTSigVerifierCache(gov, 7),
		prv:          prv,
		logger:       logger,
		receiveChan:  make(chan *types.Block, 1000),
		pullChan:     make(chan common.Hash, 1000),
		heightEvt:    common.NewEvent(),
	}
	con.ctx, con.ctxCancel = context.WithCancel(context.Background())
	_, con.initChainTipHeight = db.GetCompactionChainTipInfo()
	con.agreementModule = newAgreement(
		con.initChainTipHeight,
		con.receiveChan,
		con.pullChan,
		con.nodeSetCache,
		con.tsigVerifier,
		con.logger)
	con.agreementWaitGroup.Add(1)
	go func() {
		defer con.agreementWaitGroup.Done()
		con.agreementModule.run()
	}()
	if err := con.deliverPendingBlocks(initHeight); err != nil {
		panic(err)
	}
	return con
}

func (con *Consensus) deliverPendingBlocks(height uint64) error {
	if height >= con.initChainTipHeight {
		return nil
	}
	blocks := make([]*types.Block, 0, con.initChainTipHeight-height)
	hash, _ := con.db.GetCompactionChainTipInfo()
	for {
		block, err := con.db.GetBlock(hash)
		if err != nil {
			return err
		}
		if block.Position.Height == height {
			break
		}
		blocks = append(blocks, &block)
		hash = block.ParentHash
	}
	sort.Sort(types.BlocksByPosition(blocks))
	for _, b := range blocks {
		con.logger.Debug("Syncer BlockConfirmed", "block", b)
		con.app.BlockConfirmed(*b)
		con.logger.Debug("Syncer BlockDelivered", "block", b)
		con.app.BlockDelivered(b.Hash, b.Position, b.Randomness)
	}
	return nil
}

func (con *Consensus) assureBuffering() {
	if func() bool {
		con.lock.RLock()
		defer con.lock.RUnlock()
		return con.duringBuffering
	}() {
		return
	}
	con.lock.Lock()
	defer con.lock.Unlock()
	if con.duringBuffering {
		return
	}
	con.duringBuffering = true
	// Get latest block to prepare utils.RoundEvent.
	var (
		err               error
		blockHash, height = con.db.GetCompactionChainTipInfo()
	)
	if height == 0 {
		con.roundEvt, err = utils.NewRoundEvent(con.ctx, con.gov, con.logger,
			types.Position{}, core.ConfigRoundShift)
	} else {
		var b types.Block
		if b, err = con.db.GetBlock(blockHash); err == nil {
			con.roundEvt, err = utils.NewRoundEvent(con.ctx, con.gov,
				con.logger, b.Position, core.ConfigRoundShift)
		}
	}
	if err != nil {
		panic(err)
	}
	// Make sure con.roundEvt stopped before stopping con.agreementModule.
	con.waitGroup.Add(1)
	// Register a round event handler to reset node set cache, this handler
	// should be the highest priority.
	con.roundEvt.Register(func(evts []utils.RoundEventParam) {
		for _, e := range evts {
			if e.Reset == 0 {
				continue
			}
			con.nodeSetCache.Purge(e.Round + 1)
			con.tsigVerifier.Purge(e.Round + 1)
		}
	})
	// Register a round event handler to notify CRS to agreementModule.
	con.roundEvt.Register(func(evts []utils.RoundEventParam) {
		con.waitGroup.Add(1)
		go func() {
			defer con.waitGroup.Done()
			for _, e := range evts {
				select {
				case <-con.ctx.Done():
					return
				default:
				}
				for func() bool {
					select {
					case <-con.ctx.Done():
						return false
					case con.agreementModule.inputChan <- e.Round:
						return false
					case <-time.After(500 * time.Millisecond):
						con.logger.Warn(
							"Agreement input channel is full when notifying new round",
							"round", e.Round,
						)
						return true
					}
				}() {
				}
			}
		}()
	})
	// Register a round event handler to validate next round.
	con.roundEvt.Register(func(evts []utils.RoundEventParam) {
		con.heightEvt.RegisterHeight(
			evts[len(evts)-1].NextRoundValidationHeight(),
			utils.RoundEventRetryHandlerGenerator(con.roundEvt, con.heightEvt),
		)
	})
	con.roundEvt.TriggerInitEvent()
	con.startAgreement()
	con.startNetwork()
}

func (con *Consensus) checkIfSynced(blocks []*types.Block) (synced bool) {
	con.lock.RLock()
	defer con.lock.RUnlock()
	defer func() {
		con.logger.Debug("Syncer synced status",
			"last-block", blocks[len(blocks)-1],
			"synced", synced,
		)
	}()
	if len(con.blocks) == 0 || len(blocks) == 0 {
		return
	}
	synced = !blocks[len(blocks)-1].Position.Older(con.blocks[0].Position)
	return
}

func (con *Consensus) buildAllEmptyBlocks() {
	con.lock.Lock()
	defer con.lock.Unlock()
	// Clean empty blocks on tips of chains.
	for len(con.blocks) > 0 && con.isEmptyBlock(con.blocks[0]) {
		con.blocks = con.blocks[1:]
	}
	// Build empty blocks.
	for i, b := range con.blocks {
		if con.isEmptyBlock(b) {
			if con.blocks[i-1].Position.Height+1 == b.Position.Height {
				con.buildEmptyBlock(b, con.blocks[i-1])
			}
		}
	}
}

// ForceSync forces syncer to become synced.
func (con *Consensus) ForceSync(lastPos types.Position, skip bool) {
	if con.syncedLastBlock != nil {
		return
	}
	hash, height := con.db.GetCompactionChainTipInfo()
	if height < lastPos.Height {
		panic(fmt.Errorf("compaction chain not synced height %d, tip %d",
			lastPos.Height, height))
	} else if height > lastPos.Height {
		skip = false
	}
	block, err := con.db.GetBlock(hash)
	if err != nil {
		panic(err)
	}
	con.syncedLastBlock = &block
	con.stopBuffering()
	// We might call stopBuffering without calling assureBuffering.
	if con.dummyCancel == nil {
		con.dummyCancel, con.dummyFinished = utils.LaunchDummyReceiver(
			context.Background(), con.network.ReceiveChan(),
			func(msg types.Msg) {
				con.dummyMsgBuffer = append(con.dummyMsgBuffer, msg)
			})
	}
	con.syncedSkipNext = skip
	con.logger.Info("Force Sync", "block", &block, "skip", skip)
}

// SyncBlocks syncs blocks from compaction chain, latest is true if the caller
// regards the blocks are the latest ones. Notice that latest can be true for
// many times.
// NOTICE: parameter "blocks" should be consecutive in compaction height.
// NOTICE: this method is not expected to be called concurrently.
func (con *Consensus) SyncBlocks(
	blocks []*types.Block, latest bool) (synced bool, err error) {
	defer func() {
		con.logger.Debug("SyncBlocks returned",
			"synced", synced,
			"error", err,
			"last-block", con.syncedLastBlock,
		)
	}()
	if con.syncedLastBlock != nil {
		synced, err = true, ErrAlreadySynced
		return
	}
	if len(blocks) == 0 {
		return
	}
	// Check if blocks are consecutive.
	for i := 1; i < len(blocks); i++ {
		if blocks[i].Position.Height != blocks[i-1].Position.Height+1 {
			err = ErrInvalidBlockOrder
			return
		}
	}
	// Make sure the first block is the next block of current compaction chain
	// tip in DB.
	_, tipHeight := con.db.GetCompactionChainTipInfo()
	if blocks[0].Position.Height != tipHeight+1 {
		con.logger.Error("Mismatched block height",
			"now", blocks[0].Position.Height,
			"expected", tipHeight+1,
		)
		err = ErrInvalidSyncingHeight
		return
	}
	con.logger.Trace("SyncBlocks",
		"position", &blocks[0].Position,
		"len", len(blocks),
		"latest", latest,
	)
	for _, b := range blocks {
		if err = con.db.PutBlock(*b); err != nil {
			// A block might be put into db when confirmed by BA, but not
			// finalized yet.
			if err == db.ErrBlockExists {
				err = con.db.UpdateBlock(*b)
			}
			if err != nil {
				return
			}
		}
		if err = con.db.PutCompactionChainTipInfo(
			b.Hash, b.Position.Height); err != nil {
			return
		}
		con.heightEvt.NotifyHeight(b.Position.Height)
	}
	if latest {
		con.assureBuffering()
		con.buildAllEmptyBlocks()
		// Check if compaction and agreements' blocks are overlapped. The
		// overlapping of compaction chain and BA's oldest blocks means the
		// syncing is done.
		if con.checkIfSynced(blocks) {
			con.stopBuffering()
			con.syncedLastBlock = blocks[len(blocks)-1]
			synced = true
		}
	}
	return
}

// GetSyncedConsensus returns the core.Consensus instance after synced.
func (con *Consensus) GetSyncedConsensus() (*core.Consensus, error) {
	con.lock.Lock()
	defer con.lock.Unlock()
	if con.syncedConsensus != nil {
		return con.syncedConsensus, nil
	}
	if con.syncedLastBlock == nil {
		return nil, ErrNotSynced
	}
	// flush all blocks in con.blocks into core.Consensus, and build
	// core.Consensus from syncer.
	con.dummyCancel()
	<-con.dummyFinished
	var err error
	con.syncedConsensus, err = core.NewConsensusFromSyncer(
		con.syncedLastBlock,
		con.syncedSkipNext,
		con.dMoment,
		con.app,
		con.gov,
		con.db,
		con.network,
		con.prv,
		con.blocks,
		con.dummyMsgBuffer,
		con.logger)
	return con.syncedConsensus, err
}

// stopBuffering stops the syncer buffering routines.
//
// This method is mainly for caller to stop the syncer before synced, the syncer
// would call this method automatically after being synced.
func (con *Consensus) stopBuffering() {
	if func() (notBuffering bool) {
		con.lock.RLock()
		defer con.lock.RUnlock()
		notBuffering = !con.duringBuffering
		return
	}() {
		return
	}
	if func() (alreadyCanceled bool) {
		con.lock.Lock()
		defer con.lock.Unlock()
		if !con.duringBuffering {
			alreadyCanceled = true
			return
		}
		con.duringBuffering = false
		con.logger.Trace("Syncer is about to stop")
		// Stop network and CRS routines, wait until they are all stoped.
		con.ctxCancel()
		return
	}() {
		return
	}
	con.logger.Trace("Stop syncer modules")
	con.roundEvt.Stop()
	con.waitGroup.Done()
	// Wait for all routines depends on con.agreementModule stopped.
	con.waitGroup.Wait()
	// Since there is no one waiting for the receive channel of fullnode, we
	// need to launch a dummy receiver right away.
	con.dummyCancel, con.dummyFinished = utils.LaunchDummyReceiver(
		context.Background(), con.network.ReceiveChan(),
		func(msg types.Msg) {
			con.dummyMsgBuffer = append(con.dummyMsgBuffer, msg)
		})
	// Stop agreements.
	con.logger.Trace("Stop syncer agreement modules")
	con.stopAgreement()
	con.logger.Trace("Syncer stopped")
	return
}

// isEmptyBlock checks if a block is an empty block by both its hash and parent
// hash are empty.
func (con *Consensus) isEmptyBlock(b *types.Block) bool {
	return b.Hash == common.Hash{} && b.ParentHash == common.Hash{}
}

// buildEmptyBlock builds an empty block in agreement.
func (con *Consensus) buildEmptyBlock(b *types.Block, parent *types.Block) {
	cfg := utils.GetConfigWithPanic(con.gov, b.Position.Round, con.logger)
	b.Timestamp = parent.Timestamp.Add(cfg.MinBlockInterval)
	b.Witness.Height = parent.Witness.Height
	b.Witness.Data = make([]byte, len(parent.Witness.Data))
	copy(b.Witness.Data, parent.Witness.Data)
}

// startAgreement starts agreements for receiving votes and agreements.
func (con *Consensus) startAgreement() {
	// Start a routine for listening receive channel and pull block channel.
	go func() {
		for {
			select {
			case b, ok := <-con.receiveChan:
				if !ok {
					return
				}
				func() {
					con.lock.Lock()
					defer con.lock.Unlock()
					if len(con.blocks) > 0 &&
						!b.Position.Newer(con.blocks[0].Position) {
						return
					}
					con.blocks = append(con.blocks, b)
					sort.Sort(con.blocks)
				}()
			case h, ok := <-con.pullChan:
				if !ok {
					return
				}
				con.network.PullBlocks(common.Hashes{h})
			}
		}
	}()
}

// startNetwork starts network for receiving blocks and agreement results.
func (con *Consensus) startNetwork() {
	con.waitGroup.Add(1)
	go func() {
		defer con.waitGroup.Done()
	loop:
		for {
			select {
			case val := <-con.network.ReceiveChan():
				switch v := val.Payload.(type) {
				case *types.Block:
				case *types.AgreementResult:
					// Avoid byzantine nodes attack by broadcasting older
					// agreement results. Normal nodes might report 'synced'
					// while still fall behind other nodes.
					if v.Position.Height <= con.initChainTipHeight {
						continue loop
					}
				default:
					continue loop
				}
				con.agreementModule.inputChan <- val.Payload
			case <-con.ctx.Done():
				break loop
			}
		}
	}()
}

func (con *Consensus) stopAgreement() {
	if con.agreementModule.inputChan != nil {
		close(con.agreementModule.inputChan)
	}
	con.agreementWaitGroup.Wait()
	con.agreementModule.inputChan = nil
	close(con.receiveChan)
	close(con.pullChan)
}
