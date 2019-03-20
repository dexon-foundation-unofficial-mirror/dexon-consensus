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
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

type testTSigVerifier struct{}

func (v *testTSigVerifier) VerifySignature(hash common.Hash,
	sig crypto.Signature) bool {
	return true
}

type testTSigVerifierGetter struct{}

func (t *testTSigVerifierGetter) UpdateAndGet(round uint64) (
	TSigVerifier, bool, error) {
	return &testTSigVerifier{}, true, nil
}

type BlockChainTestSuite struct {
	suite.Suite

	nID           types.NodeID
	signer        *utils.Signer
	dMoment       time.Time
	blockInterval time.Duration
}

func (s *BlockChainTestSuite) SetupSuite() {
	prvKeys, pubKeys, err := test.NewKeys(1)
	s.Require().NoError(err)
	s.nID = types.NewNodeID(pubKeys[0])
	s.signer = utils.NewSigner(prvKeys[0])
	s.dMoment = time.Now().UTC()
	s.blockInterval = 1 * time.Millisecond
}

func (s *BlockChainTestSuite) newBlocks(c uint64, initBlock *types.Block) (
	blocks []*types.Block) {
	parentHash := common.Hash{}
	baseHeight := uint64(0)
	t := s.dMoment
	initRound := uint64(0)
	if initBlock != nil {
		parentHash = initBlock.Hash
		t = initBlock.Timestamp.Add(s.blockInterval)
		initRound = initBlock.Position.Round
		baseHeight = initBlock.Position.Height + 1
	}
	for i := uint64(0); i < uint64(c); i++ {
		b := &types.Block{
			ParentHash: parentHash,
			Position:   types.Position{Round: initRound, Height: baseHeight + i},
			Timestamp:  t,
		}
		if b.Position.Round >= DKGDelayRound {
			b.Finalization.Randomness = common.GenerateRandomBytes()
		}
		s.Require().NoError(s.signer.SignBlock(b))
		blocks = append(blocks, b)
		parentHash = b.Hash
		t = t.Add(s.blockInterval)
	}
	return
}

func (s *BlockChainTestSuite) newEmptyBlock(parent *types.Block,
	blockInterval time.Duration) *types.Block {
	emptyB := &types.Block{
		ParentHash: parent.Hash,
		Position: types.Position{
			Round:  parent.Position.Round,
			Height: parent.Position.Height + 1,
		},
		Timestamp: parent.Timestamp.Add(blockInterval),
	}
	var err error
	emptyB.Hash, err = utils.HashBlock(emptyB)
	s.Require().NoError(err)
	return emptyB
}

func (s *BlockChainTestSuite) newBlock(parent *types.Block, round uint64,
	blockInterval time.Duration) *types.Block {
	b := &types.Block{
		ParentHash: parent.Hash,
		Position: types.Position{
			Round:  round,
			Height: parent.Position.Height + 1,
		},
		Timestamp: parent.Timestamp.Add(blockInterval),
	}
	if b.Position.Round >= DKGDelayRound {
		b.Finalization.Randomness = common.GenerateRandomBytes()
	}
	s.Require().NoError(s.signer.SignBlock(b))
	return b
}

func (s *BlockChainTestSuite) newBlockChain(initB *types.Block,
	roundLength uint64) (bc *blockChain) {
	initRound := uint64(0)
	if initB != nil {
		initRound = initB.Position.Round
	}
	initHeight := uint64(0)
	if initB != nil {
		initHeight = initB.Position.Height
	}
	bc = newBlockChain(s.nID, s.dMoment, initB, test.NewApp(0, nil, nil),
		&testTSigVerifierGetter{}, s.signer, &common.NullLogger{})
	s.Require().NoError(bc.notifyRoundEvents([]utils.RoundEventParam{
		utils.RoundEventParam{
			Round:       initRound,
			Reset:       0,
			BeginHeight: initHeight,
			Config: &types.Config{
				MinBlockInterval: s.blockInterval,
				RoundLength:      roundLength,
			}}}))
	return
}

func (s *BlockChainTestSuite) newRoundOneInitBlock() *types.Block {
	initBlock := &types.Block{
		ParentHash: common.NewRandomHash(),
		Position:   types.Position{Round: 1},
		Timestamp:  s.dMoment,
	}
	s.Require().NoError(s.signer.SignBlock(initBlock))
	return initBlock
}

