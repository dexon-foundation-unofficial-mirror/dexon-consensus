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
	"bytes"
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
	// ErrInvalidBlockOrder is reported when SyncBlocks receives unordered blocks.
	ErrInvalidBlockOrder = fmt.Errorf("invalid block order")
	// ErrMismatchBlockHashSequence means the delivering sequence is not
	// correct, compared to finalized blocks.
	ErrMismatchBlockHashSequence = fmt.Errorf("mismatch block hash sequence")
	// ErrInvalidSyncingFinalizationHeight raised when the blocks to sync is
	// not following the compaction chain tip in database.
	ErrInvalidSyncingFinalizationHeight = fmt.Errorf(
		"invalid syncing finalization height")
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

	lattice              *core.Lattice
	validatedChains      map[uint32]struct{}
	finalizedBlockHashes common.Hashes
	latticeLastRound     uint64
	randomnessResults    map[common.Hash]*types.BlockRandomnessResult
	blocks               []types.ByPosition
	agreements           []*agreement
	configs              []*types.Config
	roundBeginTimes      []time.Time
	agreementRoundCut    uint64

	// lock for accessing all fields.
	lock               sync.RWMutex
	moduleWaitGroup    sync.WaitGroup
	agreementWaitGroup sync.WaitGroup
	pullChan           chan common.Hash
	receiveChan        chan *types.Block
	ctx                context.Context
	ctxCancel          context.CancelFunc
	syncedLastBlock    *types.Block
	syncedConsensus    *core.Consensus
}

// NewConsensus creates an instance for Consensus (syncer consensus).
func NewConsensus(
	dMoment time.Time,
	app core.Application,
	gov core.Governance,
	db db.Database,
	network core.Network,
	prv crypto.PrivateKey,
	logger common.Logger) *Consensus {

	con := &Consensus{
		dMoment:         dMoment,
		app:             app,
		gov:             gov,
		db:              db,
		network:         network,
		nodeSetCache:    utils.NewNodeSetCache(gov),
		prv:             prv,
		logger:          logger,
		validatedChains: make(map[uint32]struct{}),
		configs: []*types.Config{
			utils.GetConfigWithPanic(gov, 0, logger),
		},
		roundBeginTimes:   []time.Time{dMoment},
		receiveChan:       make(chan *types.Block, 1000),
		pullChan:          make(chan common.Hash, 1000),
		randomnessResults: make(map[common.Hash]*types.BlockRandomnessResult),
	}
	con.ctx, con.ctxCancel = context.WithCancel(context.Background())
	return con
}

func (con *Consensus) initConsensusObj(initBlock *types.Block) {
	var cfg *types.Config
	func() {
		con.lock.Lock()
		defer con.lock.Unlock()
		con.latticeLastRound = initBlock.Position.Round
		cfg = con.configs[con.latticeLastRound]
		debugApp, _ := con.app.(core.Debug)
		con.lattice = core.NewLattice(
			con.roundBeginTimes[con.latticeLastRound],
			con.latticeLastRound,
			cfg,
			utils.NewSigner(con.prv),
			con.app,
			debugApp,
			con.db,
			con.logger,
		)
	}()
	con.startAgreement(cfg.NumChains)
	con.startNetwork()
	con.startCRSMonitor()
}

func (con *Consensus) checkIfValidated() bool {
	con.lock.RLock()
	defer con.lock.RUnlock()
	var numChains = con.configs[con.blocks[0][0].Position.Round].NumChains
	var validatedChainCount uint32
	// Make sure we validate some block in all chains.
	for chainID := range con.validatedChains {
		if chainID < numChains {
			validatedChainCount++
		}
	}
	if validatedChainCount == numChains {
		return true
	}
	con.logger.Debug("not validated yet", "validated-chain", validatedChainCount)
	return false
}

