package types

import (
	"sort"
	"testing"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/stretchr/testify/suite"
)

type BlockTestSuite struct {
	suite.Suite
}

func (s *BlockTestSuite) TestSortByHash() {
	hash := common.Hash{}
	copy(hash[:], "aaaaaa")
	b0 := &Block{Hash: hash}
	copy(hash[:], "bbbbbb")
	b1 := &Block{Hash: hash}
	copy(hash[:], "cccccc")
	b2 := &Block{Hash: hash}
	copy(hash[:], "dddddd")
	b3 := &Block{Hash: hash}

	blocks := []*Block{b3, b2, b1, b0}
	sort.Sort(ByHash(blocks))
	s.Equal(blocks[0].Hash, b0.Hash)
	s.Equal(blocks[1].Hash, b1.Hash)
	s.Equal(blocks[2].Hash, b2.Hash)
	s.Equal(blocks[3].Hash, b3.Hash)
}

func (s *BlockTestSuite) TestSortByHeight() {
	b0 := &Block{Height: 0}
	b1 := &Block{Height: 1}
	b2 := &Block{Height: 2}
	b3 := &Block{Height: 3}

	blocks := []*Block{b3, b2, b1, b0}
	sort.Sort(ByHeight(blocks))
	s.Equal(blocks[0].Hash, b0.Hash)
	s.Equal(blocks[1].Hash, b1.Hash)
	s.Equal(blocks[2].Hash, b2.Hash)
	s.Equal(blocks[3].Hash, b3.Hash)
}

func TestBlock(t *testing.T) {
	suite.Run(t, new(BlockTestSuite))
}
