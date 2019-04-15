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

// Errors for typesDKG package.
var (
	ErrNotReachThreshold = fmt.Errorf("threshold not reach")
	ErrInvalidThreshold  = fmt.Errorf("invalid threshold")
)

// NewID creates a DKGID from NodeID.
func NewID(ID types.NodeID) cryptoDKG.ID {
	return cryptoDKG.NewID(ID.Hash[:])
}

// PrivateShare describe a secret share in DKG protocol.
type PrivateShare struct {
	ProposerID   types.NodeID         `json:"proposer_id"`
	ReceiverID   types.NodeID         `json:"receiver_id"`
	Round        uint64               `json:"round"`
	Reset        uint64               `json:"reset"`
	PrivateShare cryptoDKG.PrivateKey `json:"private_share"`
	Signature    crypto.Signature     `json:"signature"`
}

// Equal checks equality between two PrivateShare instances.
func (p *PrivateShare) Equal(other *PrivateShare) bool {
	return p.ProposerID.Equal(other.ProposerID) &&
		p.ReceiverID.Equal(other.ReceiverID) &&
		p.Round == other.Round &&
		p.Reset == other.Reset &&
		p.Signature.Type == other.Signature.Type &&
		bytes.Compare(p.Signature.Signature, other.Signature.Signature) == 0 &&
		bytes.Compare(
			p.PrivateShare.Bytes(), other.PrivateShare.Bytes()) == 0
}

// MasterPublicKey decrtibe a master public key in DKG protocol.
type MasterPublicKey struct {
	ProposerID      types.NodeID              `json:"proposer_id"`
	Round           uint64                    `json:"round"`
	Reset           uint64                    `json:"reset"`
	DKGID           cryptoDKG.ID              `json:"dkg_id"`
	PublicKeyShares cryptoDKG.PublicKeyShares `json:"public_key_shares"`
	Signature       crypto.Signature          `json:"signature"`
}

func (d *MasterPublicKey) String() string {
	return fmt.Sprintf("MasterPublicKey{KP:%s Round:%d Reset:%d}",
		d.ProposerID.String()[:6],
		d.Round,
		d.Reset)
}

// Equal check equality of two DKG master public keys.
func (d *MasterPublicKey) Equal(other *MasterPublicKey) bool {
	return d.ProposerID.Equal(other.ProposerID) &&
		d.Round == other.Round &&
		d.Reset == other.Reset &&
		d.DKGID.GetHexString() == other.DKGID.GetHexString() &&
		d.PublicKeyShares.Equal(&other.PublicKeyShares) &&
		d.Signature.Type == other.Signature.Type &&
		bytes.Compare(d.Signature.Signature, other.Signature.Signature) == 0
}

type rlpMasterPublicKey struct {
	ProposerID      types.NodeID
	Round           uint64
	Reset           uint64
	DKGID           []byte
	PublicKeyShares *cryptoDKG.PublicKeyShares
	Signature       crypto.Signature
}

// EncodeRLP implements rlp.Encoder
func (d *MasterPublicKey) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, rlpMasterPublicKey{
		ProposerID:      d.ProposerID,
		Round:           d.Round,
		Reset:           d.Reset,
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
		Reset:           dec.Reset,
		DKGID:           id,
		PublicKeyShares: *dec.PublicKeyShares.Move(),
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
	Reset        uint64           `json:"reset"`
	PrivateShare PrivateShare     `json:"private_share"`
	Signature    crypto.Signature `json:"signature"`
}

func (c *Complaint) String() string {
	if c.IsNack() {
		return fmt.Sprintf("DKGNackComplaint{CP:%s Round:%d Reset %d PSP:%s}",
			c.ProposerID.String()[:6], c.Round, c.Reset,
			c.PrivateShare.ProposerID.String()[:6])
	}
	return fmt.Sprintf("DKGComplaint{CP:%s Round:%d Reset %d PrivateShare:%v}",
		c.ProposerID.String()[:6], c.Round, c.Reset, c.PrivateShare)
}

