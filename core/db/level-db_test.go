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
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"time"

	"os"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon/rlp"
)

type LevelDBTestSuite struct {
	suite.Suite
}

func (s *LevelDBTestSuite) TestBasicUsage() {
	dbName := fmt.Sprintf("test-db-%v.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
	}(dbName)

	// Queried something from an empty database.
	hash1 := common.NewRandomHash()
	_, err = dbInst.GetBlock(hash1)
	s.Equal(ErrBlockDoesNotExist, err)

	// Update on an empty database should not success.
	node1 := types.NodeID{Hash: common.NewRandomHash()}
	block1 := types.Block{
		ProposerID: node1,
		Hash:       hash1,
		Position: types.Position{
			Height: 1,
		},
	}
	err = dbInst.UpdateBlock(block1)
	s.Equal(ErrBlockDoesNotExist, err)

	// Put to create a new record should just work fine.
	err = dbInst.PutBlock(block1)
	s.NoError(err)

	// Get it back should work fine.
	queried, err := dbInst.GetBlock(block1.Hash)
	s.NoError(err)
	s.Equal(queried.ProposerID, block1.ProposerID)

	// Test Update.
	now := time.Now().UTC()
	queried.Timestamp = now

	err = dbInst.UpdateBlock(queried)
	s.NoError(err)

	// Try to get it back via NodeID and height.
	queried, err = dbInst.GetBlock(block1.Hash)

	s.NoError(err)
	s.Equal(now, queried.Timestamp)
}

func (s *LevelDBTestSuite) TestSyncIndex() {
	dbName := fmt.Sprintf("test-db-%v-si.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
	}(dbName)

	// Create some blocks.
	blocks := [10]types.Block{}
	for i := range blocks {
		block := types.Block{
			ProposerID: types.NodeID{Hash: common.NewRandomHash()},
			Hash:       common.NewRandomHash(),
			Position: types.Position{
				Height: uint64(i),
			},
		}
		dbInst.PutBlock(block)
		blocks[i] = block
	}

	// Save blocks to db.
	err = dbInst.Close()
	s.NoError(err)

	// Load back blocks(syncIndex is called).
	dbInst, err = NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)

	// Verify result.
	for _, block := range blocks {
		queried, err := dbInst.GetBlock(block.Hash)
		s.NoError(err)
		s.Equal(block.ProposerID, queried.ProposerID)
		s.Equal(block.Position.Height, queried.Position.Height)
	}
}

func (s *LevelDBTestSuite) TestCompactionChainTipInfo() {
	dbName := fmt.Sprintf("test-db-%v-cc-tip.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
	}(dbName)
	// Save some tip info.
	hash := common.NewRandomHash()
	s.Require().NoError(dbInst.PutCompactionChainTipInfo(hash, 1))
	// Get it back to check.
	hashBack, height := dbInst.GetCompactionChainTipInfo()
	s.Require().Equal(hash, hashBack)
	s.Require().Equal(height, uint64(1))
	// Unable to put compaction chain tip info with lower height.
	err = dbInst.PutCompactionChainTipInfo(hash, 0)
	s.Require().Equal(err.Error(), ErrInvalidCompactionChainTipHeight.Error())
	// Unable to put compaction chain tip info with height not incremental by 1.
	err = dbInst.PutCompactionChainTipInfo(hash, 3)
	s.Require().Equal(err.Error(), ErrInvalidCompactionChainTipHeight.Error())
	// It's OK to put compaction chain tip info with height incremental by 1.
	s.Require().NoError(dbInst.PutCompactionChainTipInfo(hash, 2))
}

func (s *LevelDBTestSuite) TestDKGPrivateKey() {
	dbName := fmt.Sprintf("test-db-%v-dkg-prv.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
	}(dbName)
	p := dkg.NewPrivateKey()
	// We should be unable to get it.
	_, err = dbInst.GetDKGPrivateKey(1, 0)
	s.Require().Equal(err.Error(), ErrDKGPrivateKeyDoesNotExist.Error())
	// Put it.
	s.Require().NoError(dbInst.PutDKGPrivateKey(1, 0, *p))
	// We should be unable to get it because reset is different.
	_, err = dbInst.GetDKGPrivateKey(1, 1)
	s.Require().Equal(err.Error(), ErrDKGPrivateKeyDoesNotExist.Error())
	// Put it again, should not success.
	err = dbInst.PutDKGPrivateKey(1, 0, *p)
	s.Require().Equal(err.Error(), ErrDKGPrivateKeyExists.Error())
	// Get it back.
	tmpPrv, err := dbInst.GetDKGPrivateKey(1, 0)
	s.Require().NoError(err)
	s.Require().Equal(bytes.Compare(p.Bytes(), tmpPrv.Bytes()), 0)
	// Put it at different reset.
	p2 := dkg.NewPrivateKey()
	s.Require().NoError(dbInst.PutDKGPrivateKey(1, 1, *p2))
	// We should be unable to get it because reset is different.
	_, err = dbInst.GetDKGPrivateKey(1, 0)
	// Get it back.
	tmpPrv, err = dbInst.GetDKGPrivateKey(1, 1)
	s.Require().NoError(err)
	s.Require().Equal(bytes.Compare(p2.Bytes(), tmpPrv.Bytes()), 0)
	s.Require().NotEqual(bytes.Compare(p2.Bytes(), p.Bytes()), 0)
}

