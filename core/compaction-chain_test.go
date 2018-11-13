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
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type CompactionChainTestSuite struct {
	suite.Suite
}

func (s *CompactionChainTestSuite) SetupTest() {
}

func (s *CompactionChainTestSuite) newCompactionChain() *compactionChain {
	_, pubKeys, err := test.NewKeys(4)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(
		pubKeys, 100*time.Millisecond, ConfigRoundShift)
	s.Require().NoError(err)
	cc := newCompactionChain(gov)
	cc.init(&types.Block{})
	return cc
}

func (s *CompactionChainTestSuite) generateBlocks(
	size int, cc *compactionChain) []*types.Block {
	now := time.Now().UTC()
	blocks := make([]*types.Block, size)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Finalization: types.FinalizationResult{
				Timestamp: now,
			},
		}
		now = now.Add(100 * time.Millisecond)
	}
	for _, block := range blocks {
		err := cc.processBlock(block)
		s.Require().Nil(err)
	}
	return blocks
}

func (s *CompactionChainTestSuite) TestProcessBlock() {
	cc := s.newCompactionChain()
	now := time.Now().UTC()
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Finalization: types.FinalizationResult{
				Timestamp: now,
			},
		}
		now = now.Add(100 * time.Millisecond)
	}
	for _, block := range blocks {
		s.Require().NoError(cc.processBlock(block))
	}
	s.Len(cc.pendingBlocks, len(blocks)+1)
}

func (s *CompactionChainTestSuite) TestExtractBlocks() {
	cc := s.newCompactionChain()
	s.Require().Equal(uint32(4), cc.gov.Configuration(uint64(0)).NumChains)
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Position: types.Position{
				Round:   1,
				ChainID: uint32(idx % 4),
			},
		}
		s.Require().False(cc.blockRegistered(blocks[idx].Hash))
		cc.registerBlock(blocks[idx])
		s.Require().True(cc.blockRegistered(blocks[idx].Hash))
	}
	// Randomness is ready for extract.
	for i := 0; i < 4; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Randomness: h[:],
			}))
	}
	delivered := cc.extractBlocks()
	s.Require().Len(delivered, 4)
	s.Require().Equal(uint32(0), cc.chainUnsynced)

	// Randomness is not yet ready for extract.
	for i := 4; i < 6; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 4)

	// Make some randomness ready.
	for i := 4; i < 6; i++ {
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Randomness: h[:],
			}))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 6)

	// Later block's randomness is ready.
	for i := 6; i < 10; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
		if i < 8 {
			continue
		}
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Randomness: h[:],
			}))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 6)

	// Prior block's randomness is ready.
	for i := 6; i < 8; i++ {
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Randomness: h[:],
			}))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 10)

	// The delivered order should be the same as processing order.
	for i, block := range delivered {
		if i > 1 {
			s.Equal(delivered[i-1].Finalization.Height+1,
				delivered[i].Finalization.Height)
			s.Equal(delivered[i-1].Hash,
				delivered[i].Finalization.ParentHash)
		}
		s.Equal(block.Hash, blocks[i].Hash)
	}
}

func (s *CompactionChainTestSuite) TestExtractBlocksRound0() {
	cc := s.newCompactionChain()
	s.Require().Equal(uint32(4), cc.gov.Configuration(uint64(0)).NumChains)
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Position: types.Position{
				Round: 0,
			},
		}
		s.Require().False(cc.blockRegistered(blocks[idx].Hash))
		cc.registerBlock(blocks[idx])
		s.Require().True(cc.blockRegistered(blocks[idx].Hash))
	}
	// Round 0 should be able to be extracted without randomness.
	for i := 0; i < 4; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
	}
	delivered := cc.extractBlocks()
	s.Require().Len(delivered, 4)

	// Round 0 should be able to be extracted without randomness.
	for i := 4; i < 10; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 10)

	// The delivered order should be the same as processing order.
	for i, block := range delivered {
		s.Equal(block.Hash, blocks[i].Hash)
	}
}

type mockTSigVerifier struct {
	defaultRet bool
	ret        map[common.Hash]bool
}

func newMockTSigVerifier(defaultRet bool) *mockTSigVerifier {
	return &mockTSigVerifier{
		defaultRet: defaultRet,
		ret:        make(map[common.Hash]bool),
	}
}

