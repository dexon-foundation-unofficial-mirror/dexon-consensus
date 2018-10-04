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
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type LatticeDataTestSuite struct {
	suite.Suite
}

// genTestCase1 generates test case 1,
//  3
//  |
//  2
//  | \
//  1  |     1
//  |  |     |
//  0  0  0  0 (block height)
//  0  1  2  3 (validator)
func (s *LatticeDataTestSuite) genTestCase1() (data *latticeData) {
	// Create new reliableBroadcast instance with 4 validators
	var (
		round     uint64
		b         *types.Block
		delivered []*types.Block
		h         common.Hash
		chainNum  uint32 = 4
		req              = s.Require()
		err       error
	)

	data = newLatticeData(round, chainNum, 2*time.Nanosecond, 1000*time.Second)
	// Add genesis blocks.
	for i := uint32(0); i < chainNum; i++ {
		b = s.prepareGenesisBlock(i)
		delivered, err = data.addBlock(b)
		// Genesis blocks are safe to be added to DAG, they acks no one.
		req.Len(delivered, 1)
		req.Nil(err)
	}

	// Add block 0-1 which acks 0-0.
	h = data.chains[0].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Hash:       common.NewRandomHash(),
		Timestamp:  time.Now().UTC(),
		Position: types.Position{
			ChainID: 0,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{h}),
		Witness: types.Witness{
			Height: 1,
		},
	}
	s.hashBlock(b)
	delivered, err = data.addBlock(b)
	req.Len(delivered, 1)
	req.Equal(delivered[0].Hash, b.Hash)
	req.Nil(err)
	req.NotNil(data.chains[0].getBlockByHeight(1))

	// Add block 0-2 which acks 0-1 and 1-0.
	h = data.chains[0].getBlockByHeight(1).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 0,
			Height:  2,
		},
		Timestamp: time.Now().UTC(),
		Acks: common.NewSortedHashes(common.Hashes{
			h,
			data.chains[1].getBlockByHeight(0).Hash,
		}),
		Witness: types.Witness{
			Height: 2,
		},
	}
	s.hashBlock(b)
	delivered, err = data.addBlock(b)
	req.Len(delivered, 1)
	req.Equal(delivered[0].Hash, b.Hash)
	req.Nil(err)
	req.NotNil(data.chains[0].getBlockByHeight(2))

	// Add block 0-3 which acks 0-2.
	h = data.chains[0].getBlockByHeight(2).Hash
	b = &types.Block{
		ParentHash: h,
		Hash:       common.NewRandomHash(),
		Timestamp:  time.Now().UTC(),
		Position: types.Position{
			ChainID: 0,
			Height:  3,
		},
		Acks: common.NewSortedHashes(common.Hashes{h}),
		Witness: types.Witness{
			Height: 3,
		},
	}
	s.hashBlock(b)
	delivered, err = data.addBlock(b)
	req.Len(delivered, 1)
	req.Equal(delivered[0].Hash, b.Hash)
	req.Nil(err)
	req.NotNil(data.chains[0].getBlockByHeight(3))

	// Add block 3-1 which acks 3-0.
	h = data.chains[3].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Hash:       common.NewRandomHash(),
		Timestamp:  time.Now().UTC(),
		Position: types.Position{
			ChainID: 3,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{h}),
		Witness: types.Witness{
			Height: 1,
		},
	}
	s.hashBlock(b)
	delivered, err = data.addBlock(b)
	req.Len(delivered, 1)
	req.Equal(delivered[0].Hash, b.Hash)
	req.Nil(err)
	req.NotNil(data.chains[3].getBlockByHeight(0))
	return
}

// hashBlock is a helper to hash a block and check if any error.
func (s *LatticeDataTestSuite) hashBlock(b *types.Block) {
	var err error
	b.Hash, err = hashBlock(b)
	s.Require().Nil(err)
}

func (s *LatticeDataTestSuite) prepareGenesisBlock(
	chainID uint32) (b *types.Block) {

	b = &types.Block{
		ParentHash: common.Hash{},
		Position: types.Position{
			ChainID: chainID,
			Height:  0,
		},
		Acks:      common.NewSortedHashes(common.Hashes{}),
		Timestamp: time.Now().UTC(),
	}
	s.hashBlock(b)
	return
}

