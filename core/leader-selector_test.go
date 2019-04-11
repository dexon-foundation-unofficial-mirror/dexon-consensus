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

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

type LeaderSelectorTestSuite struct {
	suite.Suite
	mockValidLeaderDefault bool
	mockValidLeaderDB      map[common.Hash]bool
	mockValidLeader        validLeaderFn
}

func (s *LeaderSelectorTestSuite) SetupTest() {
	s.mockValidLeaderDefault = true
	s.mockValidLeaderDB = make(map[common.Hash]bool)
	s.mockValidLeader = func(b *types.Block, _ common.Hash) (bool, error) {
		if ret, exist := s.mockValidLeaderDB[b.Hash]; exist {
			return ret, nil
		}
		return s.mockValidLeaderDefault, nil
	}
}

func (s *LeaderSelectorTestSuite) newLeader() *leaderSelector {
	l := newLeaderSelector(s.mockValidLeader, &common.NullLogger{})
	l.restart(common.NewRandomHash())
	return l
}

func (s *LeaderSelectorTestSuite) TestDistance() {
	leader := s.newLeader()
	hash := common.NewRandomHash()
	prv, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	sig, err := prv.Sign(hash)
	s.Require().NoError(err)
	dis := leader.distance(sig)
	s.Equal(-1, dis.Cmp(maxHash))
}

func (s *LeaderSelectorTestSuite) TestProbability() {
	leader := s.newLeader()
	prv1, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	prv2, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	for {
		hash := common.NewRandomHash()
		sig1, err := prv1.Sign(hash)
		s.Require().NoError(err)
		sig2, err := prv2.Sign(hash)
		s.Require().NoError(err)
		dis1 := leader.distance(sig1)
		dis2 := leader.distance(sig2)
		prob1 := leader.probability(sig1)
		prob2 := leader.probability(sig2)
		s.True(prob1 <= 1 && prob1 >= 0)
		s.True(prob2 <= 1 && prob2 >= 0)
		cmp := dis1.Cmp(dis2)
		if cmp == 0 {
			s.True(dis1.Cmp(dis2) == 0)
			continue
		}
		if cmp == 1 {
			s.True(prob2 > prob1)
		} else if cmp == -1 {
			s.True(prob2 < prob1)
		}
		break
	}
}

func (s *LeaderSelectorTestSuite) TestLeaderBlockHash() {
	leader := s.newLeader()
	blocks := make(map[common.Hash]*types.Block)
	for i := 0; i < 10; i++ {
		prv, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		block := &types.Block{
			ProposerID: types.NewNodeID(prv.PublicKey()),
			Hash:       common.NewRandomHash(),
		}
		s.Require().NoError(
			utils.NewSigner(prv).SignCRS(block, leader.hashCRS))
		s.Require().NoError(leader.processBlock(block))
		blocks[block.Hash] = block
	}
	blockHash := leader.leaderBlockHash()
	leaderBlock, exist := blocks[blockHash]
	s.Require().True(exist)
	leaderDist := leader.distance(leaderBlock.CRSSignature)
	for _, block := range blocks {
		if block == leaderBlock {
			continue
		}
		dist := leader.distance(block.CRSSignature)
		s.Equal(-1, leaderDist.Cmp(dist))
	}
}

func (s *LeaderSelectorTestSuite) TestValidLeaderFn() {
	leader := s.newLeader()
	blocks := make(map[common.Hash]*types.Block)
	for i := 0; i < 10; i++ {
		prv, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		block := &types.Block{
			ProposerID: types.NewNodeID(prv.PublicKey()),
			Hash:       common.NewRandomHash(),
		}
		s.Require().NoError(
			utils.NewSigner(prv).SignCRS(block, leader.hashCRS))
		s.Require().NoError(leader.processBlock(block))
		blocks[block.Hash] = block
	}
	blockHash := leader.leaderBlockHash()

	s.mockValidLeaderDB[blockHash] = false
	leader.restart(leader.hashCRS)
	for _, b := range blocks {
		s.Require().NoError(leader.processBlock(b))
	}
	s.NotEqual(blockHash, leader.leaderBlockHash())
	s.mockValidLeaderDB[blockHash] = true
	s.Equal(blockHash, leader.leaderBlockHash())
	s.Len(leader.pendingBlocks, 0)
}

func (s *LeaderSelectorTestSuite) TestPotentialLeader() {
	leader := s.newLeader()
	blocks := make(map[common.Hash]*types.Block)
	for i := 0; i < 10; i++ {
		if i > 0 {
			s.mockValidLeaderDefault = false
		}
		prv, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		block := &types.Block{
			ProposerID: types.NewNodeID(prv.PublicKey()),
			Hash:       common.NewRandomHash(),
		}
		s.Require().NoError(
			utils.NewSigner(prv).SignCRS(block, leader.hashCRS))
		ok, _ := leader.potentialLeader(block)
		s.Require().NoError(leader.processBlock(block))
		if i > 0 {
			if ok {
				s.Contains(leader.pendingBlocks, block.Hash)
			} else {
				s.NotContains(leader.pendingBlocks, block.Hash)
			}
			blocks[block.Hash] = block
		}
	}
}

func TestLeaderSelector(t *testing.T) {
	suite.Run(t, new(LeaderSelectorTestSuite))
}
