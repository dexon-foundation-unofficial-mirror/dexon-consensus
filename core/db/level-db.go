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

package db

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon/rlp"
)

var (
	blockKeyPrefix            = []byte("b-")
	compactionChainTipInfoKey = []byte("cc-tip")
)

type compactionChainTipInfo struct {
	Height uint64      `json:"height"`
	Hash   common.Hash `json:"hash"`
}

// LevelDBBackedDB is a leveldb backed DB implementation.
type LevelDBBackedDB struct {
	db *leveldb.DB
}

// NewLevelDBBackedDB initialize a leveldb-backed database.
func NewLevelDBBackedDB(
	path string) (lvl *LevelDBBackedDB, err error) {

	dbInst, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return
	}
	lvl = &LevelDBBackedDB{db: dbInst}
	return
}

// Close implement Closer interface, which would release allocated resource.
func (lvl *LevelDBBackedDB) Close() error {
	return lvl.db.Close()
}

// HasBlock implements the Reader.Has method.
func (lvl *LevelDBBackedDB) HasBlock(hash common.Hash) bool {
	exists, err := lvl.internalHasBlock(lvl.getBlockKey(hash))
	if err != nil {
		// TODO(missionliao): Modify the interface to return error.
		panic(err)
	}
	return exists
}

func (lvl *LevelDBBackedDB) internalHasBlock(key []byte) (bool, error) {
	exists, err := lvl.db.Has(key, nil)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// GetBlock implements the Reader.GetBlock method.
func (lvl *LevelDBBackedDB) GetBlock(
	hash common.Hash) (block types.Block, err error) {
	queried, err := lvl.db.Get(lvl.getBlockKey(hash), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			err = ErrBlockDoesNotExist
		}
		return
	}
	err = rlp.DecodeBytes(queried, &block)
	if err != nil {
		return
	}
	return
}

// UpdateBlock implements the Writer.UpdateBlock method.
func (lvl *LevelDBBackedDB) UpdateBlock(block types.Block) (err error) {
	// NOTE: we didn't handle changes of block hash (and it
	//       should not happen).
	marshaled, err := rlp.EncodeToBytes(&block)
	if err != nil {
		return
	}
	blockKey := lvl.getBlockKey(block.Hash)
	exists, err := lvl.internalHasBlock(blockKey)
	if err != nil {
		return
	}
	if !exists {
		err = ErrBlockDoesNotExist
		return
	}
	if err = lvl.db.Put(blockKey, marshaled, nil); err != nil {
		return
	}
	return
}

// PutBlock implements the Writer.PutBlock method.
func (lvl *LevelDBBackedDB) PutBlock(block types.Block) (err error) {
	marshaled, err := rlp.EncodeToBytes(&block)
	if err != nil {
		return
	}
	blockKey := lvl.getBlockKey(block.Hash)
	exists, err := lvl.internalHasBlock(blockKey)
	if err != nil {
		return
	}
	if exists {
		err = ErrBlockExists
		return
	}
	if err = lvl.db.Put(blockKey, marshaled, nil); err != nil {
		return
	}
	return
}

// GetAllBlocks implements Reader.GetAllBlocks method, which allows callers
// to retrieve all blocks in DB.
func (lvl *LevelDBBackedDB) GetAllBlocks() (BlockIterator, error) {
	// TODO (mission): Implement this part via goleveldb's iterator.
	return nil, ErrNotImplemented
}

// PutCompactionChainTipInfo saves tip of compaction chain into the database.
func (lvl *LevelDBBackedDB) PutCompactionChainTipInfo(
	blockHash common.Hash, height uint64) error {
	marshaled, err := rlp.EncodeToBytes(&compactionChainTipInfo{
		Hash:   blockHash,
		Height: height,
	})
	if err != nil {
		return err
	}
	// Check current cached tip info to make sure the one to be updated is
	// valid.
	info, err := lvl.internalGetCompactionChainTipInfo()
	if err != nil {
		return err
	}
	if info.Height >= height {
		return ErrInvalidCompactionChainTipHeight
	}
	if err = lvl.db.Put(compactionChainTipInfoKey, marshaled, nil); err != nil {
		return err
	}
	return nil
}

func (lvl *LevelDBBackedDB) internalGetCompactionChainTipInfo() (
	info compactionChainTipInfo, err error) {
	queried, err := lvl.db.Get(compactionChainTipInfoKey, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			err = nil
		}
		return
	}
	if err = rlp.DecodeBytes(queried, &info); err != nil {
		return
	}
	return
}

// GetCompactionChainTipInfo get the tip info of compaction chain into the
// database.
func (lvl *LevelDBBackedDB) GetCompactionChainTipInfo() (
	hash common.Hash, height uint64) {
	info, err := lvl.internalGetCompactionChainTipInfo()
	if err != nil {
		panic(err)
	}
	hash, height = info.Hash, info.Height
	return
}

func (lvl *LevelDBBackedDB) getBlockKey(hash common.Hash) (ret []byte) {
	ret = make([]byte, len(blockKeyPrefix)+len(hash[:]))
	copy(ret, blockKeyPrefix)
	copy(ret[len(blockKeyPrefix):], hash[:])
	return
}