func (s *LatticeDataTestSuite) TestSanityCheckInDataLayer() {
	var (
		b    *types.Block
		h    common.Hash
		data = s.genTestCase1()
		req  = s.Require()
		err  error
	)

	// Non-genesis block with no ack, should get error.
	b = &types.Block{
		ParentHash: common.NewRandomHash(),
		Position: types.Position{
			ChainID: 0,
			Height:  10,
		},
		Acks: common.NewSortedHashes(common.Hashes{}),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(ErrNotAckParent.Error(), err.Error())

	// Non-genesis block which acks its parent but the height is invalid.
	h = data.chains[1].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 1,
			Height:  2,
		},
		Acks: common.NewSortedHashes(common.Hashes{h}),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(ErrInvalidBlockHeight.Error(), err.Error())

	// Invalid chain ID.
	h = data.chains[1].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 100,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{h}),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(ErrInvalidChainID.Error(), err.Error())

	// Fork block.
	h = data.chains[0].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 0,
			Height:  1,
		},
		Acks:      common.NewSortedHashes(common.Hashes{h}),
		Timestamp: time.Now().UTC(),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(ErrForkBlock.Error(), err.Error())

	// Replicated ack.
	h = data.chains[0].getBlockByHeight(3).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 0,
			Height:  4,
		},
		Acks: common.NewSortedHashes(common.Hashes{
			h,
			data.chains[1].getBlockByHeight(0).Hash,
		}),
		Timestamp: time.Now().UTC(),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(ErrDoubleAck.Error(), err.Error())

	// Acking block doesn't exists.
	h = data.chains[1].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 1,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{
			h,
			common.NewRandomHash(),
		}),
		Timestamp: time.Now().UTC(),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(err.Error(), ErrAckingBlockNotExists.Error())

	// Parent block on different chain.
	h = data.chains[1].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 2,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{
			h,
			data.chains[2].getBlockByHeight(0).Hash,
		}),
		Timestamp: time.Now().UTC(),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(err.Error(), ErrInvalidParentChain.Error())

	// Ack two blocks on the same chain.
	h = data.chains[2].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 2,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{
			h,
			data.chains[0].getBlockByHeight(0).Hash,
			data.chains[0].getBlockByHeight(1).Hash,
		}),
		Timestamp: time.Now().UTC(),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(err.Error(), ErrDuplicatedAckOnOneChain.Error())

	// Witness height decreases.
	h = data.chains[0].getBlockByHeight(3).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 0,
			Height:  4,
		},
		Timestamp: time.Now().UTC(),
		Acks: common.NewSortedHashes(common.Hashes{
			h,
		}),
		Witness: types.Witness{
			Height: 2,
		},
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(err.Error(), ErrInvalidWitness.Error())

	// Add block 3-1 which acks 3-0, and violet reasonable block time interval.
	h = data.chains[2].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Hash:       common.NewRandomHash(),
		Timestamp:  time.Now().UTC().Add(data.maxBlockTimeInterval),
		Position: types.Position{
			ChainID: 2,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{h}),
	}
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(err, ErrIncorrectBlockTime)
	// Violet minimum block time interval.
	b.Timestamp =
		data.chains[2].getBlockByHeight(0).Timestamp.Add(1 * time.Nanosecond)
	s.hashBlock(b)
	err = data.sanityCheck(b)
	req.NotNil(err)
	req.Equal(err, ErrIncorrectBlockTime)

	// Normal block.
	h = data.chains[1].getBlockByHeight(0).Hash
	b = &types.Block{
		ParentHash: h,
		Position: types.Position{
			ChainID: 1,
			Height:  1,
		},
		Acks:      common.NewSortedHashes(common.Hashes{h}),
		Timestamp: time.Now().UTC(),
	}
	s.hashBlock(b)
	req.Nil(data.sanityCheck(b))
}

func (s *LatticeDataTestSuite) TestRandomIntensiveAcking() {
	var (
		round     uint64
		chainNum  uint32 = 19
		data             = newLatticeData(round, chainNum, 0, 1000*time.Second)
		req              = s.Require()
		delivered []*types.Block
		extracted []*types.Block
		b         *types.Block
		err       error
	)

	// Generate genesis blocks.
	for i := uint32(0); i < chainNum; i++ {
		b = s.prepareGenesisBlock(i)
		delivered, err = data.addBlock(b)
		req.Len(delivered, 1)
		req.Nil(err)
	}

	for i := 0; i < 5000; i++ {
		b := &types.Block{
			Position: types.Position{
				ChainID: uint32(rand.Intn(int(chainNum))),
			},
			Timestamp: time.Now().UTC(),
		}
		data.prepareBlock(b)
		s.hashBlock(b)
		delivered, err = data.addBlock(b)
		req.Nil(err)
		extracted = append(extracted, delivered...)
	}

	// The len of array extractedBlocks should be about 5000.
	req.True(len(extracted) > 4500)
	// The len of data.blockInfos should be small if deleting mechanism works.
	req.True(len(data.blockByHash) < 500)
}