func (m *mockTSigVerifier) VerifySignature(
	hash common.Hash, _ crypto.Signature) bool {
	if ret, exist := m.ret[hash]; exist {
		return ret
	}
	return m.defaultRet
}

func (s *CompactionChainTestSuite) TestSyncFinalizedBlock() {
	cc := s.newCompactionChain()
	mock := newMockTSigVerifier(true)
	for i := 0; i < cc.tsigVerifier.cacheSize; i++ {
		cc.tsigVerifier.verifier[uint64(i)] = mock
	}
	now := time.Now().UTC()
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Finalization: types.FinalizationResult{
				Timestamp: now,
				Height:    uint64(idx + 1),
			},
		}
		now = now.Add(100 * time.Millisecond)
		if idx > 0 {
			blocks[idx].Finalization.ParentHash = blocks[idx-1].Hash
		}
	}
	cc.processFinalizedBlock(blocks[1])
	cc.processFinalizedBlock(blocks[3])
	s.Len(cc.extractFinalizedBlocks(), 0)

	cc.processFinalizedBlock(blocks[0])
	confirmed := cc.extractFinalizedBlocks()
	s.Equal(blocks[1].Hash, cc.lastBlock().Hash)
	s.Require().Len(confirmed, 2)
	s.Equal(confirmed[0].Hash, blocks[0].Hash)
	s.Equal(confirmed[1].Hash, blocks[1].Hash)
	hash := common.NewRandomHash()
	cc.processFinalizedBlock(&types.Block{
		Hash: hash,
		Position: types.Position{
			Round: uint64(1),
		},
		Finalization: types.FinalizationResult{
			Height: uint64(3),
		},
	})
	// Should not deliver block with error tsig
	mock.ret[hash] = false
	s.Len(cc.extractFinalizedBlocks(), 0)
	// The error block should be discarded.
	s.Len(cc.extractFinalizedBlocks(), 0)

	// Shuold not deliver block if dkg is not final
	round99 := uint64(99)
	s.Require().False(cc.gov.IsDKGFinal(round99))
	blocks[2].Position.Round = round99
	cc.processFinalizedBlock(blocks[2])
	s.Len(cc.extractFinalizedBlocks(), 0)

	// Deliver blocks.
	blocks[3].Position.Round = round99
	cc.tsigVerifier.verifier[round99] = mock
	confirmed = cc.extractFinalizedBlocks()
	s.Equal(blocks[3].Hash, cc.lastBlock().Hash)
	s.Require().Len(confirmed, 2)
	s.Equal(confirmed[0].Hash, blocks[2].Hash)
	s.Equal(confirmed[1].Hash, blocks[3].Hash)

	// Inserting a bad block. The later block should not be discarded.
	cc.processFinalizedBlock(blocks[5])
	cc.processFinalizedBlock(&types.Block{
		Hash: hash,
		Position: types.Position{
			Round: uint64(1),
		},
		Finalization: types.FinalizationResult{
			Height: uint64(5),
		},
	})
	s.Len(cc.extractFinalizedBlocks(), 0)
	// Good block is inserted, the later block should be delivered.
	cc.processFinalizedBlock(blocks[4])
	confirmed = cc.extractFinalizedBlocks()
	s.Equal(blocks[5].Hash, cc.lastBlock().Hash)
	s.Require().Len(confirmed, 2)
	s.Equal(confirmed[0].Hash, blocks[4].Hash)
	s.Equal(confirmed[1].Hash, blocks[5].Hash)

	// Ignore finalized block if it already confirmed.
	cc.init(blocks[5])
	cc.processFinalizedBlock(blocks[6])
	s.Require().NoError(cc.processBlock(blocks[5]))
	s.Require().NoError(cc.processBlock(blocks[6]))
	confirmed = cc.extractBlocks()
	s.Require().Len(confirmed, 1)
	s.Equal(confirmed[0].Hash, blocks[6].Hash)
	s.Equal(blocks[6].Hash, cc.lastBlock().Hash)
	s.Require().Len(cc.extractFinalizedBlocks(), 0)
	s.Require().Len(*cc.pendingFinalizedBlocks, 0)
}

