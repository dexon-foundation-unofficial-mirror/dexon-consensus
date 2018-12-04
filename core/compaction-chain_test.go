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

func (s *CompactionChainTestSuite) newCompactionChain() (
	*compactionChain, *mockTSigVerifier) {
	_, pubKeys, err := test.NewKeys(4)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(
		pubKeys, 100*time.Millisecond, ConfigRoundShift)
	s.Require().NoError(err)
	cc := newCompactionChain(gov)
	cc.init(&types.Block{})

	mock := newMockTSigVerifier(true)
	for i := 0; i < cc.tsigVerifier.cacheSize; i++ {
		cc.tsigVerifier.verifier[uint64(i)] = mock
	}

	return cc, mock
}

func (s *CompactionChainTestSuite) TestProcessBlock() {
	cc, _ := s.newCompactionChain()
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
	s.Len(cc.pendingBlocks, len(blocks))
}

func (s *CompactionChainTestSuite) TestExtractBlocks() {
	cc, _ := s.newCompactionChain()
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
				Position:   blocks[i].Position,
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
				Position:   blocks[i].Position,
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
				Position:   blocks[i].Position,
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
				Position:   blocks[i].Position,
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

func (s *CompactionChainTestSuite) TestMissedRandomness() {
	// This test case makes sure a block's randomness field can be fulfilled by
	// calling:
	//  - core.compactionChain.processBlockRandomnessResult
	//  - core.compactionChain.processFinalizedBlock
	cc, _ := s.newCompactionChain()
	s.Require().Equal(uint32(4), cc.gov.Configuration(uint64(0)).NumChains)
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Position: types.Position{
				Round:   1,
				Height:  uint64(idx / 4),
				ChainID: uint32(idx % 4),
			},
		}
		s.Require().False(cc.blockRegistered(blocks[idx].Hash))
		cc.registerBlock(blocks[idx])
		s.Require().True(cc.blockRegistered(blocks[idx].Hash))
	}
	// Block#4, #5, contains randomness.
	for i := range blocks {
		s.Require().NoError(cc.processBlock(blocks[i]))
		if i >= 4 && i < 6 {
			h := common.NewRandomHash()
			s.Require().NoError(cc.processBlockRandomnessResult(
				&types.BlockRandomnessResult{
					BlockHash:  blocks[i].Hash,
					Position:   blocks[i].Position,
					Randomness: h[:],
				}))
		}
	}
	s.Require().Len(cc.extractBlocks(), 0)
	// Give compactionChain module randomnessResult via finalized block
	// #0, #1, #2, #3, #4.
	for i := range blocks {
		if i >= 4 {
			break
		}
		block := blocks[i].Clone()
		h := common.NewRandomHash()
		block.Finalization.Randomness = h[:]
		block.Finalization.Height = uint64(i + 1)
		cc.processFinalizedBlock(block)
	}
	delivered := cc.extractBlocks()
	s.Require().Len(delivered, 6)
	// Give compactionChain module randomnessResult#6-9.
	for i := 6; i < 10; i++ {
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Position:   blocks[i].Position,
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
	cc, _ := s.newCompactionChain()
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

func (s *CompactionChainTestSuite) TestBootstrapSync() {
	// This test case make sure compactionChain module would only deliver
	// blocks unless tips of each chain are received, when this module is
	// initialized with a block with finalizationHeight == 0.
	cc, _ := s.newCompactionChain()
	numChains := cc.gov.Configuration(uint64(0)).NumChains
	s.Require().Equal(uint32(4), numChains)
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
	}
	s.Require().NoError(cc.processBlock(blocks[1]))
	s.Len(cc.extractBlocks(), 0)
	s.Require().NoError(cc.processBlock(blocks[2]))
	s.Len(cc.extractBlocks(), 0)
	// Although genesis block is received, we can't deliver them until tip blocks
	// of each chain is received.
	s.Require().NoError(cc.processBlock(blocks[0]))
	s.Len(cc.extractBlocks(), 0)
	// Once we receive the tip of chain#3 then we can deliver all tips.
	s.Require().NoError(cc.processBlock(blocks[3]))
	confirmed := cc.extractBlocks()
	s.Require().Len(confirmed, 4)
	s.Equal(confirmed[0].Hash, blocks[1].Hash)
	s.Equal(blocks[1].Finalization.Height, uint64(1))
	s.Equal(confirmed[1].Hash, blocks[2].Hash)
	s.Equal(blocks[2].Finalization.Height, uint64(2))
	s.Equal(confirmed[2].Hash, blocks[0].Hash)
	s.Equal(blocks[0].Finalization.Height, uint64(3))
	s.Equal(confirmed[3].Hash, blocks[3].Hash)
	s.Equal(blocks[3].Finalization.Height, uint64(4))
}

func TestCompactionChain(t *testing.T) {
	suite.Run(t, new(CompactionChainTestSuite))
}
