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

package blockdb

import (
	"encoding/json"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// LevelDBBackendBlockDB is a leveldb backend BlockDB implementation.
type LevelDBBackendBlockDB struct {
	db        *leveldb.DB
	index     map[types.ValidatorID]map[uint64]common.Hash
	indexLock sync.RWMutex
}

// NewLevelDBBackendBlockDB initialize a leveldb-backed block database.
func NewLevelDBBackendBlockDB(
	path string) (lvl *LevelDBBackendBlockDB, err error) {

	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return
	}
	lvl = &LevelDBBackendBlockDB{db: db}
	err = lvl.syncIndex()
	if err != nil {
		return
	}
	return
}

// Close would release allocated resource.
func (lvl *LevelDBBackendBlockDB) Close() error {
	return lvl.db.Close()
}

// Has implements the Reader.Has method.
func (lvl *LevelDBBackendBlockDB) Has(hash common.Hash) bool {
	exists, err := lvl.db.Has([]byte(hash[:]), nil)
	if err != nil {
		// TODO(missionliao): Modify the interface to return error.
		panic(err)
	}
	return exists
}

// Get implements the Reader.Get method.
func (lvl *LevelDBBackendBlockDB) Get(
	hash common.Hash) (block types.Block, err error) {

	queried, err := lvl.db.Get([]byte(hash[:]), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			err = ErrBlockDoesNotExist
		}
		return
	}
	err = json.Unmarshal(queried, &block)
	if err != nil {
		return
	}
	return
}

// GetByValidatorAndHeight implements
// the Reader.GetByValidatorAndHeight method.
func (lvl *LevelDBBackendBlockDB) GetByValidatorAndHeight(
	vID types.ValidatorID, height uint64) (block types.Block, err error) {

	lvl.indexLock.RLock()
	defer lvl.indexLock.RUnlock()

	// Get block's hash from in-memory index.
	vMap, exists := lvl.index[vID]
	if !exists {
		err = ErrBlockDoesNotExist
		return
	}
	hash, exists := vMap[height]
	if !exists {
		err = ErrBlockDoesNotExist
		return
	}

	// Get block from hash.
	queried, err := lvl.db.Get([]byte(hash[:]), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			err = ErrBlockDoesNotExist
		}
		return
	}

	err = json.Unmarshal(queried, &block)
	if err != nil {
		return
	}
	return
}

// Update implements the Writer.Update method.
func (lvl *LevelDBBackendBlockDB) Update(block types.Block) (err error) {
	// NOTE: we didn't handle changes of block hash (and it
	//       should not happen).
	marshaled, err := json.Marshal(&block)
	if err != nil {
		return
	}

	if !lvl.Has(block.Hash) {
		err = ErrBlockDoesNotExist
		return
	}
	err = lvl.db.Put(
		[]byte(block.Hash[:]),
		marshaled,
		&opt.WriteOptions{
			Sync: true,
		})
	if err != nil {
		return
	}
	return
}

// Put implements the Writer.Put method.
func (lvl *LevelDBBackendBlockDB) Put(block types.Block) (err error) {
	marshaled, err := json.Marshal(&block)
	if err != nil {
		return
	}
	if lvl.Has(block.Hash) {
		err = ErrBlockExists
		return
	}
	syncedOpt := &opt.WriteOptions{
		// We should force to sync for each write, it's safer
		// from crash.
		Sync: true,
	}
	err = lvl.db.Put(
		[]byte(block.Hash[:]),
		marshaled,
		syncedOpt)
	if err != nil {
		return
	}

	// Build in-memory index.
	lvl.addIndex(&block)
	return
}

func (lvl *LevelDBBackendBlockDB) syncIndex() (err error) {
	// Reset index.
	lvl.index = make(map[types.ValidatorID]map[uint64]common.Hash)

	// Construct index from DB.
	iter := lvl.db.NewIterator(nil, nil)
	defer func() {
		iter.Release()
		if err == nil {
			// Only return iterator's error when no error
			// is presented so far.
			err = iter.Error()
		}
	}()

	// Build index from blocks in DB, it may take time.
	var block types.Block
	for iter.Next() {
		err = json.Unmarshal(iter.Value(), &block)
		if err != nil {
			return
		}
		lvl.addIndex(&block)
	}
	return
}

func (lvl *LevelDBBackendBlockDB) addIndex(block *types.Block) {
	lvl.indexLock.Lock()
	defer lvl.indexLock.Unlock()

	heightMap, exists := lvl.index[block.ProposerID]
	if !exists {
		heightMap = make(map[uint64]common.Hash)
		lvl.index[block.ProposerID] = heightMap
	}
	heightMap[block.Height] = block.Hash
}