func (s *CompactionChainTestSuite) TestSync() {
	cc := s.newCompactionChain()
	mock := newMockTSigVerifier(true)
	for i := 0; i < cc.tsigVerifier.cacheSize; i++ {
		cc.tsigVerifier.verifier[uint64(i)] = mock
	}
	now := time.Now().UTC()
	blocks := make([]*types.Block, 20)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Finalization: types.FinalizationResult{
				Timestamp: now,
				Height:    uint64(idx + 1),
			},
		}
		now = now.Add(100 * time.Millisecond)
		if idx > 0 {
			blocks[idx].Finalization.ParentHash = blocks[idx-1].Hash
		}
		if idx > 10 {
			blocks[idx].Finalization.Height = 0
		}
	}
	cc.init(blocks[1])
	s.Require().NoError(cc.processBlock(blocks[11]))
	s.Len(cc.extractBlocks(), 0)
	for i := 2; i <= 10; i++ {
		cc.processFinalizedBlock(blocks[i])
	}
	s.Require().Len(cc.extractFinalizedBlocks(), 9)
	// Syncing is almost done here. The finalized block matches the first block
	// processed.
	b11 := blocks[11].Clone()
	b11.Finalization.Height = uint64(12)
	cc.processFinalizedBlock(b11)
	s.Require().Len(cc.extractFinalizedBlocks(), 1)
	s.Len(cc.extractBlocks(), 0)
	// Sync is done.
	s.Require().NoError(cc.processBlock(blocks[12]))
	confirmed := cc.extractBlocks()
	s.Require().Len(confirmed, 1)
	s.Equal(confirmed[0].Hash, blocks[12].Hash)
	s.Equal(blocks[11].Hash, blocks[12].Finalization.ParentHash)
	s.Equal(uint64(13), blocks[12].Finalization.Height)
	for i := 13; i < 20; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
	}
	confirmed = cc.extractBlocks()
	s.Require().Len(confirmed, 7)
	offset := 13
	for i, b := range confirmed {
		s.Equal(blocks[offset+i].Hash, b.Hash)
		s.Equal(blocks[offset+i-1].Hash, b.Finalization.ParentHash)
		s.Equal(uint64(offset+i+1), b.Finalization.Height)
	}
}

func (s *CompactionChainTestSuite) TestBootstrapSync() {
	cc := s.newCompactionChain()
	numChains := cc.gov.Configuration(uint64(0)).NumChains
	s.Require().Equal(uint32(4), numChains)
	mock := newMockTSigVerifier(true)
	for i := 0; i < cc.tsigVerifier.cacheSize; i++ {
		cc.tsigVerifier.verifier[uint64(i)] = mock
	}
	now := time.Now().UTC()
	blocks := make([]*types.Block, 20)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Position: types.Position{
				Height: uint64(idx) / uint64(numChains),
			},
			Finalization: types.FinalizationResult{
				Timestamp: now,
				Height:    uint64(idx + 1),
			},
		}
		now = now.Add(100 * time.Millisecond)
		if idx > 0 {
			blocks[idx].Finalization.ParentHash = blocks[idx-1].Hash
		}
		if idx > 2 {
			blocks[idx].Finalization.Height = 0
		}
	}
	cc.init(&types.Block{})
	b2 := blocks[2].Clone()
	b2.Finalization.Height = 0
	s.Require().NoError(cc.processBlock(b2))
	s.Require().NoError(cc.processBlock(blocks[3]))
	s.Len(cc.extractBlocks(), 0)
	cc.processFinalizedBlock(blocks[2])
	cc.processFinalizedBlock(blocks[1])
	s.Require().Len(cc.extractFinalizedBlocks(), 0)
	s.Require().Len(cc.extractBlocks(), 0)
	cc.processFinalizedBlock(blocks[0])
	confirmed := cc.extractFinalizedBlocks()
	s.Require().Len(confirmed, 3)
	s.Equal(confirmed[0].Hash, blocks[0].Hash)
	s.Equal(confirmed[1].Hash, blocks[1].Hash)
	s.Equal(confirmed[2].Hash, blocks[2].Hash)
	confirmed = cc.extractBlocks()
	s.Require().Len(confirmed, 1)
	s.Equal(confirmed[0].Hash, blocks[3].Hash)
	s.Equal(blocks[2].Hash, blocks[3].Finalization.ParentHash)
	s.Equal(uint64(4), blocks[3].Finalization.Height)
}

func TestCompactionChain(t *testing.T) {
	suite.Run(t, new(CompactionChainTestSuite))
}