func (s *LatticeDataTestSuite) TestRandomlyGeneratedBlocks() {
	var (
		round     uint64
		chainNum  uint32 = 19
		blockNum         = 50
		repeat           = 20
		delivered []*types.Block
		err       error
		req       = s.Require()
		datum     []*latticeData
	)

	if testing.Short() {
		chainNum = 7
		repeat = 3
	}

	// Prepare a randomly generated blocks.
	db, err := blockdb.NewMemBackedBlockDB()
	req.Nil(err)
	gen := test.NewBlocksGenerator(nil, hashBlock)
	_, err = gen.Generate(chainNum, blockNum, nil, db)
	req.Nil(err)
	iter, err := db.GetAll()
	req.Nil(err)
	// Setup a revealer that would reveal blocks randomly but still form
	// valid DAG without holes.
	revealer, err := test.NewRandomDAGRevealer(iter)
	req.Nil(err)

	revealedHashesAsString := map[string]struct{}{}
	deliveredHashesAsString := map[string]struct{}{}
	for i := 0; i < repeat; i++ {
		data := newLatticeData(round, chainNum, 0, 1000*time.Second)
		deliveredHashes := common.Hashes{}
		revealedHashes := common.Hashes{}
		revealer.Reset()
		for {
			// Reveal next block.
			b, err := revealer.Next()
			if err != nil {
				if err == blockdb.ErrIterationFinished {
					err = nil
					break
				}
			}
			s.Require().Nil(err)
			revealedHashes = append(revealedHashes, b.Hash)

			// Pass blocks to lattice.
			delivered, err = data.addBlock(&b)
			req.Nil(err)
			for _, b := range delivered {
				deliveredHashes = append(deliveredHashes, b.Hash)
			}
		}
		// To make it easier to check, sort hashes of
		// strongly acked blocks, and concatenate them into
		// a string.
		sort.Sort(deliveredHashes)
		asString := ""
		for _, h := range deliveredHashes {
			asString += h.String() + ","
		}
		deliveredHashesAsString[asString] = struct{}{}
		// Compose revealing hash sequense to string.
		asString = ""
		for _, h := range revealedHashes {
			asString += h.String() + ","
		}
		revealedHashesAsString[asString] = struct{}{}
		datum = append(datum, data)
	}
	// Make sure concatenated hashes of strongly acked blocks are identical.
	req.Len(deliveredHashesAsString, 1)
	for h := range deliveredHashesAsString {
		// Make sure at least some blocks are strongly acked.
		req.True(len(h) > 0)
	}
	// Make sure we test for more than 1 revealing sequence.
	req.True(len(revealedHashesAsString) > 1)
	// Make sure each latticeData instance have identical working set.
	req.True(len(datum) >= repeat)
	for i, bI := range datum {
		for j, bJ := range datum {
			if i == j {
				continue
			}
			for chainID, statusI := range bI.chains {
				req.Equal(statusI.minHeight, bJ.chains[chainID].minHeight)
				req.Equal(statusI.nextOutput, bJ.chains[chainID].nextOutput)
				req.Equal(len(statusI.blocks), len(bJ.chains[chainID].blocks))
				// Check nextAck.
				for x, ackI := range statusI.nextAck {
					req.Equal(ackI, bJ.chains[chainID].nextAck[x])
				}
				// Check blocks.
				if len(statusI.blocks) > 0 {
					req.Equal(statusI.blocks[0], bJ.chains[chainID].blocks[0])
				}
			}
			// Check blockByHash.
			req.Equal(bI.blockByHash, bJ.blockByHash)
		}
	}
}

