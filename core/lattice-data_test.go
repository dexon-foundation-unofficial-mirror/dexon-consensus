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
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
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
func (s *LatticeDataTestSuite) genTestCase1() (
	data *latticeData, blocks map[uint32]map[uint64]*types.Block) {
	// Create new latticeData instance with 4 validators
	var (
		delivered []*types.Block
		chainNum  uint32 = 4
		req              = s.Require()
		now              = time.Now().UTC()
		err       error
	)
	// Setup stuffs.
	genesisConfig := &types.Config{
		RoundInterval:    500 * time.Second,
		NumChains:        chainNum,
		MinBlockInterval: 2 * time.Nanosecond,
	}
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	data = newLatticeData(db, newGenesisLatticeDataConfig(now, genesisConfig))
	config := &types.Config{
		RoundInterval:    1000 * time.Second,
		NumChains:        chainNum,
		MinBlockInterval: 2 * time.Nanosecond,
	}
	data.appendConfig(1, config)
	// Add genesis blocks.
	addBlock := func(b *types.Block) {
		s.hashBlock(b)
		delivered, err = data.addBlock(b)
		req.NoError(err)
		req.Len(delivered, 1)
		req.Equal(delivered[0].Hash, b.Hash)
	}
	// Genesis blocks are safe to be added to DAG, they acks no one.
	b00 := s.prepareGenesisBlock(0)
	addBlock(b00)
	b10 := s.prepareGenesisBlock(1)
	addBlock(b10)
	b20 := s.prepareGenesisBlock(2)
	addBlock(b20)
	b30 := s.prepareGenesisBlock(3)
	addBlock(b30)
	// Add block 0-1 which acks 0-0.
	b01 := &types.Block{
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
		Timestamp:  time.Now().UTC(),
		Position: types.Position{
			ChainID: 0,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{b00.Hash}),
		Witness: types.Witness{
			Height: 1,
		},
	}
	addBlock(b01)
	// Add block 0-2 which acks 0-1 and 1-0.
	b02 := &types.Block{
		ParentHash: b01.Hash,
		Position: types.Position{
			ChainID: 0,
			Height:  2,
		},
		Timestamp: time.Now().UTC(),
		Acks: common.NewSortedHashes(common.Hashes{
			b01.Hash,
			b10.Hash,
		}),
		Witness: types.Witness{
			Height: 2,
		},
	}
	addBlock(b02)
	// Add block 0-3 which acks 0-2.
	b03 := &types.Block{
		ParentHash: b02.Hash,
		Hash:       common.NewRandomHash(),
		Timestamp:  time.Now().UTC(),
		Position: types.Position{
			ChainID: 0,
			Height:  3,
		},
		Acks: common.NewSortedHashes(common.Hashes{b02.Hash}),
		Witness: types.Witness{
			Height: 3,
		},
	}
	addBlock(b03)
	// Add block 3-1 which acks 3-0.
	b31 := &types.Block{
		ParentHash: b30.Hash,
		Hash:       common.NewRandomHash(),
		Timestamp:  time.Now().UTC(),
		Position: types.Position{
			ChainID: 3,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{b30.Hash}),
		Witness: types.Witness{
			Height: 1,
		},
	}
	addBlock(b31)
	// Return created blocks.
	blocks = map[uint32]map[uint64]*types.Block{
		0: map[uint64]*types.Block{
			0: b00,
			1: b01,
			2: b02,
			3: b03,
		},
		1: map[uint64]*types.Block{0: b10},
		2: map[uint64]*types.Block{0: b20},
		3: map[uint64]*types.Block{0: b30},
	}
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

func (s *LatticeDataTestSuite) TestSanityCheck() {
	var (
		data, blocks = s.genTestCase1()
		req          = s.Require()
	)
	check := func(expectedErr error, b *types.Block) {
		s.hashBlock(b)
		err := data.sanityCheck(b)
		req.NotNil(err)
		req.IsType(expectedErr, err)
	}
	// Non-genesis block with no ack, should get error.
	check(ErrNotAckParent, &types.Block{
		ParentHash: blocks[1][0].Hash,
		Position: types.Position{
			ChainID: 1,
			Height:  1,
		},
		Acks:      common.NewSortedHashes(common.Hashes{}),
		Timestamp: time.Now().UTC(),
	})
	// Non-genesis block which acks its parent but the height is invalid.
	check(ErrInvalidBlockHeight, &types.Block{
		ParentHash: blocks[1][0].Hash,
		Position: types.Position{
			ChainID: 1,
			Height:  2,
		},
		Acks:      common.NewSortedHashes(common.Hashes{blocks[1][0].Hash}),
		Timestamp: time.Now().UTC(),
	})
	// Invalid chain ID.
	check(ErrInvalidChainID, &types.Block{
		ParentHash: blocks[1][0].Hash,
		Position: types.Position{
			ChainID: 100,
			Height:  1,
		},
		Acks:      common.NewSortedHashes(common.Hashes{blocks[1][0].Hash}),
		Timestamp: time.Now().UTC(),
	})
	// Replicated ack.
	check(ErrDoubleAck, &types.Block{
		ParentHash: blocks[0][3].Hash,
		Position: types.Position{
			ChainID: 0,
			Height:  4,
		},
		Acks: common.NewSortedHashes(common.Hashes{
			blocks[0][3].Hash,
			blocks[1][0].Hash,
		}),
		Timestamp: time.Now().UTC(),
		Witness: types.Witness{
			Height: 4,
		},
	})
	// Acking block doesn't exists.
	check(&ErrAckingBlockNotExists{}, &types.Block{
		ParentHash: blocks[1][0].Hash,
		Position: types.Position{
			ChainID: 1,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{
			blocks[1][0].Hash,
			common.NewRandomHash(),
		}),
		Timestamp: time.Now().UTC(),
	})
	// Parent block on different chain.
	check(&ErrAckingBlockNotExists{}, &types.Block{
		ParentHash: blocks[1][0].Hash,
		Position: types.Position{
			ChainID: 2,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{
			blocks[1][0].Hash,
			blocks[2][0].Hash,
		}),
		Timestamp: time.Now().UTC(),
	})
	// Ack two blocks on the same chain.
	check(ErrDuplicatedAckOnOneChain, &types.Block{
		ParentHash: blocks[2][0].Hash,
		Position: types.Position{
			ChainID: 2,
			Height:  1,
		},
		Acks: common.NewSortedHashes(common.Hashes{
			blocks[2][0].Hash,
			blocks[0][0].Hash,
			blocks[0][1].Hash,
		}),
		Timestamp: time.Now().UTC(),
	})
	// Witness height decreases.
	check(ErrInvalidWitness, &types.Block{
		ParentHash: blocks[0][3].Hash,
		Position: types.Position{
			ChainID: 0,
			Height:  4,
		},
		Timestamp: time.Now().UTC(),
		Acks: common.NewSortedHashes(common.Hashes{
			blocks[0][3].Hash,
		}),
		Witness: types.Witness{
			Height: 2,
		},
	})
	// Add block 3-1 which acks 3-0, and violet reasonable block time interval.
	b := &types.Block{
		ParentHash: blocks[2][0].Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			ChainID: 2,
			Height:  1,
		},
		Acks:      common.NewSortedHashes(common.Hashes{blocks[2][0].Hash}),
		Timestamp: time.Now().UTC(),
	}
	// Violet minimum block time interval.
	b.Timestamp = blocks[2][0].Timestamp.Add(1 * time.Nanosecond)
	check(ErrIncorrectBlockTime, b)
	// Add a normal block with timestamp pass round cutting time.
	b11 := &types.Block{
		ParentHash: blocks[1][0].Hash,
		Position: types.Position{
			ChainID: 1,
			Height:  1,
		},
		Acks:      common.NewSortedHashes(common.Hashes{blocks[1][0].Hash}),
		Timestamp: time.Now().UTC().Add(500 * time.Second),
	}
	s.hashBlock(b11)
	req.NoError(data.sanityCheck(b11))
	_, err := data.addBlock(b11)
	req.NoError(err)
	// A block didn't perform round switching.
	b12 := &types.Block{
		ParentHash: b11.Hash,
		Position: types.Position{
			ChainID: 1,
			Height:  2,
		},
		Acks:      common.NewSortedHashes(common.Hashes{b11.Hash}),
		Timestamp: time.Now().UTC().Add(501 * time.Second),
	}
	check(ErrRoundNotSwitch, b12)
	// A block with expected new round ID should be OK.
	b12.Position.Round = 1
	s.hashBlock(b12)
	req.NoError(data.sanityCheck(b12))
}