// Equal checks equality between two Complaint instances.
func (c *Complaint) Equal(other *Complaint) bool {
	return c.ProposerID.Equal(other.ProposerID) &&
		c.Round == other.Round &&
		c.Reset == other.Reset &&
		c.PrivateShare.Equal(&other.PrivateShare) &&
		c.Signature.Type == other.Signature.Type &&
		bytes.Compare(c.Signature.Signature, other.Signature.Signature) == 0
}

type rlpComplaint struct {
	ProposerID   types.NodeID
	Round        uint64
	Reset        uint64
	IsNack       bool
	PrivateShare []byte
	Signature    crypto.Signature
}

// EncodeRLP implements rlp.Encoder
func (c *Complaint) EncodeRLP(w io.Writer) error {
	if c.IsNack() {
		return rlp.Encode(w, rlpComplaint{
			ProposerID:   c.ProposerID,
			Round:        c.Round,
			Reset:        c.Reset,
			IsNack:       true,
			PrivateShare: c.PrivateShare.ProposerID.Hash[:],
			Signature:    c.Signature,
		})
	}
	prvShare, err := rlp.EncodeToBytes(&c.PrivateShare)
	if err != nil {
		return err
	}
	return rlp.Encode(w, rlpComplaint{
		ProposerID:   c.ProposerID,
		Round:        c.Round,
		Reset:        c.Reset,
		IsNack:       false,
		PrivateShare: prvShare,
		Signature:    c.Signature,
	})
}

// DecodeRLP implements rlp.Decoder
func (c *Complaint) DecodeRLP(s *rlp.Stream) error {
	var dec rlpComplaint
	if err := s.Decode(&dec); err != nil {
		return err
	}

	var prvShare PrivateShare
	if dec.IsNack {
		copy(prvShare.ProposerID.Hash[:], dec.PrivateShare)
		prvShare.Round = dec.Round
		prvShare.Reset = dec.Reset
	} else {
		if err := rlp.DecodeBytes(dec.PrivateShare, &prvShare); err != nil {
			return err
		}
	}

	*c = Complaint{
		ProposerID:   dec.ProposerID,
		Round:        dec.Round,
		Reset:        dec.Reset,
		PrivateShare: prvShare,
		Signature:    dec.Signature,
	}
	return nil
}

// IsNack returns true if it's a nack complaint in DKG protocol.
func (c *Complaint) IsNack() bool {
	return len(c.PrivateShare.Signature.Signature) == 0
}

// PartialSignature describe a partial signature in DKG protocol.
type PartialSignature struct {
	ProposerID       types.NodeID               `json:"proposer_id"`
	Round            uint64                     `json:"round"`
	Hash             common.Hash                `json:"hash"`
	PartialSignature cryptoDKG.PartialSignature `json:"partial_signature"`
	Signature        crypto.Signature           `json:"signature"`
}

// MPKReady describe a dkg ready message in DKG protocol.
type MPKReady struct {
	ProposerID types.NodeID     `json:"proposer_id"`
	Round      uint64           `json:"round"`
	Reset      uint64           `json:"reset"`
	Signature  crypto.Signature `json:"signature"`
}

func (ready *MPKReady) String() string {
	return fmt.Sprintf("DKGMPKReady{RP:%s Round:%d Reset:%d}",
		ready.ProposerID.String()[:6],
		ready.Round,
		ready.Reset)
}

// Equal check equality of two MPKReady instances.
func (ready *MPKReady) Equal(other *MPKReady) bool {
	return ready.ProposerID.Equal(other.ProposerID) &&
		ready.Round == other.Round &&
		ready.Reset == other.Reset &&
		ready.Signature.Type == other.Signature.Type &&
		bytes.Compare(ready.Signature.Signature, other.Signature.Signature) == 0
}

