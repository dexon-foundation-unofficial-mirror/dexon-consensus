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

// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package common

import (
	"bytes"
	"encoding/hex"
	"sort"
	"time"
)

const (
	// HashLength is the length of a hash in DEXON.
	HashLength = 32
)

// Hash is the basic hash type in DEXON.
type Hash [HashLength]byte

func (h Hash) String() string {
	return hex.EncodeToString([]byte(h[:]))
}

// Bytes return the hash as slice of bytes.
func (h Hash) Bytes() []byte {
	return h[:]
}

// Equal compares if two hashes are the same.
func (h Hash) Equal(hp Hash) bool {
	return h == hp
}

// Less compares if current hash is lesser.
func (h Hash) Less(hp Hash) bool {
	return bytes.Compare(h[:], hp[:]) < 0
}

// MarshalText implements the encoding.TextMarhsaler interface.
func (h Hash) MarshalText() ([]byte, error) {
	result := make([]byte, hex.EncodedLen(HashLength))
	hex.Encode(result, h[:])
	return result, nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (h *Hash) UnmarshalText(text []byte) error {
	_, err := hex.Decode(h[:], text)
	return err
}

// Hashes is for sorting hashes.
type Hashes []Hash

func (hs Hashes) Len() int           { return len(hs) }
func (hs Hashes) Less(i, j int) bool { return hs[i].Less(hs[j]) }
func (hs Hashes) Swap(i, j int)      { hs[i], hs[j] = hs[j], hs[i] }

// SortedHashes is a slice of hashes sorted in ascending order.
type SortedHashes Hashes

// NewSortedHashes converts a slice of hashes to a sorted one. It's a
// firewall to prevent us from assigning unsorted hashes to a variable
// declared as SortedHashes directly.
func NewSortedHashes(hs Hashes) SortedHashes {
	sort.Sort(hs)
	return SortedHashes(hs)
}

// ByTime implements sort.Interface for time.Time.
type ByTime []time.Time

func (t ByTime) Len() int           { return len(t) }
func (t ByTime) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }
func (t ByTime) Less(i, j int) bool { return t[i].Before(t[j]) }
