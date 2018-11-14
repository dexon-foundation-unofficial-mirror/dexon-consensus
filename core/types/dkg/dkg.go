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

package dkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dexon-foundation/dexon/rlp"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	cryptoDKG "github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// PrivateShare describe a secret share in DKG protocol.
type PrivateShare struct {
	ProposerID   types.NodeID         `json:"proposer_id"`
	ReceiverID   types.NodeID         `json:"receiver_id"`
	Round        uint64               `json:"round"`
	PrivateShare cryptoDKG.PrivateKey `json:"private_share"`
	Signature    crypto.Signature     `json:"signature"`
}

// Equal checks equality between two PrivateShare instances.
func (p *PrivateShare) Equal(other *PrivateShare) bool {
	return p.ProposerID.Equal(other.ProposerID) &&
		p.ReceiverID.Equal(other.ReceiverID) &&
		p.Round == other.Round &&
		p.Signature.Type == other.Signature.Type &&
		bytes.Compare(p.Signature.Signature, other.Signature.Signature) == 0 &&
		bytes.Compare(
			p.PrivateShare.Bytes(), other.PrivateShare.Bytes()) == 0
}

// MasterPublicKey decrtibe a master public key in DKG protocol.
type MasterPublicKey struct {
	ProposerID      types.NodeID              `json:"proposer_id"`
	Round           uint64                    `json:"round"`
	DKGID           cryptoDKG.ID              `json:"dkg_id"`
	PublicKeyShares cryptoDKG.PublicKeyShares `json:"public_key_shares"`
	Signature       crypto.Signature          `json:"signature"`
}

func (d *MasterPublicKey) String() string {
	return fmt.Sprintf("MasterPublicKey{KP:%s Round:%d}",
		d.ProposerID.String()[:6],
		d.Round)
}

// Equal check equality of two DKG master public keys.
func (d *MasterPublicKey) Equal(other *MasterPublicKey) bool {
	return d.ProposerID.Equal(other.ProposerID) &&
		d.Round == other.Round &&
		d.DKGID.GetHexString() == other.DKGID.GetHexString() &&
		d.PublicKeyShares.Equal(&other.PublicKeyShares) &&
		d.Signature.Type == other.Signature.Type &&
		bytes.Compare(d.Signature.Signature, other.Signature.Signature) == 0
}

type rlpMasterPublicKey struct {
	ProposerID      types.NodeID
	Round           uint64
	DKGID           []byte
	PublicKeyShares *cryptoDKG.PublicKeyShares
	Signature       crypto.Signature
}

// EncodeRLP implements rlp.Encoder
func (d *MasterPublicKey) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, rlpMasterPublicKey{
		ProposerID:      d.ProposerID,
		Round:           d.Round,
		DKGID:           d.DKGID.GetLittleEndian(),
		PublicKeyShares: &d.PublicKeyShares,
		Signature:       d.Signature,
	})
}

// DecodeRLP implements rlp.Decoder
func (d *MasterPublicKey) DecodeRLP(s *rlp.Stream) error {
	var dec rlpMasterPublicKey
	if err := s.Decode(&dec); err != nil {
		return err
	}

	id, err := cryptoDKG.BytesID(dec.DKGID)
	if err != nil {
		return err
	}

	*d = MasterPublicKey{
		ProposerID:      dec.ProposerID,
		Round:           dec.Round,
		DKGID:           id,
		PublicKeyShares: *dec.PublicKeyShares,
		Signature:       dec.Signature,
	}
	return err
}

// NewMasterPublicKey returns a new MasterPublicKey instance.
func NewMasterPublicKey() *MasterPublicKey {
	return &MasterPublicKey{
		PublicKeyShares: *cryptoDKG.NewEmptyPublicKeyShares(),
	}
}

// UnmarshalJSON implements json.Unmarshaller.
func (d *MasterPublicKey) UnmarshalJSON(data []byte) error {
	type innertMasterPublicKey MasterPublicKey
	d.PublicKeyShares = *cryptoDKG.NewEmptyPublicKeyShares()
	return json.Unmarshal(data, (*innertMasterPublicKey)(d))
}

// Complaint describe a complaint in DKG protocol.
type Complaint struct {
	ProposerID   types.NodeID     `json:"proposer_id"`
	Round        uint64           `json:"round"`
	PrivateShare PrivateShare     `json:"private_share"`
	Signature    crypto.Signature `json:"signature"`
}

func (c *Complaint) String() string {
	if c.IsNack() {
		return fmt.Sprintf("DKGNackComplaint{CP:%s Round:%d PSP:%s}",
			c.ProposerID.String()[:6], c.Round,
			c.PrivateShare.ProposerID.String()[:6])
	}
	return fmt.Sprintf("DKGComplaint{CP:%s Round:%d PrivateShare:%v}",
		c.ProposerID.String()[:6], c.Round, c.PrivateShare)
}

// Equal checks equality between two Complaint instances.
func (c *Complaint) Equal(other *Complaint) bool {
	return c.ProposerID.Equal(other.ProposerID) &&
		c.Round == other.Round &&
		c.PrivateShare.Equal(&other.PrivateShare) &&
		c.Signature.Type == other.Signature.Type &&
		bytes.Compare(c.Signature.Signature, other.Signature.Signature) == 0
}

// PartialSignature describe a partial signature in DKG protocol.
type PartialSignature struct {
	ProposerID       types.NodeID               `json:"proposer_id"`
	Round            uint64                     `json:"round"`
	Hash             common.Hash                `json:"hash"`
	PartialSignature cryptoDKG.PartialSignature `json:"partial_signature"`
	Signature        crypto.Signature           `json:"signature"`
}

// Finalize describe a dig finalize message in DKG protocol.
type Finalize struct {
	ProposerID types.NodeID     `json:"proposer_id"`
	Round      uint64           `json:"round"`
	Signature  crypto.Signature `json:"signature"`
}

func (final *Finalize) String() string {
	return fmt.Sprintf("DKGFinal{FP:%s Round:%d}",
		final.ProposerID.String()[:6],
		final.Round)
}

// Equal check equality of two Finalize instances.
func (final *Finalize) Equal(other *Finalize) bool {
	return final.ProposerID.Equal(other.ProposerID) &&
		final.Round == other.Round &&
		final.Signature.Type == other.Signature.Type &&
		bytes.Compare(final.Signature.Signature, other.Signature.Signature) == 0
}

// IsNack returns true if it's a nack complaint in DKG protocol.
func (c *Complaint) IsNack() bool {
	return len(c.PrivateShare.Signature.Signature) == 0
}