func (s *BlockChainTestSuite) TestBasicUsage() {
	initBlock := s.newRoundOneInitBlock()
	bc := s.newBlockChain(initBlock, 10)
	// test scenario: block, empty block, randomness can be added in any order
	// of position.
	blocks := s.newBlocks(4, initBlock)
	b0, b1, b2, b3 := blocks[0], blocks[1], blocks[2], blocks[3]
	// generate block-5 after block-4, which is an empty block.
	b4 := s.newEmptyBlock(b3, time.Millisecond)
	b5 := &types.Block{
		ParentHash: b4.Hash,
		Position:   types.Position{Round: 1, Height: b4.Position.Height + 1},
		Finalization: types.FinalizationResult{
			Randomness: common.GenerateRandomBytes(),
		},
	}
	s.Require().NoError(s.signer.SignBlock(b5))
	s.Require().NoError(bc.addBlock(b5))
	emptyB, err := bc.addEmptyBlock(b4.Position)
	s.Require().Nil(emptyB)
	s.Require().NoError(err)
	s.Require().NoError(bc.addBlock(b3))
	s.Require().NoError(bc.addBlock(b2))
	s.Require().NoError(bc.addBlock(b1))
	s.Require().NoError(bc.addBlock(b0))
	extracted := bc.extractBlocks()
	s.Require().Len(extracted, 6)
	s.Require().Equal(extracted[4].Hash, b4.Hash)
	extracted = bc.extractBlocks()
	s.Require().Len(extracted, 0)
}

func (s *BlockChainTestSuite) TestSanityCheck() {
	bc := s.newBlockChain(nil, 4)
	// Empty block is not allowed.
	s.Require().Panics(func() {
		bc.sanityCheck(&types.Block{})
	})
	blocks := s.newBlocks(3, nil)
	b0, b1, b2 := blocks[0], blocks[1], blocks[2]
	// ErrNotGenesisBlock
	s.Require().Equal(ErrNotGenesisBlock.Error(), bc.sanityCheck(b1).Error())
	// Genesis block should pass sanity check.
	s.Require().NoError(bc.sanityCheck(b0))
	s.Require().NoError(bc.addBlock(b0))
	// ErrIsGenesisBlock
	s.Require().Equal(ErrIsGenesisBlock.Error(), bc.sanityCheck(b0).Error())
	// ErrRetrySanityCheckLater
	s.Require().Equal(
		ErrRetrySanityCheckLater.Error(), bc.sanityCheck(b2).Error())
	// ErrInvalidBlockHeight
	s.Require().NoError(bc.addBlock(b1))
	s.Require().NoError(bc.addBlock(b2))
	s.Require().Equal(
		ErrInvalidBlockHeight.Error(), bc.sanityCheck(b1).Error())
	// ErrInvalidRoundID
	// Should not switch round when tip is not the last block.
	s.Require().Equal(
		ErrInvalidRoundID.Error(),
		bc.sanityCheck(s.newBlock(b2, 1, 1*time.Second)).Error())
	b3 := s.newBlock(b2, 0, 100*time.Second)
	s.Require().NoError(bc.addBlock(b3))
	// Should switch round when tip is the last block.
	s.Require().Equal(
		ErrRoundNotSwitch.Error(),
		bc.sanityCheck(s.newBlock(b3, 0, 1*time.Second)).Error())
	// ErrIncorrectParentHash
	b4 := &types.Block{
		ParentHash: b2.Hash,
		Position: types.Position{
			Round:  1,
			Height: 4,
		},
		Timestamp: b3.Timestamp.Add(1 * time.Second),
	}
	s.Require().NoError(s.signer.SignBlock(b4))
	s.Require().Equal(
		ErrIncorrectParentHash.Error(), bc.sanityCheck(b4).Error())
	// There is no valid signature attached.
	b4.ParentHash = b3.Hash
	s.Require().Error(bc.sanityCheck(b4))
	// OK case.
	s.Require().NoError(s.signer.SignBlock(b4))
	s.Require().NoError(bc.sanityCheck(b4))
}

