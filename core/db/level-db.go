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
	"encoding/binary"
	"io"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon/rlp"
)

var (
	blockKeyPrefix            = []byte("b-")
	compactionChainTipInfoKey = []byte("cc-tip")
	dkgPrivateKeyKeyPrefix    = []byte("dkg-prvs")
	dkgProtocolInfoKeyPrefix  = []byte("dkg-protocol-info")
)

type compactionChainTipInfo struct {
	Height uint64      `json:"height"`
	Hash   common.Hash `json:"hash"`
}

// DKGProtocolInfo DKG protocol info.
type DKGProtocolInfo struct {
	ID                        types.NodeID
	Round                     uint64
	Threshold                 uint64
	IDMap                     NodeIDToDKGID
	MpkMap                    NodeIDToPubShares
	MasterPrivateShare        dkg.PrivateKeyShares
	IsMasterPrivateShareEmpty bool
	PrvShares                 dkg.PrivateKeyShares
	IsPrvSharesEmpty          bool
	PrvSharesReceived         NodeID
	NodeComplained            NodeID
	AntiComplaintReceived     NodeIDToNodeIDs
	Step                      uint64
	Reset                     uint64
}

type dkgPrivateKey struct {
	PK    dkg.PrivateKey
	Reset uint64
}

// Equal compare with target DKGProtocolInfo.
func (info *DKGProtocolInfo) Equal(target *DKGProtocolInfo) bool {
	if !info.ID.Equal(target.ID) ||
		info.Round != target.Round ||
		info.Threshold != target.Threshold ||
		info.IsMasterPrivateShareEmpty != target.IsMasterPrivateShareEmpty ||
		info.IsPrvSharesEmpty != target.IsPrvSharesEmpty ||
		info.Step != target.Step ||
		info.Reset != target.Reset ||
		!info.MasterPrivateShare.Equal(&target.MasterPrivateShare) ||
		!info.PrvShares.Equal(&target.PrvShares) {
		return false
	}

	if len(info.IDMap) != len(target.IDMap) {
		return false
	}
	for k, v := range info.IDMap {
		tV, exist := target.IDMap[k]
		if !exist {
			return false
		}

		if !v.IsEqual(&tV) {
			return false
		}
	}

	if len(info.MpkMap) != len(target.MpkMap) {
		return false
	}
	for k, v := range info.MpkMap {
		tV, exist := target.MpkMap[k]
		if !exist {
			return false
		}

		if !v.Equal(tV) {
			return false
		}
	}

	if len(info.PrvSharesReceived) != len(target.PrvSharesReceived) {
		return false
	}
	for k := range info.PrvSharesReceived {
		_, exist := target.PrvSharesReceived[k]
		if !exist {
			return false
		}
	}

	if len(info.NodeComplained) != len(target.NodeComplained) {
		return false
	}
	for k := range info.NodeComplained {
		_, exist := target.NodeComplained[k]
		if !exist {
			return false
		}
	}

	if len(info.AntiComplaintReceived) != len(target.AntiComplaintReceived) {
		return false
	}
	for k, v := range info.AntiComplaintReceived {
		tV, exist := target.AntiComplaintReceived[k]
		if !exist {
			return false
		}

		if len(v) != len(tV) {
			return false
		}
		for kk := range v {
			_, exist := tV[kk]
			if !exist {
				return false
			}
		}
	}

	return true
}

// NodeIDToNodeIDs the map with NodeID to NodeIDs.
type NodeIDToNodeIDs map[types.NodeID]map[types.NodeID]struct{}

// EncodeRLP implements rlp.Encoder
func (m NodeIDToNodeIDs) EncodeRLP(w io.Writer) error {
	var allBytes [][][]byte
	for k, v := range m {
		kBytes, err := k.MarshalText()
		if err != nil {
			return err
		}
		allBytes = append(allBytes, [][]byte{kBytes})

		var vBytes [][]byte
		for subK := range v {
			bytes, err := subK.MarshalText()
			if err != nil {
				return err
			}
			vBytes = append(vBytes, bytes)
		}
		allBytes = append(allBytes, vBytes)
	}

	return rlp.Encode(w, allBytes)
}