func (s *LatticeDataTestSuite) TestPrepareBlock() {
	var (
		round       uint64
		chainNum    uint32 = 4
		req                = s.Require()
		minInterval        = 50 * time.Millisecond
		delivered   []*types.Block
		err         error
		data        = newLatticeData(
			round, chainNum, 0, 3000*time.Second)
	)
	// Setup genesis blocks.
	b00 := s.prepareGenesisBlock(0)
	time.Sleep(minInterval)
	b10 := s.prepareGenesisBlock(1)
	time.Sleep(minInterval)
	b20 := s.prepareGenesisBlock(2)
	time.Sleep(minInterval)
	b30 := s.prepareGenesisBlock(3)
	// Submit these blocks to lattice.
	delivered, err = data.addBlock(b00)
	req.Len(delivered, 1)
	req.Nil(err)
	delivered, err = data.addBlock(b10)
	req.Len(delivered, 1)
	req.Nil(err)
	delivered, err = data.addBlock(b20)
	req.Len(delivered, 1)
	req.Nil(err)
	delivered, err = data.addBlock(b30)
	req.Len(delivered, 1)
	req.Nil(err)
	// We should be able to collect all 4 genesis blocks by calling
	// prepareBlock.
	b11 := &types.Block{
		Position: types.Position{
			ChainID: 1,
		},
		Timestamp: time.Now().UTC(),
	}
	data.prepareBlock(b11)
	s.hashBlock(b11)
	req.Contains(b11.Acks, b00.Hash)
	req.Contains(b11.Acks, b10.Hash)
	req.Contains(b11.Acks, b20.Hash)
	req.Contains(b11.Acks, b30.Hash)
	req.Equal(b11.ParentHash, b10.Hash)
	req.Equal(b11.Position.Height, uint64(1))
	delivered, err = data.addBlock(b11)
	req.Len(delivered, 1)
	req.Nil(err)
	// Propose/Process a block based on collected info.
	b12 := &types.Block{
		Position: types.Position{
			ChainID: 1,
		},
		Timestamp: time.Now().UTC(),
	}
	data.prepareBlock(b12)
	s.hashBlock(b12)
	// This time we only need to ack b11.
	req.Len(b12.Acks, 1)
	req.Contains(b12.Acks, b11.Hash)
	req.Equal(b12.ParentHash, b11.Hash)
	req.Equal(b12.Position.Height, uint64(2))
	// When calling with other validator ID, we should be able to
	// get 4 blocks to ack.
	b01 := &types.Block{
		Position: types.Position{
			ChainID: 0,
		},
	}
	data.prepareBlock(b01)
	s.hashBlock(b01)
	req.Len(b01.Acks, 4)
	req.Contains(b01.Acks, b00.Hash)
	req.Contains(b01.Acks, b11.Hash)
	req.Contains(b01.Acks, b20.Hash)
	req.Contains(b01.Acks, b30.Hash)
	req.Equal(b01.ParentHash, b00.Hash)
	req.Equal(b01.Position.Height, uint64(1))
}

func (s *LatticeDataTestSuite) TestCalcPurgeHeight() {
	// Test chainStatus.calcPurgeHeight, we don't have
	// to prepare blocks to test it.
	var req = s.Require()
	chain := &chainStatus{
		minHeight:  0,
		nextOutput: 0,
		nextAck:    []uint64{1, 1, 1, 1},
	}
	// When calculated safe is underflow, nok.
	safe, ok := chain.calcPurgeHeight()
	req.False(ok)
	// height=1 is outputed, and acked by everyone else.
	chain.nextOutput = 1
	safe, ok = chain.calcPurgeHeight()
	req.True(ok)
	req.Equal(safe, uint64(0))
	// Should take nextAck's height into consideration.
	chain.nextOutput = 2
	safe, ok = chain.calcPurgeHeight()
	req.True(ok)
	req.Equal(safe, uint64(0))
	// When minHeight is large that safe height, return nok.
	chain.minHeight = 1
	chain.nextOutput = 1
	safe, ok = chain.calcPurgeHeight()
	req.False(ok)
}

func (s *LatticeDataTestSuite) TestPurge() {
	// Make a simplest test case to test chainStatus.purge.
	// Make sure status after purge 1 block expected.
	b00 := &types.Block{Hash: common.NewRandomHash()}
	b01 := &types.Block{Hash: common.NewRandomHash()}
	b02 := &types.Block{Hash: common.NewRandomHash()}
	chain := &chainStatus{
		blocks:     []*types.Block{b00, b01, b02},
		nextAck:    []uint64{1, 1, 1, 1},
		nextOutput: 1,
	}
	hashes := chain.purge()
	s.Equal(hashes, common.Hashes{b00.Hash})
	s.Equal(chain.minHeight, uint64(1))
	s.Require().Len(chain.blocks, 2)
	s.Equal(chain.blocks[0].Hash, b01.Hash)
	s.Equal(chain.blocks[1].Hash, b02.Hash)
}

func (s *LatticeDataTestSuite) TestNextPosition() {
	// Test 'NextPosition' method when lattice is ready.
	data := s.genTestCase1()
	s.Equal(data.nextPosition(0), types.Position{ChainID: 0, Height: 4})

	// Test 'NextPosition' method when lattice is empty.
	data = newLatticeData(0, 4, 0, 1000*time.Second)
	s.Equal(data.nextPosition(0), types.Position{ChainID: 0, Height: 0})
}

func TestLatticeData(t *testing.T) {
	suite.Run(t, new(LatticeDataTestSuite))
}