func (s *LatticeDataTestSuite) TestRandomlyGeneratedBlocks() {
	var (
		chainNum    uint32 = 19
		repeat             = 20
		delivered   []*types.Block
		err         error
		req         = s.Require()
		datum       []*latticeData
		genesisTime = time.Now().UTC()
	)
	if testing.Short() {
		chainNum = 7
		repeat = 3
	}
	// Setup configuration that no restriction on block interval and
	// round cutting.
	genesisConfig := &types.Config{
		RoundInterval:    1000 * time.Second,
		NumChains:        chainNum,
		MinBlockInterval: 1 * time.Second,
	}
	// Prepare a randomly generated blocks.
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	gen := test.NewBlocksGenerator(&test.BlocksGeneratorConfig{
		NumChains:            genesisConfig.NumChains,
		MinBlockTimeInterval: genesisConfig.MinBlockInterval,
	}, nil, hashBlock)
	req.NoError(gen.Generate(
		0,
		genesisTime,
		genesisTime.Add(genesisConfig.RoundInterval),
		db))
	iter, err := db.GetAll()
	req.NoError(err)
	// Setup a revealer that would reveal blocks randomly but still form
	// valid DAG without holes.
	revealer, err := test.NewRandomDAGRevealer(iter)
	req.Nil(err)

	revealedHashesAsString := map[string]struct{}{}
	deliveredHashesAsString := map[string]struct{}{}
	for i := 0; i < repeat; i++ {
		db, err := blockdb.NewMemBackedBlockDB()
		req.NoError(err)
		data := newLatticeData(
			db, newGenesisLatticeDataConfig(genesisTime, genesisConfig))
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
			req.NoError(err)
			revealedHashes = append(revealedHashes, b.Hash)
			// Pass blocks to lattice.
			req.NoError(data.sanityCheck(&b))
			delivered, err = data.addBlock(&b)
			req.NoError(err)
			for _, b := range delivered {
				deliveredHashes = append(deliveredHashes, b.Hash)
			}
		}
		// To make it easier to check, sort hashes of
		// delivered blocks, and concatenate them into
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
	// Make sure concatenated hashes of delivered blocks are identical.
	req.Len(deliveredHashesAsString, 1)
	for h := range deliveredHashesAsString {
		// Make sure at least some blocks are delivered.
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
			// Check chain status of this pair.
			for chainID, statusI := range bI.chains {
				req.Equal(statusI.tip, bJ.chains[chainID].tip)
				req.Equal(len(statusI.blocks), len(bJ.chains[chainID].blocks))
				// Check lastAckPos.
				for x, pos := range statusI.lastAckPos {
					req.Equal(pos, bJ.chains[chainID].lastAckPos[x])
				}
				// Check blocks.
				if len(statusI.blocks) > 0 {
					req.Equal(statusI.blocks[0], bJ.chains[chainID].blocks[0])
				}
			}
		}
	}
}