func (con *Consensus) checkIfSynced(blocks []*types.Block) bool {
	con.lock.RLock()
	defer con.lock.RUnlock()
	var (
		numChains      = con.configs[con.blocks[0][0].Position.Round].NumChains
		compactionTips = make([]*types.Block, numChains)
		overlapCount   = uint32(0)
	)
	// Find tips (newset blocks) of each chain in compaction chain.
	b := blocks[len(blocks)-1]
	for tipCount := uint32(0); tipCount < numChains; {
		if compactionTips[b.Position.ChainID] == nil {
			// Check chainID for config change.
			if b.Position.ChainID < numChains {
				compactionTips[b.Position.ChainID] = b
				tipCount++
			}
		}
		if (b.Finalization.ParentHash == common.Hash{}) {
			return false
		}
		b1, err := con.db.GetBlock(b.Finalization.ParentHash)
		if err != nil {
			panic(err)
		}
		b = &b1
	}
	// Check if chain tips of compaction chain and current cached confirmed
	// blocks are overlapped on each chain, numChains is decided by the round
	// of last block we seen on compaction chain.
	for chainID, b := range compactionTips {
		if len(con.blocks[chainID]) > 0 {
			if !b.Position.Older(&con.blocks[chainID][0].Position) {
				overlapCount++
			}
		}
	}
	if overlapCount == numChains {
		return true
	}
	con.logger.Debug("not synced yet",
		"overlap-count", overlapCount,
		"num-chain", numChains,
		"last-block", blocks[len(blocks)-1])
	return false
}

// ensureAgreementOverlapRound ensures the oldest blocks in each chain in
// con.blocks are all in the same round, for avoiding config change while
// syncing.
func (con *Consensus) ensureAgreementOverlapRound() bool {
	con.lock.Lock()
	defer con.lock.Unlock()
	if con.agreementRoundCut > 0 {
		return true
	}
	// Clean empty blocks on tips of chains.
	for idx, bs := range con.blocks {
		for len(bs) > 0 && con.isEmptyBlock(bs[0]) {
			bs = bs[1:]
		}
		con.blocks[idx] = bs
	}
	// Build empty blocks.
	for _, bs := range con.blocks {
		for i := range bs {
			if con.isEmptyBlock(bs[i]) {
				if bs[i-1].Position.Height == bs[i].Position.Height-1 {
					con.buildEmptyBlock(bs[i], bs[i-1])
				}
			}
		}
	}
	var tipRoundMap map[uint64]uint32
	for {
		tipRoundMap = make(map[uint64]uint32)
		for _, bs := range con.blocks {
			if len(bs) > 0 {
				tipRoundMap[bs[0].Position.Round]++
			}
		}
		if len(tipRoundMap) <= 1 {
			break
		}
		// Make all tips in same round.
		var maxRound uint64
		for r := range tipRoundMap {
			if r > maxRound {
				maxRound = r
			}
		}
		for idx, bs := range con.blocks {
			for len(bs) > 0 && bs[0].Position.Round < maxRound {
				bs = bs[1:]
			}
			con.blocks[idx] = bs
		}
	}
	if len(tipRoundMap) == 1 {
		var r uint64
		for r = range tipRoundMap {
			break
		}
		con.logger.Debug("check agreement round cut",
			"tip-round", r,
			"configs", len(con.configs))
		if tipRoundMap[r] == con.configs[r].NumChains {
			con.agreementRoundCut = r
			con.logger.Info("agreement round cut found, round", r)
			return true
		}
	}
	return false
}

