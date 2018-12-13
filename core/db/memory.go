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
	"encoding/json"
	"io/ioutil"
	"os"
	"sync"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type blockSeqIterator struct {
	idx int
	db  *MemBackedDB
}

// NextBlock implemenets BlockIterator.NextBlock method.
func (seq *blockSeqIterator) NextBlock() (types.Block, error) {
	curIdx := seq.idx
	seq.idx++
	return seq.db.getBlockByIndex(curIdx)
}

// MemBackedDB is a memory backed DB implementation.
type MemBackedDB struct {
	blocksMutex        sync.RWMutex
	blockHashSequence  common.Hashes
	blocksByHash       map[common.Hash]*types.Block
	persistantFilePath string
}

// NewMemBackedDB initialize a memory-backed database.
func NewMemBackedDB(persistantFilePath ...string) (
	dbInst *MemBackedDB, err error) {
	dbInst = &MemBackedDB{
		blockHashSequence: common.Hashes{},
		blocksByHash:      make(map[common.Hash]*types.Block),
	}
	if len(persistantFilePath) == 0 || len(persistantFilePath[0]) == 0 {
		return
	}
	dbInst.persistantFilePath = persistantFilePath[0]
	buf, err := ioutil.ReadFile(dbInst.persistantFilePath)
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
	dbInst.blockHashSequence = toLoad.Sequence
	dbInst.blocksByHash = toLoad.ByHash
	return
}

// HasBlock returns wheter or not the DB has a block identified with the hash.
func (m *MemBackedDB) HasBlock(hash common.Hash) bool {
	m.blocksMutex.RLock()
	defer m.blocksMutex.RUnlock()

	_, ok := m.blocksByHash[hash]
	return ok
}

// GetBlock returns a block given a hash.
func (m *MemBackedDB) GetBlock(hash common.Hash) (types.Block, error) {
	m.blocksMutex.RLock()
	defer m.blocksMutex.RUnlock()

	return m.internalGetBlock(hash)
}

func (m *MemBackedDB) internalGetBlock(hash common.Hash) (types.Block, error) {
	b, ok := m.blocksByHash[hash]
	if !ok {
		return types.Block{}, ErrBlockDoesNotExist
	}
	return *b, nil
}

// PutBlock inserts a new block into the database.
func (m *MemBackedDB) PutBlock(block types.Block) error {
	if m.HasBlock(block.Hash) {
		return ErrBlockExists
	}

	m.blocksMutex.Lock()
	defer m.blocksMutex.Unlock()

	m.blockHashSequence = append(m.blockHashSequence, block.Hash)
	m.blocksByHash[block.Hash] = &block
	return nil
}

// UpdateBlock updates a block in the database.
func (m *MemBackedDB) UpdateBlock(block types.Block) error {
	if !m.HasBlock(block.Hash) {
		return ErrBlockDoesNotExist
	}

	m.blocksMutex.Lock()
	defer m.blocksMutex.Unlock()

	m.blocksByHash[block.Hash] = &block
	return nil
}

// Close implement Closer interface, which would release allocated resource.
func (m *MemBackedDB) Close() (err error) {
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

func (m *MemBackedDB) getBlockByIndex(idx int) (types.Block, error) {
	m.blocksMutex.RLock()
	defer m.blocksMutex.RUnlock()

	if idx >= len(m.blockHashSequence) {
		return types.Block{}, ErrIterationFinished
	}

	hash := m.blockHashSequence[idx]
	return m.internalGetBlock(hash)
}

// GetAllBlocks implement Reader.GetAllBlocks method, which allows caller
// to retrieve all blocks in DB.
func (m *MemBackedDB) GetAllBlocks() (BlockIterator, error) {
	return &blockSeqIterator{db: m}, nil
}