func (s *LatticeDataTestSuite) TestPrepareBlock() {
	var (
		chainNum    uint32 = 4
		req                = s.Require()
		minInterval        = 50 * time.Millisecond
		delivered   []*types.Block
		err         error
	)
	// Setup configuration that no restriction on block interval and
	// round cutting.
	genesisConfig := &types.Config{
		RoundInterval:    3000 * time.Second,
		NumChains:        chainNum,
		MinBlockInterval: 1 * time.Second,
	}
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	data := newLatticeData(
		db, newGenesisLatticeDataConfig(time.Now().UTC(), genesisConfig))
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
	req.NoError(err)
	req.Len(delivered, 1)
	delivered, err = data.addBlock(b10)
	req.NoError(err)
	req.Len(delivered, 1)
	delivered, err = data.addBlock(b20)
	req.NoError(err)
	req.Len(delivered, 1)
	delivered, err = data.addBlock(b30)
	req.NoError(err)
	req.Len(delivered, 1)
	// We should be able to collect all 4 genesis blocks by calling
	// prepareBlock.
	b11 := &types.Block{
		Position: types.Position{
			ChainID: 1,
		},
		Timestamp: time.Now().UTC(),
	}
	req.NoError(data.prepareBlock(b11))
	s.hashBlock(b11)
	req.Contains(b11.Acks, b00.Hash)
	req.Contains(b11.Acks, b10.Hash)
	req.Contains(b11.Acks, b20.Hash)
	req.Contains(b11.Acks, b30.Hash)
	req.Equal(b11.ParentHash, b10.Hash)
	req.Equal(b11.Position.Height, uint64(1))
	delivered, err = data.addBlock(b11)
	req.Len(delivered, 1)
	req.NoError(err)
	// Propose/Process a block based on collected info.
	b12 := &types.Block{
		Position: types.Position{
			ChainID: 1,
		},
		Timestamp: time.Now().UTC(),
	}
	req.NoError(data.prepareBlock(b12))
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
	req.NoError(data.prepareBlock(b01))
	s.hashBlock(b01)
	req.Len(b01.Acks, 4)
	req.Contains(b01.Acks, b00.Hash)
	req.Contains(b01.Acks, b11.Hash)
	req.Contains(b01.Acks, b20.Hash)
	req.Contains(b01.Acks, b30.Hash)
	req.Equal(b01.ParentHash, b00.Hash)
	req.Equal(b01.Position.Height, uint64(1))
}

