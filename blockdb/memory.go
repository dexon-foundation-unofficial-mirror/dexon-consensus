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
	"io/ioutil"
	"os"
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type seqIterator struct {
	idx int
	db  *MemBackedBlockDB
}

func (seq *seqIterator) Next() (types.Block, error) {
	curIdx := seq.idx
	seq.idx++
	return seq.db.getByIndex(curIdx)
}

// MemBackedBlockDB is a memory backed BlockDB implementation.
type MemBackedBlockDB struct {
	blocksMutex        sync.RWMutex
	blockHashSequence  common.Hashes
	blocksByHash       map[common.Hash]*types.Block
	persistantFilePath string
}

// NewMemBackedBlockDB initialize a memory-backed block database.
func NewMemBackedBlockDB(persistantFilePath ...string) (db *MemBackedBlockDB, err error) {
	db = &MemBackedBlockDB{
		blockHashSequence: common.Hashes{},
		blocksByHash:      make(map[common.Hash]*types.Block),
	}
	if len(persistantFilePath) == 0 || len(persistantFilePath[0]) == 0 {
		return
	}
	db.persistantFilePath = persistantFilePath[0]
	buf, err := ioutil.ReadFile(db.persistantFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			// Something unexpected happened.
			return
		}
		// It's expected behavior that file doesn't exists, we should not
		// report error on it.
		err = nil
		return
	}

	// Init this instance by file content, it's a temporary way
	// to export those private field for JSON encoding.
	toLoad := struct {
		Sequence common.Hashes
		ByHash   map[common.Hash]*types.Block
	}{}
	err = json.Unmarshal(buf, &toLoad)
	if err != nil {
		return
	}
	db.blockHashSequence = toLoad.Sequence
	db.blocksByHash = toLoad.ByHash
	return
}

// Has returns wheter or not the DB has a block identified with the hash.
func (m *MemBackedBlockDB) Has(hash common.Hash) bool {
	m.blocksMutex.RLock()
	defer m.blocksMutex.RUnlock()

	_, ok := m.blocksByHash[hash]
	return ok
}

// Get returns a block given a hash.
func (m *MemBackedBlockDB) Get(hash common.Hash) (types.Block, error) {
	m.blocksMutex.RLock()
	defer m.blocksMutex.RUnlock()

	return m.internalGet(hash)
}

func (m *MemBackedBlockDB) internalGet(hash common.Hash) (types.Block, error) {
	b, ok := m.blocksByHash[hash]
	if !ok {
		return types.Block{}, ErrBlockDoesNotExist
	}
	return *b, nil
}

// Put inserts a new block into the database.
func (m *MemBackedBlockDB) Put(block types.Block) error {
	if m.Has(block.Hash) {
		return ErrBlockExists
	}

	m.blocksMutex.Lock()
	defer m.blocksMutex.Unlock()

	m.blockHashSequence = append(m.blockHashSequence, block.Hash)
	m.blocksByHash[block.Hash] = &block
	return nil
}

// Update updates a block in the database.
func (m *MemBackedBlockDB) Update(block types.Block) error {
	m.blocksMutex.Lock()
	defer m.blocksMutex.Unlock()

	m.blocksByHash[block.Hash] = &block
	return nil
}

// Close implement Closer interface, which would release allocated resource.
func (m *MemBackedBlockDB) Close() (err error) {
	// Save internal state to a pretty-print json file. It's a temporary way
	// to dump private file via JSON encoding.
	if len(m.persistantFilePath) == 0 {
		return
	}

	m.blocksMutex.RLock()
	defer m.blocksMutex.RUnlock()

	toDump := struct {
		Sequence common.Hashes
		ByHash   map[common.Hash]*types.Block
	}{
		Sequence: m.blockHashSequence,
		ByHash:   m.blocksByHash,
	}

	// Dump to JSON with 2-space indent.
	buf, err := json.Marshal(&toDump)
	if err != nil {
		return
	}

	err = ioutil.WriteFile(m.persistantFilePath, buf, 0644)
	return
}

func (m *MemBackedBlockDB) getByIndex(idx int) (types.Block, error) {
	m.blocksMutex.RLock()
	defer m.blocksMutex.RUnlock()

	if idx >= len(m.blockHashSequence) {
		return types.Block{}, ErrIterationFinished
	}

	hash := m.blockHashSequence[idx]
	return m.internalGet(hash)
}

// GetAll implement Reader.GetAll method, which allows caller
// to retrieve all blocks in DB.
func (m *MemBackedBlockDB) GetAll() (BlockIterator, error) {
	return &seqIterator{db: m}, nil
}