func (con *Consensus) findLatticeSyncBlock(
	blocks []*types.Block) (*types.Block, error) {
	lastBlock := blocks[len(blocks)-1]
	round := lastBlock.Position.Round
	for {
		// Find round r which r-1, r, r+1 are all in same total ordering config.
		for {
			sameAsPrevRound := round == 0 || !con.isConfigChanged(
				con.configs[round-1], con.configs[round])
			sameAsNextRound := !con.isConfigChanged(
				con.configs[round], con.configs[round+1])
			if sameAsPrevRound && sameAsNextRound {
				break
			}
			if round == 0 {
				// Unable to find a safe round, wait for new rounds.
				return nil, nil
			}
			round--
		}
		// Find the newset block which round is "round".
		for lastBlock.Position.Round != round {
			if (lastBlock.Finalization.ParentHash == common.Hash{}) {
				return nil, ErrGenesisBlockReached
			}
			b, err := con.db.GetBlock(lastBlock.Finalization.ParentHash)
			if err != nil {
				return nil, err
			}
			lastBlock = &b
		}
		// Find the deliver set by hash for two times. Blocks in a deliver set
		// returned by total ordering is sorted by hash. If a block's parent hash
		// is greater than its hash means there is a cut between deliver sets.
		var curBlock, prevBlock *types.Block
		var deliverSetFirstBlock, deliverSetLastBlock *types.Block
		curBlock = lastBlock
		for {
			if (curBlock.Finalization.ParentHash == common.Hash{}) {
				return nil, ErrGenesisBlockReached
			}
			b, err := con.db.GetBlock(curBlock.Finalization.ParentHash)
			if err != nil {
				return nil, err
			}
			prevBlock = &b
			if !prevBlock.Hash.Less(curBlock.Hash) {
				break
			}
			curBlock = prevBlock
		}
		deliverSetLastBlock = prevBlock
		curBlock = prevBlock
		for {
			if (curBlock.Finalization.ParentHash == common.Hash{}) {
				break
			}
			b, err := con.db.GetBlock(curBlock.Finalization.ParentHash)
			if err != nil {
				return nil, err
			}
			prevBlock = &b
			if !prevBlock.Hash.Less(curBlock.Hash) {
				break
			}
			curBlock = prevBlock
		}
		deliverSetFirstBlock = curBlock
		// Check if all blocks from deliverSetFirstBlock to deliverSetLastBlock
		// are in the same round.
		ok := true
		curBlock = deliverSetLastBlock
		for {
			if curBlock.Position.Round != round {
				ok = false
				break
			}
			b, err := con.db.GetBlock(curBlock.Finalization.ParentHash)
			if err != nil {
				return nil, err
			}
			curBlock = &b
			if curBlock.Hash == deliverSetFirstBlock.Hash {
				break
			}
		}
		if ok {
			return deliverSetFirstBlock, nil
		}
		if round == 0 {
			return nil, nil
		}
		round--
	}
}

func (con *Consensus) processFinalizedBlock(block *types.Block) error {
	if con.lattice == nil {
		return nil
	}
	con.finalizedBlockHashes = append(con.finalizedBlockHashes, block.Hash)
	delivered, err := con.lattice.ProcessFinalizedBlock(block)
	if err != nil {
		return err
	}
	for idx, b := range delivered {
		if con.finalizedBlockHashes[idx] != b.Hash {
			return ErrMismatchBlockHashSequence
		}
		con.validatedChains[b.Position.ChainID] = struct{}{}
	}
	con.finalizedBlockHashes = con.finalizedBlockHashes[len(delivered):]
	return nil
}