func (s *LatticeDataTestSuite) TestNextPosition() {
	// Test 'NextPosition' method when lattice is ready.
	data, _ := s.genTestCase1()
	s.Equal(data.nextPosition(0), types.Position{ChainID: 0, Height: 4})
	// Test 'NextPosition' method when lattice is empty.
	// Setup a configuration that no restriction on block interval and
	// round cutting.
	genesisConfig := &types.Config{
		RoundInterval:    1000 * time.Second,
		NumChains:        4,
		MinBlockInterval: 1 * time.Second,
	}
	data = newLatticeData(
		nil, newGenesisLatticeDataConfig(time.Now().UTC(), genesisConfig))
	s.Equal(data.nextPosition(0), types.Position{ChainID: 0, Height: 0})
}

func (s *LatticeDataTestSuite) TestPrepareEmptyBlock() {
	data, _ := s.genTestCase1()
	b := &types.Block{
		Position: types.Position{
			ChainID: 0,
		},
	}
	data.prepareEmptyBlock(b)
	s.True(b.IsEmpty())
	s.Equal(uint64(4), b.Position.Height)
}

func (s *LatticeDataTestSuite) TestNumChainsChange() {
	// This test case verify the behavior when NumChains
	// changes. We only reply on methods of latticeData
	// for test. It would run in this way:
	// - Several configs would be prepared in advance, scenario for NumChains
	//   increasing and decreasing would be included.
	// - Blocks would be prepared from candidate chains.
	// - Once a block is detected as last block of that chain in that round,
	//   that chain would be revmoved from candidate chains.
	// - Once candidate chains are empty, proceed to next round until the last
	//   round.
	//
	// These scenarioes would be checked in this test:
	// - Each block generated successfully by latticeData.prepareBlock
	//   should be no error when passing to latticeData.sanityCheck
	//   and latticeData.addBlock.
	// - The delivered blocks should form a valid DAG.
	fixConfig := func(config *types.Config) *types.Config {
		config.MinBlockInterval = 10 * time.Second
		config.RoundInterval = 100 * time.Second
		return config
	}
	var (
		req       = s.Require()
		maxChains = uint32(16)
		configs   = []*types.Config{
			fixConfig(&types.Config{NumChains: 13}),
			fixConfig(&types.Config{NumChains: 10}),
			fixConfig(&types.Config{NumChains: maxChains}),
			fixConfig(&types.Config{NumChains: 7}),
		}
		randObj = rand.New(rand.NewSource(time.Now().UnixNano()))
	)
	// Setup blockdb instance.
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	// Set up latticeData instance.
	lattice := newLatticeData(db, newGenesisLatticeDataConfig(
		time.Now().UTC(), configs[0]))
	req.NoError(lattice.appendConfig(1, configs[1]))
	req.NoError(lattice.appendConfig(2, configs[2]))
	req.NoError(lattice.appendConfig(3, configs[3]))
	// Run until candidate chains are empty.
	var (
		delivered         []*types.Block
		candidateChainIDs []uint32
		nextRound         uint64
	)
	for {
		// Decide chainID.
		if len(candidateChainIDs) == 0 {
			// Proceed to next round.
			if nextRound >= uint64(len(configs)) {
				break
			}
			c := configs[nextRound]
			nextRound++
			for i := uint32(0); i < c.NumChains; i++ {
				candidateChainIDs = append(candidateChainIDs, i)
			}
		}
		chainID := candidateChainIDs[randObj.Intn(len(candidateChainIDs))]
		// Prepare blocks, see if we are legal to propose block at
		// this position.
		b := &types.Block{
			Position: types.Position{
				ChainID: chainID,
				Round:   nextRound - 1,
			}}
		err = lattice.prepareBlock(b)
		if err == ErrRoundNotSwitch {
			// This round is done, remove this channel from candidate.
			for i := range candidateChainIDs {
				if candidateChainIDs[i] != chainID {
					continue
				}
				candidateChainIDs = append(
					candidateChainIDs[:i], candidateChainIDs[i+1:]...)
				break
			}
			continue
		}
		req.NoError(err)
		s.hashBlock(b)
		// Do the actual lattice usage.
		req.NoError(lattice.sanityCheck(b))
		d, err := lattice.addBlock(b)
		req.NoError(err)
		delivered = append(delivered, d...)
	}
	// verify delivered form a DAG.
	dag := map[common.Hash]struct{}{}
	for _, b := range delivered {
		for _, ack := range b.Acks {
			_, exists := dag[ack]
			req.True(exists)
		}
		dag[b.Hash] = struct{}{}
	}
}

func TestLatticeData(t *testing.T) {
	suite.Run(t, new(LatticeDataTestSuite))
}
