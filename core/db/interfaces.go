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
	"errors"
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

var (
	// ErrBlockExists is the error when block eixsts.
	ErrBlockExists = errors.New("block exists")
	// ErrBlockDoesNotExist is the error when block does not eixst.
	ErrBlockDoesNotExist = errors.New("block does not exist")
	// ErrIterationFinished is the error to check if the iteration is finished.
	ErrIterationFinished = errors.New("iteration finished")
	// ErrEmptyPath is the error when the required path is empty.
	ErrEmptyPath = fmt.Errorf("empty path")
	// ErrClosed is the error when using DB after it's closed.
	ErrClosed = fmt.Errorf("db closed")
	// ErrNotImplemented is the error that some interface is not implemented.
	ErrNotImplemented = fmt.Errorf("not implemented")
	// ErrInvalidCompactionChainTipHeight means the newly updated height of
	// the tip of compaction chain is invalid, usually means it's smaller than
	// current cached one.
	ErrInvalidCompactionChainTipHeight = fmt.Errorf(
		"invalid compaction chain tip height")
	// ErrDKGPrivateKeyExists raised when attempting to save DKG private key
	// that already saved.
	ErrDKGPrivateKeyExists = errors.New("dkg private key exists")
	// ErrDKGPrivateKeyDoesNotExist raised when the DKG private key of the
	// requested round does not exists.
	ErrDKGPrivateKeyDoesNotExist = errors.New("dkg private key does not exists")
	// ErrDKGProtocolExists raised when attempting to save DKG protocol
	// that already saved.
	ErrDKGProtocolExists = errors.New("dkg protocol exists")
	// ErrDKGProtocolDoesNotExist raised when the DKG protocol of the
	// requested round does not exists.
	ErrDKGProtocolDoesNotExist = errors.New("dkg protocol does not exists")
)

// Database is the interface for a Database.
type Database interface {
	Reader
	Writer

	// Close allows database implementation able to
	// release resource when finishing.
	Close() error
}

// Reader defines the interface for reading blocks into DB.
type Reader interface {
	HasBlock(hash common.Hash) bool
	GetBlock(hash common.Hash) (types.Block, error)
	GetAllBlocks() (BlockIterator, error)

	// GetCompactionChainTipInfo returns the block hash and finalization height
	// of the tip block of compaction chain. Empty hash and zero height means
	// the compaction chain is empty.
	GetCompactionChainTipInfo() (common.Hash, uint64)

	// DKG Private Key related methods.
	GetDKGPrivateKey(round, reset uint64) (dkg.PrivateKey, error)
	GetDKGProtocol() (dkgProtocol DKGProtocolInfo, err error)
}

// Writer defines the interface for writing blocks into DB.
type Writer interface {
	UpdateBlock(block types.Block) error
	PutBlock(block types.Block) error
	PutCompactionChainTipInfo(common.Hash, uint64) error
	PutDKGPrivateKey(round, reset uint64, pk dkg.PrivateKey) error
	PutOrUpdateDKGProtocol(dkgProtocol DKGProtocolInfo) error
}

// BlockIterator defines an iterator on blocks hold
// in a DB.
type BlockIterator interface {
	NextBlock() (types.Block, error)
}