// DecodeRLP implements rlp.Encoder
func (m *NodeIDToNodeIDs) DecodeRLP(s *rlp.Stream) error {
	*m = make(NodeIDToNodeIDs)
	var dec [][][]byte
	if err := s.Decode(&dec); err != nil {
		return err
	}

	for i := 0; i < len(dec); i += 2 {
		key := types.NodeID{}
		err := key.UnmarshalText(dec[i][0])
		if err != nil {
			return err
		}

		valueMap := map[types.NodeID]struct{}{}
		for _, v := range dec[i+1] {
			value := types.NodeID{}
			err := value.UnmarshalText(v)
			if err != nil {
				return err
			}

			valueMap[value] = struct{}{}
		}

		(*m)[key] = valueMap
	}

	return nil
}

// NodeID the map with NodeID.
type NodeID map[types.NodeID]struct{}

// EncodeRLP implements rlp.Encoder
func (m NodeID) EncodeRLP(w io.Writer) error {
	var allBytes [][]byte
	for k := range m {
		kBytes, err := k.MarshalText()
		if err != nil {
			return err
		}
		allBytes = append(allBytes, kBytes)
	}

	return rlp.Encode(w, allBytes)
}

// DecodeRLP implements rlp.Encoder
func (m *NodeID) DecodeRLP(s *rlp.Stream) error {
	*m = make(NodeID)
	var dec [][]byte
	if err := s.Decode(&dec); err != nil {
		return err
	}

	for i := 0; i < len(dec); i++ {
		key := types.NodeID{}
		err := key.UnmarshalText(dec[i])
		if err != nil {
			return err
		}

		(*m)[key] = struct{}{}
	}

	return nil
}

// NodeIDToPubShares the map with NodeID to PublicKeyShares.
type NodeIDToPubShares map[types.NodeID]*dkg.PublicKeyShares

// EncodeRLP implements rlp.Encoder
func (m NodeIDToPubShares) EncodeRLP(w io.Writer) error {
	var allBytes [][]byte
	for k, v := range m {
		kBytes, err := k.MarshalText()
		if err != nil {
			return err
		}
		allBytes = append(allBytes, kBytes)

		bytes, err := rlp.EncodeToBytes(v)
		if err != nil {
			return err
		}
		allBytes = append(allBytes, bytes)
	}

	return rlp.Encode(w, allBytes)
}

// DecodeRLP implements rlp.Encoder
func (m *NodeIDToPubShares) DecodeRLP(s *rlp.Stream) error {
	*m = make(NodeIDToPubShares)
	var dec [][]byte
	if err := s.Decode(&dec); err != nil {
		return err
	}

	for i := 0; i < len(dec); i += 2 {
		key := types.NodeID{}
		err := key.UnmarshalText(dec[i])
		if err != nil {
			return err
		}

		value := dkg.PublicKeyShares{}
		err = rlp.DecodeBytes(dec[i+1], &value)
		if err != nil {
			return err
		}

		(*m)[key] = &value
	}

	return nil
}

// NodeIDToDKGID the map with NodeID to DKGID.
type NodeIDToDKGID map[types.NodeID]dkg.ID

// EncodeRLP implements rlp.Encoder
func (m NodeIDToDKGID) EncodeRLP(w io.Writer) error {
	var allBytes [][]byte
	for k, v := range m {
		kBytes, err := k.MarshalText()
		if err != nil {
			return err
		}
		allBytes = append(allBytes, kBytes)
		allBytes = append(allBytes, v.GetLittleEndian())
	}

	return rlp.Encode(w, allBytes)
}

// DecodeRLP implements rlp.Encoder
func (m *NodeIDToDKGID) DecodeRLP(s *rlp.Stream) error {
	*m = make(NodeIDToDKGID)
	var dec [][]byte
	if err := s.Decode(&dec); err != nil {
		return err
	}

	for i := 0; i < len(dec); i += 2 {
		key := types.NodeID{}
		err := key.UnmarshalText(dec[i])
		if err != nil {
			return err
		}

		value := dkg.ID{}
		err = value.SetLittleEndian(dec[i+1])
		if err != nil {
			return err
		}

		(*m)[key] = value
	}

	return nil
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
		panic(err)
	}
	return exists
}