// SyncBlocks syncs blocks from compaction chain, latest is true if the caller
// regards the blocks are the latest ones. Notice that latest can be true for
// many times.
// NOTICE: parameter "blocks" should be consecutive in compaction height.
func (con *Consensus) SyncBlocks(
	blocks []*types.Block, latest bool) (bool, error) {
	if con.syncedLastBlock != nil {
		return true, ErrAlreadySynced
	}
	if len(blocks) == 0 {
		return false, nil
	}
	// Check if blocks are consecutive.
	for i := 1; i < len(blocks); i++ {
		if blocks[i].Finalization.Height != blocks[i-1].Finalization.Height+1 {
			return false, ErrInvalidBlockOrder
		}
	}
	// Make sure the first block is the next block of current compaction chain
	// tip in DB.
	_, tipHeight := con.db.GetCompactionChainTipInfo()
	if blocks[0].Finalization.Height != tipHeight+1 {
		con.logger.Error("mismatched finalization height",
			"now", blocks[0].Finalization.Height,
			"expected", tipHeight+1)
		return false, ErrInvalidSyncingFinalizationHeight
	}
	con.logger.Trace("syncBlocks",
		"position", &blocks[0].Position,
		"final height", blocks[0].Finalization.Height,
		"len", len(blocks),
		"latest", latest,
	)
	con.setupConfigs(blocks)
	for _, b := range blocks {
		// TODO(haoping) remove this if lattice puts blocks into db.
		if err := con.db.PutBlock(*b); err != nil {
			// A block might be put into db when confirmed by BA, but not
			// finalized yet.
			if err == db.ErrBlockExists {
				err = con.db.UpdateBlock(*b)
			}
			if err != nil {
				return false, err
			}
		}
		if err := con.db.PutCompactionChainTipInfo(
			b.Hash, b.Finalization.Height); err != nil {
			return false, err
		}
		if err := con.processFinalizedBlock(b); err != nil {
			return false, err
		}
	}
	if latest && con.lattice == nil {
		// New Lattice and find the deliver set of total ordering when "latest" is
		// true for first time. Deliver set is found by block hashes.
		syncBlock, err := con.findLatticeSyncBlock(blocks)
		if err != nil {
			return false, err
		}
		if syncBlock != nil {
			con.logger.Debug("deliver set found", "block", syncBlock)
			// New lattice with the round of syncBlock.
			con.initConsensusObj(syncBlock)
			con.setupConfigs(blocks)
			// Process blocks from syncBlock to blocks' last block.
			b := blocks[len(blocks)-1]
			blocksCount := b.Finalization.Height - syncBlock.Finalization.Height + 1
			blocksToProcess := make([]*types.Block, blocksCount)
			for {
				blocksToProcess[blocksCount-1] = b
				blocksCount--
				if b.Hash == syncBlock.Hash {
					break
				}
				b1, err := con.db.GetBlock(b.Finalization.ParentHash)
				if err != nil {
					return false, err
				}
				b = &b1
			}
			for _, b := range blocksToProcess {
				if err := con.processFinalizedBlock(b); err != nil {
					return false, err
				}
			}
		}
	}
	if latest && con.ensureAgreementOverlapRound() {
		// Check if compaction and agreements' blocks are overlapped. The
		// overlapping of compaction chain and BA's oldest blocks means the
		// syncing is done.
		if con.checkIfValidated() && con.checkIfSynced(blocks) {
			if err := con.Stop(); err != nil {
				return false, err
			}
			con.syncedLastBlock = blocks[len(blocks)-1]
			con.logger.Info("syncer.Consensus synced",
				"last-block", con.syncedLastBlock)
		}
	}
	return con.syncedLastBlock != nil, nil
}

// GetSyncedConsensus returns the core.Consensus instance after synced.
func (con *Consensus) GetSyncedConsensus() (*core.Consensus, error) {
	if con.syncedConsensus != nil {
		return con.syncedConsensus, nil
	}
	if con.syncedLastBlock == nil {
		return nil, ErrNotSynced
	}
	// flush all blocks in con.blocks into core.Consensus, and build
	// core.Consensus from syncer.
	confirmedBlocks := []*types.Block{}
	randomnessResults := []*types.BlockRandomnessResult{}
	func() {
		con.lock.Lock()
		defer con.lock.Unlock()
		for _, bs := range con.blocks {
			confirmedBlocks = append(confirmedBlocks, bs...)
		}
		for _, r := range con.randomnessResults {
			randomnessResults = append(randomnessResults, r)
		}
	}()
	var err error
	con.syncedConsensus, err = core.NewConsensusFromSyncer(
		con.syncedLastBlock,
		con.roundBeginTimes[con.syncedLastBlock.Position.Round],
		con.app,
		con.gov,
		con.db,
		con.network,
		con.prv,
		con.lattice,
		confirmedBlocks,
		randomnessResults,
		con.logger)
	return con.syncedConsensus, err
}

