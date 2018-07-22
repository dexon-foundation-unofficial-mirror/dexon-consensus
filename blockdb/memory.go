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
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// MemBackendBlockDB is a memory backend BlockDB implementation.
type MemBackendBlockDB struct {
	blocksByHash      map[common.Hash]*types.Block
	blocksByValidator map[types.ValidatorID]map[uint64]*types.Block
}

// NewMemBackedBlockDB initialize a memory-backed block database.
func NewMemBackedBlockDB() *MemBackendBlockDB {
	return &MemBackendBlockDB{
		blocksByHash:      make(map[common.Hash]*types.Block),
		blocksByValidator: make(map[types.ValidatorID]map[uint64]*types.Block),
	}
}

// Has returns wheter or not the DB has a block identified with the hash.
func (m *MemBackendBlockDB) Has(hash common.Hash) bool {
	_, ok := m.blocksByHash[hash]
	return ok
}

// Get returns a block given a hash.
func (m *MemBackendBlockDB) Get(hash common.Hash) (types.Block, error) {
	b, ok := m.blocksByHash[hash]
	if !ok {
		return types.Block{}, ErrBlockDoesNotExist
	}
	return *b, nil
}

// GetByValidatorAndHeight returns a block given validator ID and hash.
func (m *MemBackendBlockDB) GetByValidatorAndHeight(
	vID types.ValidatorID, height uint64) (types.Block, error) {
	validatorBlocks, ok := m.blocksByValidator[vID]
	if !ok {
		return types.Block{}, ErrValidatorDoesNotExist
	}
	block, ok2 := validatorBlocks[height]
	if !ok2 {
		return types.Block{}, ErrBlockDoesNotExist
	}
	return *block, nil
}

// Put inserts a new block into the database.
func (m *MemBackendBlockDB) Put(block types.Block) error {
	if m.Has(block.Hash) {
		return ErrBlockExists
	}
	return m.Update(block)
}

// Update updates a block in the database.
func (m *MemBackendBlockDB) Update(block types.Block) error {
	m.blocksByHash[block.Hash] = &block
	return nil
}