// Finalize describe a dkg finalize message in DKG protocol.
type Finalize struct {
	ProposerID types.NodeID     `json:"proposer_id"`
	Round      uint64           `json:"round"`
	Reset      uint64           `json:"reset"`
	Signature  crypto.Signature `json:"signature"`
}

func (final *Finalize) String() string {
	return fmt.Sprintf("DKGFinal{FP:%s Round:%d Reset:%d}",
		final.ProposerID.String()[:6],
		final.Round,
		final.Reset)
}

// Equal check equality of two Finalize instances.
func (final *Finalize) Equal(other *Finalize) bool {
	return final.ProposerID.Equal(other.ProposerID) &&
		final.Round == other.Round &&
		final.Reset == other.Reset &&
		final.Signature.Type == other.Signature.Type &&
		bytes.Compare(final.Signature.Signature, other.Signature.Signature) == 0
}

// Success describe a dkg success message in DKG protocol.
type Success struct {
	ProposerID types.NodeID     `json:"proposer_id"`
	Round      uint64           `json:"round"`
	Reset      uint64           `json:"reset"`
	Signature  crypto.Signature `json:"signature"`
}

func (s *Success) String() string {
	return fmt.Sprintf("DKGSuccess{SP:%s Round:%d Reset:%d}",
		s.ProposerID.String()[:6],
		s.Round,
		s.Reset)
}

// Equal check equality of two Success instances.
func (s *Success) Equal(other *Success) bool {
	return s.ProposerID.Equal(other.ProposerID) &&
		s.Round == other.Round &&
		s.Reset == other.Reset &&
		s.Signature.Type == other.Signature.Type &&
		bytes.Compare(s.Signature.Signature, other.Signature.Signature) == 0
}

// GroupPublicKey is the result of DKG protocol.
type GroupPublicKey struct {
	Round          uint64
	QualifyIDs     cryptoDKG.IDs
	QualifyNodeIDs map[types.NodeID]struct{}
	IDMap          map[types.NodeID]cryptoDKG.ID
	GroupPublicKey *cryptoDKG.PublicKey
	Threshold      int
}

// VerifySignature verifies if the signature is correct.
func (gpk *GroupPublicKey) VerifySignature(
	hash common.Hash, sig crypto.Signature) bool {
	return gpk.GroupPublicKey.VerifySignature(hash, sig)
}

// CalcQualifyNodes returns the qualified nodes.
func CalcQualifyNodes(
	mpks []*MasterPublicKey, complaints []*Complaint, threshold int) (
	qualifyIDs cryptoDKG.IDs, qualifyNodeIDs map[types.NodeID]struct{}, err error) {
	if len(mpks) < threshold {
		err = ErrInvalidThreshold
		return
	}

	// Calculate qualify members.
	disqualifyIDs := map[types.NodeID]struct{}{}
	complaintsByID := map[types.NodeID]map[types.NodeID]struct{}{}
	for _, complaint := range complaints {
		if complaint.IsNack() {
			if _, exist := complaintsByID[complaint.PrivateShare.ProposerID]; !exist {
				complaintsByID[complaint.PrivateShare.ProposerID] =
					make(map[types.NodeID]struct{})
			}
			complaintsByID[complaint.PrivateShare.ProposerID][complaint.ProposerID] =
				struct{}{}
		} else {
			disqualifyIDs[complaint.PrivateShare.ProposerID] = struct{}{}
		}
	}
	for nID, complaints := range complaintsByID {
		if len(complaints) >= threshold {
			disqualifyIDs[nID] = struct{}{}
		}
	}
	qualifyIDs = make(cryptoDKG.IDs, 0, len(mpks)-len(disqualifyIDs))
	if cap(qualifyIDs) < threshold {
		err = ErrNotReachThreshold
		return
	}
	qualifyNodeIDs = make(map[types.NodeID]struct{})
	for _, mpk := range mpks {
		if _, exist := disqualifyIDs[mpk.ProposerID]; exist {
			continue
		}
		qualifyIDs = append(qualifyIDs, mpk.DKGID)
		qualifyNodeIDs[mpk.ProposerID] = struct{}{}
	}
	return
}