// Stop the syncer.
//
// This method is mainly for caller to stop the syncer before synced, the syncer
// would call this method automatically after synced.
func (con *Consensus) Stop() error {
	// Stop network and CRS routines, wait until they are all stoped.
	con.ctxCancel()
	con.moduleWaitGroup.Wait()
	// Stop agreements.
	con.stopAgreement()
	return nil
}

// isEmptyBlock checks if a block is an empty block by both its hash and parent
// hash are empty.
func (con *Consensus) isEmptyBlock(b *types.Block) bool {
	return b.Hash == common.Hash{} && b.ParentHash == common.Hash{}
}

// buildEmptyBlock builds an empty block in agreement.
func (con *Consensus) buildEmptyBlock(b *types.Block, parent *types.Block) {
	cfg := con.configs[b.Position.Round]
	b.Timestamp = parent.Timestamp.Add(cfg.MinBlockInterval)
	b.Witness.Height = parent.Witness.Height
	b.Witness.Data = make([]byte, len(parent.Witness.Data))
	copy(b.Witness.Data, parent.Witness.Data)
	b.Acks = common.NewSortedHashes(common.Hashes{parent.Hash})
}

func (con *Consensus) setupConfigsUntilRound(round uint64) {
	curMaxNumChains := uint32(0)
	func() {
		con.lock.Lock()
		defer con.lock.Unlock()
		for r := uint64(len(con.configs)); r <= round; r++ {
			cfg := utils.GetConfigWithPanic(con.gov, r, con.logger)
			con.configs = append(con.configs, cfg)
			con.roundBeginTimes = append(
				con.roundBeginTimes,
				con.roundBeginTimes[r-1].Add(con.configs[r-1].RoundInterval))
			if cfg.NumChains >= curMaxNumChains {
				curMaxNumChains = cfg.NumChains
			}
		}
		// Notify core.Lattice for new configs.
		if con.lattice != nil {
			for con.latticeLastRound+1 <= round {
				con.latticeLastRound++
				if err := con.lattice.AppendConfig(
					con.latticeLastRound,
					con.configs[con.latticeLastRound]); err != nil {
					panic(err)
				}
			}
		}
	}()
	con.resizeByNumChains(curMaxNumChains)
}

// setupConfigs is called by SyncBlocks with blocks from compaction chain. In
// the first time, setupConfigs setups from round 0.
func (con *Consensus) setupConfigs(blocks []*types.Block) {
	// Find max round in blocks.
	var maxRound uint64
	for _, b := range blocks {
		if b.Position.Round > maxRound {
			maxRound = b.Position.Round
		}
	}
	con.logger.Debug("syncer setupConfigs",
		"max", maxRound,
		"lattice", con.latticeLastRound)
	// Get configs from governance.
	//
	// In fullnode, the notification of new round is yet another TX, which
	// needs to be executed after corresponding block delivered. Thus, the
	// configuration for 'maxRound + core.ConfigRoundShift' won't be ready when
	// seeing this block.
	con.setupConfigsUntilRound(maxRound + core.ConfigRoundShift - 1)
}

// resizeByNumChains resizes fake lattice and agreement if numChains increases.
// Notice the decreasing case is neglected.
func (con *Consensus) resizeByNumChains(numChains uint32) {
	con.lock.Lock()
	defer con.lock.Unlock()
	if numChains > uint32(len(con.blocks)) {
		for i := uint32(len(con.blocks)); i < numChains; i++ {
			// Resize the pool of blocks.
			con.blocks = append(con.blocks, types.ByPosition{})
			// Resize agreement modules.
			a := newAgreement(con.receiveChan, con.pullChan, con.nodeSetCache, con.logger)
			con.agreements = append(con.agreements, a)
			con.agreementWaitGroup.Add(1)
			go func() {
				defer con.agreementWaitGroup.Done()
				a.run()
			}()
		}
	}
}

