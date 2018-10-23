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

package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dexon-foundation/dexon/rlp"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/dkg"
)

// DKGPrivateShare describe a secret share in DKG protocol.
type DKGPrivateShare struct {
	ProposerID   NodeID           `json:"proposer_id"`
	ReceiverID   NodeID           `json:"receiver_id"`
	Round        uint64           `json:"round"`
	PrivateShare dkg.PrivateKey   `json:"private_share"`
	Signature    crypto.Signature `json:"signature"`
}

// Equal checks equality between two DKGPrivateShare instances.
func (p *DKGPrivateShare) Equal(other *DKGPrivateShare) bool {
	return p.ProposerID.Equal(other.ProposerID) &&
		p.ReceiverID.Equal(other.ReceiverID) &&
		p.Round == other.Round &&
		p.Signature.Type == other.Signature.Type &&
		bytes.Compare(p.Signature.Signature, other.Signature.Signature) == 0 &&
		bytes.Compare(
			p.PrivateShare.Bytes(), other.PrivateShare.Bytes()) == 0
}

// DKGMasterPublicKey decrtibe a master public key in DKG protocol.
type DKGMasterPublicKey struct {
	ProposerID      NodeID              `json:"proposer_id"`
	Round           uint64              `json:"round"`
	DKGID           dkg.ID              `json:"dkg_id"`
	PublicKeyShares dkg.PublicKeyShares `json:"public_key_shares"`
	Signature       crypto.Signature    `json:"signature"`
}

func (d *DKGMasterPublicKey) String() string {
	return fmt.Sprintf("MasterPublicKey[%s:%d]",
		d.ProposerID.String()[:6],
		d.Round)
}

// Equal check equality of two DKG master public keys.
func (d *DKGMasterPublicKey) Equal(other *DKGMasterPublicKey) bool {
	return d.ProposerID.Equal(other.ProposerID) &&
		d.Round == other.Round &&
		d.DKGID.GetHexString() == other.DKGID.GetHexString() &&
		d.PublicKeyShares.Equal(&other.PublicKeyShares) &&
		d.Signature.Type == other.Signature.Type &&
		bytes.Compare(d.Signature.Signature, other.Signature.Signature) == 0
}

type rlpDKGMasterPublicKey struct {
	ProposerID      NodeID
	Round           uint64
	DKGID           []byte
	PublicKeyShares *dkg.PublicKeyShares
	Signature       crypto.Signature
}

// EncodeRLP implements rlp.Encoder
func (d *DKGMasterPublicKey) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, rlpDKGMasterPublicKey{
		ProposerID:      d.ProposerID,
		Round:           d.Round,
		DKGID:           d.DKGID.GetLittleEndian(),
		PublicKeyShares: &d.PublicKeyShares,
		Signature:       d.Signature,
	})
}

// DecodeRLP implements rlp.Decoder
func (d *DKGMasterPublicKey) DecodeRLP(s *rlp.Stream) error {
	var dec rlpDKGMasterPublicKey
	if err := s.Decode(&dec); err != nil {
		return err
	}

	id, err := dkg.BytesID(dec.DKGID)
	if err != nil {
		return err
	}

	*d = DKGMasterPublicKey{
		ProposerID:      dec.ProposerID,
		Round:           dec.Round,
		DKGID:           id,
		PublicKeyShares: *dec.PublicKeyShares,
		Signature:       dec.Signature,
	}
	return err
}

// NewDKGMasterPublicKey returns a new DKGMasterPublicKey instance.
func NewDKGMasterPublicKey() *DKGMasterPublicKey {
	return &DKGMasterPublicKey{
		PublicKeyShares: *dkg.NewEmptyPublicKeyShares(),
	}
}

// UnmarshalJSON implements json.Unmarshaller.
func (d *DKGMasterPublicKey) UnmarshalJSON(data []byte) error {
	type innertDKGMasterPublicKey DKGMasterPublicKey
	d.PublicKeyShares = *dkg.NewEmptyPublicKeyShares()
	return json.Unmarshal(data, (*innertDKGMasterPublicKey)(d))
}

// DKGComplaint describe a complaint in DKG protocol.
type DKGComplaint struct {
	ProposerID   NodeID           `json:"proposer_id"`
	Round        uint64           `json:"round"`
	PrivateShare DKGPrivateShare  `json:"private_share"`
	Signature    crypto.Signature `json:"signature"`
}

func (c *DKGComplaint) String() string {
	if c.IsNack() {
		return fmt.Sprintf("DKGNackComplaint[%s:%d]%s",
			c.ProposerID.String()[:6], c.Round,
			c.PrivateShare.ProposerID.String()[:6])
	}
	return fmt.Sprintf("DKGComplaint[%s:%d]%v",
		c.ProposerID.String()[:6], c.Round, c.PrivateShare)
}

// Equal checks equality between two DKGComplaint instances.
func (c *DKGComplaint) Equal(other *DKGComplaint) bool {
	return c.ProposerID.Equal(other.ProposerID) &&
		c.Round == other.Round &&
		c.PrivateShare.Equal(&other.PrivateShare) &&
		c.Signature.Type == other.Signature.Type &&
		bytes.Compare(c.Signature.Signature, other.Signature.Signature) == 0
}

// DKGPartialSignature describe a partial signature in DKG protocol.
type DKGPartialSignature struct {
	ProposerID       NodeID               `json:"proposer_id"`
	Round            uint64               `json:"round"`
	Hash             common.Hash          `json:"hash"`
	PartialSignature dkg.PartialSignature `json:"partial_signature"`
	Signature        crypto.Signature     `json:"signature"`
}

// DKGFinalize describe a dig finalize message in DKG protocol.
type DKGFinalize struct {
	ProposerID NodeID           `json:"proposer_id"`
	Round      uint64           `json:"round"`
	Signature  crypto.Signature `json:"signature"`
}

func (final *DKGFinalize) String() string {
	return fmt.Sprintf("DKGFinal[%s:%d]",
		final.ProposerID.String()[:6],
		final.Round)
}

// Equal check equality of two DKGFinalize instances.
func (final *DKGFinalize) Equal(other *DKGFinalize) bool {
	return final.ProposerID.Equal(other.ProposerID) &&
		final.Round == other.Round &&
		final.Signature.Type == other.Signature.Type &&
		bytes.Compare(final.Signature.Signature, other.Signature.Signature) == 0
}

// IsNack returns true if it's a nack complaint in DKG protocol.
func (c *DKGComplaint) IsNack() bool {
	return len(c.PrivateShare.Signature.Signature) == 0
}