func (s *BlockChainTestSuite) TestNotifyRoundEvents() {
	roundLength := uint64(10)
	bc := s.newBlockChain(nil, roundLength)
	newEvent := func(round, reset, height uint64) []utils.RoundEventParam {
		return []utils.RoundEventParam{
			utils.RoundEventParam{
				Round:       round,
				Reset:       reset,
				BeginHeight: height,
				CRS:         common.Hash{},
				Config:      &types.Config{RoundLength: roundLength},
			}}
	}
	s.Require().Equal(ErrInvalidRoundID.Error(),
		bc.notifyRoundEvents(newEvent(2, 0, roundLength)).Error())
	s.Require().NoError(bc.notifyRoundEvents(newEvent(1, 0, roundLength)))
	// Make sure new config is appended when new round is ready.
	s.Require().Len(bc.configs, 2)
	s.Require().Equal(ErrInvalidRoundID.Error(),
		bc.notifyRoundEvents(newEvent(3, 1, roundLength*2)).Error())
	s.Require().Equal(ErrInvalidBlockHeight.Error(),
		bc.notifyRoundEvents(newEvent(1, 1, roundLength)).Error())
	s.Require().NoError(bc.notifyRoundEvents(newEvent(1, 1, roundLength*2)))
	// Make sure roundEndHeight is extended when DKG reset.
	s.Require().Equal(bc.configs[len(bc.configs)-1].RoundEndHeight(),
		roundLength*3)
}

func (s *BlockChainTestSuite) TestConfirmed() {
	bc := s.newBlockChain(nil, 10)
	blocks := s.newBlocks(3, nil)
	// Add a confirmed block.
	s.Require().NoError(bc.addBlock(blocks[0]))
	// Add a pending block.
	s.Require().NoError(bc.addBlock(blocks[2]))
	s.Require().True(bc.confirmed(0))
	s.Require().False(bc.confirmed(1))
	s.Require().True(bc.confirmed(2))
}

func (s *BlockChainTestSuite) TestNextBlockAndTipRound() {
	var roundLength uint64 = 3
	bc := s.newBlockChain(nil, roundLength)
	s.Require().NoError(bc.notifyRoundEvents([]utils.RoundEventParam{
		utils.RoundEventParam{
			Round:       1,
			Reset:       0,
			BeginHeight: roundLength,
			CRS:         common.Hash{},
			Config: &types.Config{
				MinBlockInterval: s.blockInterval,
				RoundLength:      roundLength,
			}}}))
	blocks := s.newBlocks(3, nil)
	nextH, nextT := bc.nextBlock()
	s.Require().Equal(nextH, uint64(0))
	s.Require().Equal(nextT, s.dMoment)
	// Add one block.
	s.Require().NoError(bc.addBlock(blocks[0]))
	nextH, nextT = bc.nextBlock()
	s.Require().Equal(nextH, uint64(1))
	s.Require().Equal(
		nextT, blocks[0].Timestamp.Add(bc.configs[0].minBlockInterval))
	// Add one block, expected to be pending.
	s.Require().NoError(bc.addBlock(blocks[2]))
	nextH2, nextT2 := bc.nextBlock()
	s.Require().Equal(nextH, nextH2)
	s.Require().Equal(nextT, nextT2)
	// Add a block, which is the last block of this round.
	b3 := s.newBlock(blocks[2], 1, 1*time.Second)
	s.Require().NoError(bc.addBlock(blocks[1]))
	s.Require().NoError(bc.sanityCheck(b3))
	s.Require().NoError(bc.addBlock(b3))
	s.Require().Equal(bc.tipRound(), uint64(1))
}

func (s *BlockChainTestSuite) TestLastXBlock() {
	initBlock := s.newRoundOneInitBlock()
	bc := s.newBlockChain(initBlock, 10)
	s.Require().Nil(bc.lastPendingBlock())
	s.Require().True(bc.lastDeliveredBlock() == initBlock)
	blocks := s.newBlocks(2, initBlock)
	s.Require().NoError(bc.addBlock(blocks[0]))
	s.Require().True(bc.lastPendingBlock() == blocks[0])
	s.Require().True(bc.lastDeliveredBlock() == initBlock)
	s.Require().Len(bc.extractBlocks(), 1)
	s.Require().Nil(bc.lastPendingBlock())
	s.Require().True(bc.lastDeliveredBlock() == blocks[0])
	s.Require().NoError(bc.addBlock(blocks[1]))
	s.Require().True(bc.lastPendingBlock() == blocks[1])
	s.Require().True(bc.lastDeliveredBlock() == blocks[0])
}