// startAgreement starts agreements for receiving votes and agreements.
func (con *Consensus) startAgreement(numChains uint32) {
	// Start a routine for listening receive channel and pull block channel.
	go func() {
		for {
			select {
			case b, ok := <-con.receiveChan:
				if !ok {
					return
				}
				chainID := b.Position.ChainID
				func() {
					con.lock.Lock()
					defer con.lock.Unlock()
					// If round is cut in agreements, do not add blocks with round less
					// then cut round.
					if b.Position.Round < con.agreementRoundCut {
						return
					}
					con.blocks[chainID] = append(con.blocks[chainID], b)
					sort.Sort(con.blocks[chainID])
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

func (con *Consensus) cacheRandomnessResult(r *types.BlockRandomnessResult) {
	// We only have to cache randomness result after cutting round.
	if r.Position.Round < func() uint64 {
		con.lock.RLock()
		defer con.lock.RUnlock()
		return con.agreementRoundCut
	}() {
		return
	}
	con.lock.Lock()
	defer con.lock.Unlock()
	if old, exists := con.randomnessResults[r.BlockHash]; exists {
		if bytes.Compare(old.Randomness, r.Randomness) != 0 {
			panic(fmt.Errorf("receive different randomness result: %s, %s",
				r.BlockHash.String()[:6], &r.Position))
		}
		// We don't have to assign the map again.
		return
	}
	con.randomnessResults[r.BlockHash] = r
}

// startNetwork starts network for receiving blocks and agreement results.
func (con *Consensus) startNetwork() {
	go func() {
		con.moduleWaitGroup.Add(1)
		defer con.moduleWaitGroup.Done()
	Loop:
		for {
			select {
			case val := <-con.network.ReceiveChan():
				var pos types.Position
				switch v := val.(type) {
				case *types.Block:
					pos = v.Position
				case *types.AgreementResult:
					pos = v.Position
				case *types.BlockRandomnessResult:
					con.cacheRandomnessResult(v)
					continue Loop
				default:
					continue Loop
				}
				func() {
					con.lock.RLock()
					defer con.lock.RUnlock()
					if pos.ChainID >= uint32(len(con.agreements)) {
						con.logger.Error("Unknown chainID message received (syncer)",
							"position", &pos)
					}
				}()
				con.agreements[pos.ChainID].inputChan <- val
			case <-con.ctx.Done():
				return
			}
		}
	}()
}

// startCRSMonitor is the dummiest way to verify if the CRS for one round
// is ready or not.
func (con *Consensus) startCRSMonitor() {
	var lastNotifiedRound uint64
	// Notify all agreements for new CRS.
	notifyNewCRS := func(round uint64) {
		con.setupConfigsUntilRound(round)
		con.lock.Lock()
		defer con.lock.Unlock()
		if round == lastNotifiedRound {
			return
		}
		con.logger.Debug("CRS is ready", "round", round)
		lastNotifiedRound = round
		for _, a := range con.agreements {
			a.inputChan <- round
		}
	}
	go func() {
		con.moduleWaitGroup.Add(1)
		defer con.moduleWaitGroup.Done()
		for {
			select {
			case <-con.ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			// Notify agreement modules for the latest round that CRS is
			// available if the round is not notified yet.
			checked := lastNotifiedRound + 1
			for (con.gov.CRS(checked) != common.Hash{}) {
				checked++
			}
			checked--
			if checked > lastNotifiedRound {
				notifyNewCRS(checked)
			}
		}
	}()
}

func (con *Consensus) stopAgreement() {
	func() {
		con.lock.Lock()
		defer con.lock.Unlock()
		for _, a := range con.agreements {
			if a.inputChan != nil {
				close(a.inputChan)
				a.inputChan = nil
			}
		}
	}()
	con.agreementWaitGroup.Wait()
	close(con.receiveChan)
	close(con.pullChan)
}

func (con *Consensus) isConfigChanged(prev, cur *types.Config) bool {
	return prev.K != cur.K ||
		prev.NumChains != cur.NumChains ||
		prev.PhiRatio != cur.PhiRatio
}
