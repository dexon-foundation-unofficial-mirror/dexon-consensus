package blockdb

import (
	"os"
	"testing"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type MemBackedBlockDBTestSuite struct {
	suite.Suite

	v0            types.ValidatorID
	b00, b01, b02 *types.Block
}

func (s *MemBackedBlockDBTestSuite) SetupSuite() {
	s.v0 = types.ValidatorID{Hash: common.NewRandomHash()}

	genesisHash := common.NewRandomHash()
	s.b00 = &types.Block{
		ProposerID: s.v0,
		ParentHash: genesisHash,
		Hash:       genesisHash,
		Height:     0,
		Acks:       make(map[common.Hash]struct{}),
	}
	s.b01 = &types.Block{
		ProposerID: s.v0,
		ParentHash: s.b00.Hash,
		Hash:       common.NewRandomHash(),
		Height:     1,
		Acks: map[common.Hash]struct{}{
			s.b00.Hash: struct{}{},
		},
	}
	s.b02 = &types.Block{
		ProposerID: s.v0,
		ParentHash: s.b01.Hash,
		Hash:       common.NewRandomHash(),
		Height:     2,
		Acks: map[common.Hash]struct{}{
			s.b01.Hash: struct{}{},
		},
	}
}

func (s *MemBackedBlockDBTestSuite) TestSaveAndLoad() {
	// Make sure we are able to save/load from file.
	dbPath := "test-save-and-load.db"

	// Make sure the file pointed by 'dbPath' doesn't exist.
	_, err := os.Stat(dbPath)
	s.Require().NotNil(err)

	db, err := NewMemBackedBlockDB(dbPath)
	s.Require().Nil(err)
	s.Require().NotNil(db)
	defer func() {
		if db != nil {
			s.Nil(os.Remove(dbPath))
			db = nil
		}
	}()

	s.Nil(db.Put(*s.b00))
	s.Nil(db.Put(*s.b01))
	s.Nil(db.Put(*s.b02))
	s.Nil(db.Close())

	// Load the json file back to check if all inserted blocks
	// exists.
	db, err = NewMemBackedBlockDB(dbPath)
	s.Require().Nil(err)
	s.Require().NotNil(db)
	s.True(db.Has(s.b00.Hash))
	s.True(db.Has(s.b01.Hash))
	s.True(db.Has(s.b02.Hash))
	s.Nil(db.Close())
}

func (s *MemBackedBlockDBTestSuite) TestIteration() {
	// Make sure the file pointed by 'dbPath' doesn't exist.
	db, err := NewMemBackedBlockDB()
	s.Require().Nil(err)
	s.Require().NotNil(db)

	// Setup database.
	s.Nil(db.Put(*s.b00))
	s.Nil(db.Put(*s.b01))
	s.Nil(db.Put(*s.b02))

	// Check if we can iterate all 3 blocks.
	iter, err := db.GetAll()
	s.Require().Nil(err)
	touched := map[common.Hash]struct{}{}
	for {
		b, err := iter.Next()
		if err == ErrIterationFinished {
			break
		}
		s.Require().Nil(err)
		touched[b.Hash] = struct{}{}
	}
	s.Len(touched, 3)
	s.Contains(touched, s.b00.Hash)
	s.Contains(touched, s.b01.Hash)
	s.Contains(touched, s.b02.Hash)
}

func TestMemBackedBlockDB(t *testing.T) {
	suite.Run(t, new(MemBackedBlockDBTestSuite))
}
