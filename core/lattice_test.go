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
	"math/rand"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

// testLatticeMgr wraps compaction chain and lattice.
type testLatticeMgr struct {
	lattice  *Lattice
	ccModule *compactionChain
	app      *test.App
	db       blockdb.BlockDatabase
}

func (mgr *testLatticeMgr) prepareBlock(
	chainID uint32) (b *types.Block, err error) {

	b = &types.Block{
		Position: types.Position{
			ChainID: chainID,
		}}
	err = mgr.lattice.PrepareBlock(b, time.Now().UTC())
	return
}

// Process describes the usage of Lattice.ProcessBlock.
func (mgr *testLatticeMgr) processBlock(b *types.Block) (err error) {
	var (
		delivered []*types.Block
	)
	if err = mgr.lattice.SanityCheck(b); err != nil {
		if err == ErrRetrySanityCheckLater {
			err = nil
		} else {
			return
		}
	}
	if err = mgr.db.Put(*b); err != nil {
		if err != blockdb.ErrBlockExists {
			return
		}
		err = nil
	}
	if delivered, err = mgr.lattice.ProcessBlock(b); err != nil {
		return
	}
	// Deliver blocks.
	for _, b = range delivered {
		if err = mgr.ccModule.processBlock(b); err != nil {
			return
		}
	}
	for _, b = range mgr.ccModule.extractBlocks() {
		if err = mgr.db.Update(*b); err != nil {
			return
		}
		mgr.app.BlockDelivered(b.Hash, b.Position, b.Finalization)
	}
	if err = mgr.lattice.PurgeBlocks(delivered); err != nil {
		return
	}
	return
}

type LatticeTestSuite struct {
	suite.Suite
}

func (s *LatticeTestSuite) newTestLatticeMgr(
	cfg *types.Config, dMoment time.Time) *testLatticeMgr {
	var req = s.Require()
	// Setup private key.
	prvKey, err := ecdsa.NewPrivateKey()
	req.NoError(err)
	// Setup blockdb.
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	// Setup governance.
	_, pubKeys, err := test.NewKeys(int(cfg.NotarySetSize))
	req.NoError(err)
	gov, err := test.NewGovernance(pubKeys, cfg.LambdaBA, ConfigRoundShift)
	req.NoError(err)
	// Setup application.
	app := test.NewApp(gov.State())
	// Setup compaction chain.
	cc := newCompactionChain(gov)
	cc.init(&types.Block{})
	mock := newMockTSigVerifier(true)
	for i := 0; i < cc.tsigVerifier.cacheSize; i++ {
		cc.tsigVerifier.verifier[uint64(i)] = mock
	}
	// Setup lattice.
	return &testLatticeMgr{
		ccModule: cc,
		app:      app,
		db:       db,
		lattice: NewLattice(
			dMoment,
			0,
			cfg,
			NewAuthenticator(prvKey),
			app,
			app,
			db,
			&common.NullLogger{})}
}

func (s *LatticeTestSuite) TestBasicUsage() {
	// One Lattice prepare blocks on chains randomly selected each time
	// and process it. Those generated blocks and kept into a buffer, and
	// process by other Lattice instances with random order.
	var (
		blockNum        = 100
		chainNum        = uint32(19)
		otherLatticeNum = 20
		req             = s.Require()
		err             error
		cfg             = types.Config{
			NumChains:        chainNum,
			NotarySetSize:    chainNum,
			PhiRatio:         float32(2) / float32(3),
			K:                0,
			MinBlockInterval: 0,
			RoundInterval:    time.Hour,
		}
		dMoment   = time.Now().UTC()
		master    = s.newTestLatticeMgr(&cfg, dMoment)
		apps      = []*test.App{master.app}
		revealSeq = map[string]struct{}{}
	)
	// Master-lattice generates blocks.
	for i := uint32(0); i < chainNum; i++ {
		// Produced genesis blocks should be delivered before all other blocks,
		// or the consensus time would be wrong.
		b, err := master.prepareBlock(i)
		req.NotNil(b)
		req.NoError(err)
		// Ignore error "acking blocks don't exist".
		req.NoError(master.processBlock(b))
	}
	for i := 0; i < (blockNum - int(chainNum)); i++ {
		b, err := master.prepareBlock(uint32(rand.Intn(int(chainNum))))
		req.NotNil(b)
		req.NoError(err)
		// Ignore error "acking blocks don't exist".
		req.NoError(master.processBlock(b))
	}
	// Now we have some blocks, replay them on different lattices.
	iter, err := master.db.GetAll()
	req.NoError(err)
	revealer, err := test.NewRandomRevealer(iter)
	req.NoError(err)
	for i := 0; i < otherLatticeNum; i++ {
		revealer.Reset()
		revealed := ""
		other := s.newTestLatticeMgr(&cfg, dMoment)
		for {
			b, err := revealer.Next()
			if err != nil {
				if err == blockdb.ErrIterationFinished {
					err = nil
					break
				}
			}
			req.NoError(err)
			req.NoError(other.processBlock(&b))
			revealed += b.Hash.String() + ","
		}
		revealSeq[revealed] = struct{}{}
		apps = append(apps, other.app)
	}
	// Make sure not only one revealing sequence.
	req.True(len(revealSeq) > 1)
	// Make sure nothing goes wrong.
	for i, app := range apps {
		err := app.Verify()
		req.NoError(err)
		for j, otherApp := range apps {
			if i >= j {
				continue
			}
			err := app.Compare(otherApp)
			s.NoError(err)
		}
	}
}

func (s *LatticeTestSuite) TestSanityCheck() {
	// This sanity check focuses on hash/signature part.
	var (
		chainNum = uint32(19)
		cfg      = types.Config{
			NumChains:        chainNum,
			PhiRatio:         float32(2) / float32(3),
			K:                0,
			MinBlockInterval: 0,
		}
		lattice = s.newTestLatticeMgr(&cfg, time.Now().UTC()).lattice
		auth    = lattice.authModule // Steal auth module from lattice, :(
		req     = s.Require()
		err     error
	)
	// A block properly signed should pass sanity check.
	b := &types.Block{
		Position:  types.Position{ChainID: 0},
		Timestamp: time.Now().UTC(),
	}
	req.NoError(auth.SignBlock(b))
	req.NoError(lattice.SanityCheck(b))
	// A block with incorrect signature should not pass sanity check.
	b.Signature, err = auth.prvKey.Sign(common.NewRandomHash())
	req.NoError(err)
	req.Equal(lattice.SanityCheck(b), ErrIncorrectSignature)
	// A block with un-sorted acks should not pass sanity check.
	b.Acks = common.NewSortedHashes(common.Hashes{
		common.NewRandomHash(),
		common.NewRandomHash(),
		common.NewRandomHash(),
		common.NewRandomHash(),
		common.NewRandomHash(),
	})
	b.Acks[0], b.Acks[1] = b.Acks[1], b.Acks[0]
	req.NoError(auth.SignBlock(b))
	req.Equal(lattice.SanityCheck(b), ErrAcksNotSorted)
	// A block with incorrect hash should not pass sanity check.
	b.Hash = common.NewRandomHash()
	req.Equal(lattice.SanityCheck(b), ErrIncorrectHash)
}

func TestLattice(t *testing.T) {
	suite.Run(t, new(LatticeTestSuite))
}