func (lvl *LevelDBBackedDB) internalHasBlock(key []byte) (bool, error) {
	return lvl.db.Has(key, nil)
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
	err = lvl.db.Put(blockKey, marshaled, nil)
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
	err = lvl.db.Put(blockKey, marshaled, nil)
	return
}

// GetAllBlocks implements Reader.GetAllBlocks method, which allows callers
// to retrieve all blocks in DB.
func (lvl *LevelDBBackedDB) GetAllBlocks() (BlockIterator, error) {
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
	if info.Height+1 != height {
		return ErrInvalidCompactionChainTipHeight
	}
	return lvl.db.Put(compactionChainTipInfoKey, marshaled, nil)
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
	err = rlp.DecodeBytes(queried, &info)
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

// GetDKGPrivateKey get DKG private key of one round.
func (lvl *LevelDBBackedDB) GetDKGPrivateKey(round, reset uint64) (
	prv dkg.PrivateKey, err error) {
	queried, err := lvl.db.Get(lvl.getDKGPrivateKeyKey(round), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			err = ErrDKGPrivateKeyDoesNotExist
		}
		return
	}
	pk := dkgPrivateKey{}
	err = rlp.DecodeBytes(queried, &pk)
	if pk.Reset != reset {
		err = ErrDKGPrivateKeyDoesNotExist
		return
	}
	prv = pk.PK
	return
}

// PutDKGPrivateKey save DKG private key of one round.
func (lvl *LevelDBBackedDB) PutDKGPrivateKey(
	round, reset uint64, prv dkg.PrivateKey) error {
	// Check existence.
	_, err := lvl.GetDKGPrivateKey(round, reset)
	if err == nil {
		return ErrDKGPrivateKeyExists
	}
	if err != ErrDKGPrivateKeyDoesNotExist {
		return err
	}
	pk := &dkgPrivateKey{
		PK:    prv,
		Reset: reset,
	}
	marshaled, err := rlp.EncodeToBytes(&pk)
	if err != nil {
		return err
	}
	return lvl.db.Put(
		lvl.getDKGPrivateKeyKey(round), marshaled, nil)
}

// GetDKGProtocol get DKG protocol.
func (lvl *LevelDBBackedDB) GetDKGProtocol() (
	info DKGProtocolInfo, err error) {
	queried, err := lvl.db.Get(lvl.getDKGProtocolInfoKey(), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			err = ErrDKGProtocolDoesNotExist
		}
		return
	}

	err = rlp.DecodeBytes(queried, &info)
	return
}

// PutOrUpdateDKGProtocol save DKG protocol.
func (lvl *LevelDBBackedDB) PutOrUpdateDKGProtocol(info DKGProtocolInfo) error {
	marshaled, err := rlp.EncodeToBytes(&info)
	if err != nil {
		return err
	}
	return lvl.db.Put(lvl.getDKGProtocolInfoKey(), marshaled, nil)
}

func (lvl *LevelDBBackedDB) getBlockKey(hash common.Hash) (ret []byte) {
	ret = make([]byte, len(blockKeyPrefix)+len(hash[:]))
	copy(ret, blockKeyPrefix)
	copy(ret[len(blockKeyPrefix):], hash[:])
	return
}

func (lvl *LevelDBBackedDB) getDKGPrivateKeyKey(
	round uint64) (ret []byte) {
	ret = make([]byte, len(dkgPrivateKeyKeyPrefix)+8)
	copy(ret, dkgPrivateKeyKeyPrefix)
	binary.LittleEndian.PutUint64(
		ret[len(dkgPrivateKeyKeyPrefix):], round)
	return
}

func (lvl *LevelDBBackedDB) getDKGProtocolInfoKey() (ret []byte) {
	ret = make([]byte, len(dkgProtocolInfoKeyPrefix)+8)
	copy(ret, dkgProtocolInfoKeyPrefix)
	return
}