func (s *BlockChainTestSuite) TestPendingBlockRecords() {
	bs := s.newBlocks(5, nil)
	ps := pendingBlockRecords{}
	s.Require().NoError(ps.insert(pendingBlockRecord{bs[2].Position, bs[2]}))
	s.Require().NoError(ps.insert(pendingBlockRecord{bs[1].Position, bs[1]}))
	s.Require().NoError(ps.insert(pendingBlockRecord{bs[0].Position, bs[0]}))
	s.Require().Equal(ErrDuplicatedPendingBlock.Error(),
		ps.insert(pendingBlockRecord{bs[0].Position, nil}).Error())
	s.Require().True(ps[0].position.Equal(bs[0].Position))
	s.Require().True(ps[1].position.Equal(bs[1].Position))
	s.Require().True(ps[2].position.Equal(bs[2].Position))
	s.Require().NoError(ps.insert(pendingBlockRecord{bs[4].Position, bs[4]}))
	// Here assume block3 is empty, since we didn't verify parent hash in
	// pendingBlockRecords, it should be fine.
	s.Require().NoError(ps.insert(pendingBlockRecord{bs[3].Position, nil}))
	s.Require().True(ps[3].position.Equal(bs[3].Position))
	s.Require().True(ps[4].position.Equal(bs[4].Position))
}

func (s *BlockChainTestSuite) TestFindPendingBlock() {
	bc := s.newBlockChain(nil, 10)
	blocks := s.newBlocks(7, nil)
	s.Require().NoError(bc.addBlock(blocks[6]))
	s.Require().NoError(bc.addBlock(blocks[5]))
	s.Require().NoError(bc.addBlock(blocks[3]))
	s.Require().NoError(bc.addBlock(blocks[2]))
	s.Require().NoError(bc.addBlock(blocks[1]))
	s.Require().NoError(bc.addBlock(blocks[0]))
	s.Require().True(bc.findPendingBlock(blocks[0].Position) == blocks[0])
	s.Require().True(bc.findPendingBlock(blocks[1].Position) == blocks[1])
	s.Require().True(bc.findPendingBlock(blocks[2].Position) == blocks[2])
	s.Require().True(bc.findPendingBlock(blocks[3].Position) == blocks[3])
	s.Require().Nil(bc.findPendingBlock(blocks[4].Position))
	s.Require().True(bc.findPendingBlock(blocks[5].Position) == blocks[5])
	s.Require().True(bc.findPendingBlock(blocks[6].Position) == blocks[6])
}

func (s *BlockChainTestSuite) TestAddEmptyBlockDirectly() {
	bc := s.newBlockChain(nil, 10)
	blocks := s.newBlocks(1, nil)
	s.Require().NoError(bc.addBlock(blocks[0]))
	// Add an empty block after a normal block.
	pos := types.Position{Height: 1}
	emptyB1, err := bc.addEmptyBlock(pos)
	s.Require().NotNil(emptyB1)
	s.Require().True(emptyB1.Position.Equal(pos))
	s.Require().NoError(err)
	// Add an empty block after an empty block.
	pos = types.Position{Height: 2}
	emptyB2, err := bc.addEmptyBlock(pos)
	s.Require().NotNil(emptyB2)
	s.Require().True(emptyB2.Position.Equal(pos))
	s.Require().NoError(err)
	// prepare a normal block.
	pos = types.Position{Height: 3}
	b3, err := bc.proposeBlock(pos, emptyB2.Timestamp.Add(s.blockInterval), false)
	s.Require().NotNil(b3)
	s.Require().NoError(err)
	// Add an empty block far away from current tip.
	pos = types.Position{Height: 4}
	emptyB4, err := bc.addEmptyBlock(pos)
	s.Require().Nil(emptyB4)
	s.Require().NoError(err)
	// propose an empty block based on the block at height=3, which mimics the
	// scenario that the empty block is pulled from others.
	emptyB4 = &types.Block{
		ParentHash: b3.Hash,
		Position:   pos,
		Timestamp:  b3.Timestamp.Add(s.blockInterval),
		Witness: types.Witness{
			Height: b3.Witness.Height,
			Data:   b3.Witness.Data, // Hacky, don't worry.
		},
	}
	emptyB4.Hash, err = utils.HashBlock(emptyB4)
	s.Require().NoError(err)
	s.Require().NoError(bc.addBlock(emptyB4))
	rec, found := bc.pendingBlocks.searchByHeight(4)
	s.Require().True(found)
	s.Require().NotNil(rec.block)
}

func TestBlockChain(t *testing.T) {
	suite.Run(t, new(BlockChainTestSuite))
}