func (s *LevelDBTestSuite) TestDKGProtocol() {
	dbName := fmt.Sprintf("test-db-%v-dkg-master-prv-shares.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
	}(dbName)

	_, err = dbInst.GetDKGProtocol()
	s.Require().Equal(err.Error(), ErrDKGProtocolDoesNotExist.Error())

	s.Require().NoError(dbInst.PutOrUpdateDKGProtocol(DKGProtocolInfo{}))
}

func (s *LevelDBTestSuite) TestDKGProtocolInfoRLPEncodeDecode() {
	protocol := DKGProtocolInfo{
		ID:        types.NodeID{Hash: common.Hash{0x11}},
		Round:     5,
		Threshold: 10,
		IDMap: NodeIDToDKGID{
			types.NodeID{Hash: common.Hash{0x01}}: dkg.ID{},
			types.NodeID{Hash: common.Hash{0x02}}: dkg.ID{},
		},
		MpkMap: NodeIDToPubShares{
			types.NodeID{Hash: common.Hash{0x01}}: dkg.NewEmptyPublicKeyShares(),
			types.NodeID{Hash: common.Hash{0x02}}: dkg.NewEmptyPublicKeyShares(),
		},
		AntiComplaintReceived: NodeIDToNodeIDs{
			types.NodeID{Hash: common.Hash{0x01}}: map[types.NodeID]struct{}{
				types.NodeID{Hash: common.Hash{0x02}}: {},
			},
			types.NodeID{Hash: common.Hash{0x03}}: map[types.NodeID]struct{}{
				types.NodeID{Hash: common.Hash{0x04}}: {},
			},
		},
		PrvSharesReceived: NodeID{
			types.NodeID{Hash: common.Hash{0x01}}: struct{}{},
		},
	}

	b, err := rlp.EncodeToBytes(&protocol)
	s.Require().NoError(err)

	newProtocol := DKGProtocolInfo{}
	err = rlp.DecodeBytes(b, &newProtocol)
	s.Require().NoError(err)

	s.Require().True(protocol.Equal(&newProtocol))
}

func (s *LevelDBTestSuite) TestNodeIDToNodeIDsRLPEncodeDecode() {
	m := NodeIDToNodeIDs{
		types.NodeID{Hash: common.Hash{0x01}}: map[types.NodeID]struct{}{
			types.NodeID{Hash: common.Hash{0x02}}: {},
		},
		types.NodeID{Hash: common.Hash{0x03}}: map[types.NodeID]struct{}{
			types.NodeID{Hash: common.Hash{0x04}}: {},
		},
	}

	b, err := rlp.EncodeToBytes(&m)
	s.Require().NoError(err)

	newM := NodeIDToNodeIDs{}
	err = rlp.DecodeBytes(b, &newM)
	s.Require().NoError(err)

	s.Require().True(reflect.DeepEqual(m, newM))
}

func (s *LevelDBTestSuite) TestNodeIDRLPEncodeDecode() {
	m := NodeID{
		types.NodeID{Hash: common.Hash{0x01}}: struct{}{},
		types.NodeID{Hash: common.Hash{0x02}}: struct{}{},
	}

	b, err := rlp.EncodeToBytes(&m)
	s.Require().NoError(err)

	newM := NodeID{}
	err = rlp.DecodeBytes(b, &newM)
	s.Require().NoError(err)

	s.Require().True(reflect.DeepEqual(m, newM))
}

func (s *LevelDBTestSuite) TestNodeIDToPubSharesRLPEncodeDecode() {
	m := NodeIDToPubShares{
		types.NodeID{Hash: common.Hash{0x01}}: dkg.NewEmptyPublicKeyShares(),
		types.NodeID{Hash: common.Hash{0x02}}: dkg.NewEmptyPublicKeyShares(),
	}

	b, err := rlp.EncodeToBytes(&m)
	s.Require().NoError(err)

	newM := NodeIDToPubShares{}
	err = rlp.DecodeBytes(b, &newM)
	s.Require().NoError(err)

	for k, v := range m {
		newV, exist := newM[k]
		s.Require().True(exist)
		s.Require().True(newV.Equal(v))
	}
}

func (s *LevelDBTestSuite) TestNodeIDToDKGIDRLPEncodeDecode() {
	m := NodeIDToDKGID{
		types.NodeID{Hash: common.Hash{0x01}}: dkg.ID{},
		types.NodeID{Hash: common.Hash{0x02}}: dkg.ID{},
	}

	b, err := rlp.EncodeToBytes(&m)
	s.Require().NoError(err)

	newM := NodeIDToDKGID{}
	err = rlp.DecodeBytes(b, &newM)
	s.Require().NoError(err)

	s.Require().True(reflect.DeepEqual(m, newM))
}

func TestLevelDB(t *testing.T) {
	suite.Run(t, new(LevelDBTestSuite))
}