// NewGroupPublicKey creats a GroupPublicKey instance.
func NewGroupPublicKey(
	round uint64,
	mpks []*MasterPublicKey, complaints []*Complaint,
	threshold int) (
	*GroupPublicKey, error) {
	qualifyIDs, qualifyNodeIDs, err :=
		CalcQualifyNodes(mpks, complaints, threshold)
	if err != nil {
		return nil, err
	}
	mpkMap := make(map[cryptoDKG.ID]*MasterPublicKey, cap(qualifyIDs))
	idMap := make(map[types.NodeID]cryptoDKG.ID)
	for _, mpk := range mpks {
		if _, exist := qualifyNodeIDs[mpk.ProposerID]; !exist {
			continue
		}
		mpkMap[mpk.DKGID] = mpk
		idMap[mpk.ProposerID] = mpk.DKGID
	}
	// Recover Group Public Key.
	pubShares := make([]*cryptoDKG.PublicKeyShares, 0, len(qualifyIDs))
	for _, id := range qualifyIDs {
		pubShares = append(pubShares, &mpkMap[id].PublicKeyShares)
	}
	groupPK := cryptoDKG.RecoverGroupPublicKey(pubShares)
	return &GroupPublicKey{
		Round:          round,
		QualifyIDs:     qualifyIDs,
		QualifyNodeIDs: qualifyNodeIDs,
		IDMap:          idMap,
		Threshold:      threshold,
		GroupPublicKey: groupPK,
	}, nil
}

// NodePublicKeys is the result of DKG protocol.
type NodePublicKeys struct {
	Round          uint64
	QualifyIDs     cryptoDKG.IDs
	QualifyNodeIDs map[types.NodeID]struct{}
	IDMap          map[types.NodeID]cryptoDKG.ID
	PublicKeys     map[types.NodeID]*cryptoDKG.PublicKey
	Threshold      int
}

// NewNodePublicKeys creats a NodePublicKeys instance.
func NewNodePublicKeys(
	round uint64,
	mpks []*MasterPublicKey, complaints []*Complaint,
	threshold int) (
	*NodePublicKeys, error) {
	qualifyIDs, qualifyNodeIDs, err :=
		CalcQualifyNodes(mpks, complaints, threshold)
	if err != nil {
		return nil, err
	}
	mpkMap := make(map[cryptoDKG.ID]*MasterPublicKey, cap(qualifyIDs))
	idMap := make(map[types.NodeID]cryptoDKG.ID)
	for _, mpk := range mpks {
		if _, exist := qualifyNodeIDs[mpk.ProposerID]; !exist {
			continue
		}
		mpkMap[mpk.DKGID] = mpk
		idMap[mpk.ProposerID] = mpk.DKGID
	}
	// Recover qualify members' public key.
	pubKeys := make(map[types.NodeID]*cryptoDKG.PublicKey, len(qualifyIDs))
	for _, recvID := range qualifyIDs {
		pubShares := cryptoDKG.NewEmptyPublicKeyShares()
		for _, id := range qualifyIDs {
			pubShare, err := mpkMap[id].PublicKeyShares.Share(recvID)
			if err != nil {
				return nil, err
			}
			if err := pubShares.AddShare(id, pubShare); err != nil {
				return nil, err
			}
		}
		pubKey, err := pubShares.RecoverPublicKey(qualifyIDs)
		if err != nil {
			return nil, err
		}
		pubKeys[mpkMap[recvID].ProposerID] = pubKey
	}
	return &NodePublicKeys{
		Round:          round,
		QualifyIDs:     qualifyIDs,
		QualifyNodeIDs: qualifyNodeIDs,
		IDMap:          idMap,
		PublicKeys:     pubKeys,
		Threshold:      threshold,
	}, nil
}
