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
	"encoding/json"
	"fmt"

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

func (p *DKGPrivateShare) String() string {
	return fmt.Sprintf("prvShare(%d:%s->%s:%s:%s)",
		p.Round,
		p.ProposerID.String()[:6],
		p.ReceiverID.String()[:6],
		p.PrivateShare.String(),
		p.Signature.String()[:6])
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
	return fmt.Sprintf("DKGComplaint[%s:%d]%s",
		c.ProposerID.String()[:6], c.Round, &c.PrivateShare)
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

// IsNack returns true if it's a nack complaint in DKG protocol.
func (c *DKGComplaint) IsNack() bool {
	return len(c.PrivateShare.Signature.Signature) == 0
}
