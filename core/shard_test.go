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
	"math/rand"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
	"github.com/stretchr/testify/suite"
)

// testShardMgr wraps compaction chain and shard.
type testShardMgr struct {
	shard    *Shard
	ccModule *compactionChain
	app      *test.App
	db       blockdb.BlockDatabase
}

func (mgr *testShardMgr) prepareBlock(
	chainID uint32) (b *types.Block, err error) {

	b = &types.Block{
		Position: types.Position{
			ChainID: chainID,
		}}
	err = mgr.shard.PrepareBlock(b, time.Now().UTC())
	return
}

// Process describes the usage of Shard.ProcessBlock.
func (mgr *testShardMgr) processBlock(b *types.Block) (err error) {
	var (
		delivered []*types.Block
		verified  []*types.Block
		pendings  = []*types.Block{b}
	)
	if err = mgr.shard.SanityCheck(b); err != nil {
		if err == ErrAckingBlockNotExists {
			err = nil
		}
		return
	}
	for {
		if len(pendings) == 0 {
			break
		}
		b, pendings = pendings[0], pendings[1:]
		if verified, delivered, err = mgr.shard.ProcessBlock(b); err != nil {
			return
		}
		// Deliver blocks.
		for _, b = range delivered {
			if err = mgr.ccModule.processBlock(b); err != nil {
				return
			}
			if err = mgr.db.Update(*b); err != nil {
				return
			}
			mgr.app.BlockDeliver(*b)
		}
		// Update pending blocks for verified block (pass sanity check).
		pendings = append(pendings, verified...)
	}
	return
}

type ShardTestSuite struct {
	suite.Suite
}

func (s *ShardTestSuite) newTestShardMgr(cfg *types.Config) *testShardMgr {
	var req = s.Require()
	// Setup private key.
	prvKey, err := eth.NewPrivateKey()
	req.Nil(err)
	// Setup blockdb.
	db, err := blockdb.NewMemBackedBlockDB()
	req.Nil(err)
	// Setup application.
	app := test.NewApp()
	// Setup shard.
	return &testShardMgr{
		ccModule: newCompactionChain(db, eth.SigToPub),
		app:      app,
		db:       db,
		shard: NewShard(
			uint32(0),
			cfg,
			prvKey,
			eth.SigToPub,
			app,
			app,
			db)}
}

func (s *ShardTestSuite) TestBasicUsage() {
	// One shard prepare blocks on chains randomly selected each time
	// and process it. Those generated blocks and kept into a buffer, and
	// process by other shard instances with random order.
	var (
		blockNum      = 100
		chainNum      = uint32(19)
		otherShardNum = 20
		req           = s.Require()
		err           error
		cfg           = types.Config{
			NumChains: chainNum,
			PhiRatio:  float32(2) / float32(3),
			K:         0,
		}
		master    = s.newTestShardMgr(&cfg)
		apps      = []*test.App{master.app}
		revealSeq = map[string]struct{}{}
	)
	// Master-shard generates blocks.
	for i := uint32(0); i < chainNum; i++ {
		// Produce genesis blocks should be delivered before all other blocks,
		// or the consensus time would be wrong.
		b, err := master.prepareBlock(i)
		req.NotNil(b)
		req.Nil(err)
		// We've ignored the error for "acking blocks don't exist".
		req.Nil(master.processBlock(b))
	}
	for i := 0; i < (blockNum - int(chainNum)); i++ {
		b, err := master.prepareBlock(uint32(rand.Intn(int(chainNum))))
		req.NotNil(b)
		req.Nil(err)
		// We've ignored the error for "acking blocks don't exist".
		req.Nil(master.processBlock(b))
	}
	// Now we have some blocks, replay them on different shards.
	iter, err := master.db.GetAll()
	req.Nil(err)
	revealer, err := test.NewRandomRevealer(iter)
	req.Nil(err)
	for i := 0; i < otherShardNum; i++ {
		revealer.Reset()
		revealed := ""
		other := s.newTestShardMgr(&cfg)
		for {
			b, err := revealer.Next()
			if err != nil {
				if err == blockdb.ErrIterationFinished {
					err = nil
					break
				}
			}
			req.Nil(err)
			req.Nil(other.processBlock(&b))
			revealed += b.Hash.String() + ","
			revealSeq[revealed] = struct{}{}
		}
		apps = append(apps, other.app)
	}
	// Make sure not only one revealing sequence.
	req.True(len(revealSeq) > 1)
	// Make sure nothing goes wrong.
	for i, app := range apps {
		req.Nil(app.Verify())
		for j, otherApp := range apps {
			if i >= j {
				continue
			}
			req.Nil(app.Compare(otherApp))
		}
	}
}

func TestShard(t *testing.T) {
	suite.Run(t, new(ShardTestSuite))
}
