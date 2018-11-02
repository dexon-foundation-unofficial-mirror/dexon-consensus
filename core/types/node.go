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

package types

import (
	"bytes"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
)

// NodeID is the ID type for nodes.
type NodeID struct {
	common.Hash
}

// NewNodeID returns a NodeID with Hash set to the hash value of
// public key.
func NewNodeID(pubKey crypto.PublicKey) NodeID {
	return NodeID{Hash: crypto.Keccak256Hash(pubKey.Bytes()[1:])}
}

// Equal checks if the hash representation is the same NodeID.
func (v NodeID) Equal(v2 NodeID) bool {
	return v.Hash == v2.Hash
}

// NodeIDs implements sort.Interface for NodeID.
type NodeIDs []NodeID

func (v NodeIDs) Len() int {
	return len(v)
}

func (v NodeIDs) Less(i int, j int) bool {
	return bytes.Compare([]byte(v[i].Hash[:]), []byte(v[j].Hash[:])) == -1
}

func (v NodeIDs) Swap(i int, j int) {
	v[i], v[j] = v[j], v[i]
}
