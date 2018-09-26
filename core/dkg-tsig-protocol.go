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

package core

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/dkg"
)

// Errors for dkg module.
var (
	ErrNotDKGParticipant = fmt.Errorf(
		"not a DKG participant")
	ErrNotQualifyDKGParticipant = fmt.Errorf(
		"not a qualified DKG participant")
	ErrIDShareNotFound = fmt.Errorf(
		"private share not found for specific ID")
	ErrNotReachThreshold = fmt.Errorf(
		"threshold not reach")
	ErrIncorrectPrivateShareSignature = fmt.Errorf(
		"incorrect private share signature")
	ErrMismatchPartialSignatureType = fmt.Errorf(
		"mismatch partialSignature type")
	ErrIncorrectPartialSignatureSignature = fmt.Errorf(
		"incorrect partialSignature signature")
	ErrIncorrectPartialSignature = fmt.Errorf(
		"incorrect partialSignature")
	ErrNotEnoughtPartialSignatures = fmt.Errorf(
		"not enough of partial signatures")
)

type dkgReceiver interface {
	// ProposeDKGComplaint proposes a DKGComplaint.
	ProposeDKGComplaint(complaint *types.DKGComplaint)

	// ProposeDKGMasterPublicKey propose a DKGMasterPublicKey.
	ProposeDKGMasterPublicKey(mpk *types.DKGMasterPublicKey)

	// ProposeDKGPrivateShare propose a DKGPrivateShare.
	ProposeDKGPrivateShare(prv *types.DKGPrivateShare)

	// ProposeDKGAntiNackComplaint propose a DKGPrivateShare as an anti complaint.
	ProposeDKGAntiNackComplaint(prv *types.DKGPrivateShare)
}

type dkgProtocol struct {
	ID                 types.NodeID
	recv               dkgReceiver
	round              uint64
	threshold          int
	sigToPub           SigToPubFn
	idMap              map[types.NodeID]dkg.ID
	mpkMap             map[types.NodeID]*dkg.PublicKeyShares
	masterPrivateShare *dkg.PrivateKeyShares
	prvShares          *dkg.PrivateKeyShares
	prvSharesReceived  map[types.NodeID]struct{}
	nodeComplained     map[types.NodeID]struct{}
	// Complaint[from][to]'s anti is saved to antiComplaint[from][to].
	antiComplaintReceived map[types.NodeID]map[types.NodeID]struct{}
}

type dkgShareSecret struct {
	privateKey *dkg.PrivateKey
}

type dkgGroupPublicKey struct {
	round          uint64
	qualifyIDs     dkg.IDs
	qualifyNodeIDs types.NodeIDs
	idMap          map[types.NodeID]dkg.ID
	publicKeys     map[types.NodeID]*dkg.PublicKey
	groupPublicKey *dkg.PublicKey
	threshold      int
	sigToPub       SigToPubFn
}

type tsigProtocol struct {
	groupPublicKey *dkgGroupPublicKey
	hash           common.Hash
	psigType       types.DKGPartialSignatureType
	sigs           map[dkg.ID]dkg.PartialSignature
	threshold      int
}

func newDKGID(ID types.NodeID) dkg.ID {
	return dkg.NewID(ID.Hash[:])
}

func newDKGProtocol(
	ID types.NodeID,
	recv dkgReceiver,
	round uint64,
	threshold int,
	sigToPub SigToPubFn) *dkgProtocol {

	prvShare, pubShare := dkg.NewPrivateKeyShares(threshold)

	recv.ProposeDKGMasterPublicKey(&types.DKGMasterPublicKey{
		ProposerID:      ID,
		Round:           round,
		DKGID:           newDKGID(ID),
		PublicKeyShares: *pubShare,
	})

	return &dkgProtocol{
		ID:                    ID,
		recv:                  recv,
		round:                 round,
		threshold:             threshold,
		sigToPub:              sigToPub,
		idMap:                 make(map[types.NodeID]dkg.ID),
		mpkMap:                make(map[types.NodeID]*dkg.PublicKeyShares),
		masterPrivateShare:    prvShare,
		prvShares:             dkg.NewEmptyPrivateKeyShares(),
		prvSharesReceived:     make(map[types.NodeID]struct{}),
		nodeComplained:        make(map[types.NodeID]struct{}),
		antiComplaintReceived: make(map[types.NodeID]map[types.NodeID]struct{}),
	}
}

func (d *dkgProtocol) processMasterPublicKeys(
	mpks []*types.DKGMasterPublicKey) error {
	d.idMap = make(map[types.NodeID]dkg.ID, len(mpks))
	d.mpkMap = make(map[types.NodeID]*dkg.PublicKeyShares, len(mpks))
	d.prvSharesReceived = make(map[types.NodeID]struct{}, len(mpks))
	ids := make(dkg.IDs, len(mpks))
	for i := range mpks {
		nID := mpks[i].ProposerID
		d.idMap[nID] = mpks[i].DKGID
		d.mpkMap[nID] = &mpks[i].PublicKeyShares
		ids[i] = mpks[i].DKGID
	}
	d.masterPrivateShare.SetParticipants(ids)
	for _, mpk := range mpks {
		share, ok := d.masterPrivateShare.Share(mpk.DKGID)
		if !ok {
			return ErrIDShareNotFound
		}
		d.recv.ProposeDKGPrivateShare(&types.DKGPrivateShare{
			ProposerID:   d.ID,
			ReceiverID:   mpk.ProposerID,
			Round:        d.round,
			PrivateShare: *share,
		})
	}
	return nil
}

func (d *dkgProtocol) proposeNackComplaints() {
	for nID := range d.mpkMap {
		if _, exist := d.prvSharesReceived[nID]; exist {
			continue
		}
		d.recv.ProposeDKGComplaint(&types.DKGComplaint{
			ProposerID: d.ID,
			Round:      d.round,
			PrivateShare: types.DKGPrivateShare{
				ProposerID: nID,
				Round:      d.round,
			},
		})
	}
}

func (d *dkgProtocol) processNackComplaints(complaints []*types.DKGComplaint) (
	err error) {
	for _, complaint := range complaints {
		if !complaint.IsNack() {
			continue
		}
		if complaint.PrivateShare.ProposerID != d.ID {
			continue
		}
		id, exist := d.idMap[complaint.ProposerID]
		if !exist {
			err = ErrNotDKGParticipant
			continue
		}
		share, ok := d.masterPrivateShare.Share(id)
		if !ok {
			err = ErrIDShareNotFound
			continue
		}
		d.recv.ProposeDKGAntiNackComplaint(&types.DKGPrivateShare{
			ProposerID:   d.ID,
			ReceiverID:   complaint.ProposerID,
			Round:        d.round,
			PrivateShare: *share,
		})
	}
	return
}

func (d *dkgProtocol) enforceNackComplaints(complaints []*types.DKGComplaint) {
	for _, complaint := range complaints {
		if !complaint.IsNack() {
			continue
		}
		from := complaint.ProposerID
		to := complaint.PrivateShare.ProposerID
		if _, exist :=
			d.antiComplaintReceived[from][to]; !exist {
			d.recv.ProposeDKGComplaint(&types.DKGComplaint{
				ProposerID: d.ID,
				Round:      d.round,
				PrivateShare: types.DKGPrivateShare{
					ProposerID: to,
					Round:      d.round,
				},
			})
		}
	}
}

func (d *dkgProtocol) sanityCheck(prvShare *types.DKGPrivateShare) error {
	if _, exist := d.idMap[prvShare.ProposerID]; !exist {
		return ErrNotDKGParticipant
	}
	ok, err := verifyDKGPrivateShareSignature(prvShare, d.sigToPub)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectPrivateShareSignature
	}
	return nil
}

func (d *dkgProtocol) processPrivateShare(
	prvShare *types.DKGPrivateShare) error {
	if d.round != prvShare.Round {
		return nil
	}
	receiverID, exist := d.idMap[prvShare.ReceiverID]
	// This node is not a DKG participant, ignore the private share.
	if !exist {
		return nil
	}
	if err := d.sanityCheck(prvShare); err != nil {
		return err
	}
	mpk := d.mpkMap[prvShare.ProposerID]
	ok, err := mpk.VerifyPrvShare(receiverID, &prvShare.PrivateShare)
	if err != nil {
		return err
	}
	if prvShare.ReceiverID == d.ID {
		d.prvSharesReceived[prvShare.ProposerID] = struct{}{}
	}
	if !ok {
		if _, exist := d.nodeComplained[prvShare.ProposerID]; exist {
			return nil
		}
		complaint := &types.DKGComplaint{
			ProposerID:   d.ID,
			Round:        d.round,
			PrivateShare: *prvShare,
		}
		d.nodeComplained[prvShare.ProposerID] = struct{}{}
		d.recv.ProposeDKGComplaint(complaint)
	} else if prvShare.ReceiverID == d.ID {
		sender := d.idMap[prvShare.ProposerID]
		if err := d.prvShares.AddShare(sender, &prvShare.PrivateShare); err != nil {
			return err
		}
	} else {
		// The prvShare is an anti complaint.
		if _, exist := d.antiComplaintReceived[prvShare.ReceiverID]; !exist {
			d.antiComplaintReceived[prvShare.ReceiverID] =
				make(map[types.NodeID]struct{})
			d.recv.ProposeDKGAntiNackComplaint(prvShare)
		}
		d.antiComplaintReceived[prvShare.ReceiverID][prvShare.ProposerID] =
			struct{}{}
	}
	return nil
}

func (d *dkgProtocol) recoverShareSecret(qualifyIDs dkg.IDs) (
	*dkgShareSecret, error) {
	if len(qualifyIDs) <= d.threshold {
		return nil, ErrNotReachThreshold
	}
	prvKey, err := d.prvShares.RecoverPrivateKey(qualifyIDs)
	if err != nil {
		return nil, err
	}
	return &dkgShareSecret{
		privateKey: prvKey,
	}, nil
}

func (ss *dkgShareSecret) sign(hash common.Hash) dkg.PartialSignature {
	// DKG sign will always success.
	sig, _ := ss.privateKey.Sign(hash)
	return dkg.PartialSignature(sig)
}

func newDKGGroupPublicKey(
	round uint64,
	mpks []*types.DKGMasterPublicKey, complaints []*types.DKGComplaint,
	threshold int, sigToPub SigToPubFn) (
	*dkgGroupPublicKey, error) {
	// Calculate qualify members.
	disqualifyIDs := map[types.NodeID]struct{}{}
	complaintsByID := map[types.NodeID]int{}
	for _, complaint := range complaints {
		if complaint.IsNack() {
			complaintsByID[complaint.PrivateShare.ProposerID]++
		} else {
			disqualifyIDs[complaint.PrivateShare.ProposerID] = struct{}{}
		}
	}
	for nID, num := range complaintsByID {
		if num > threshold {
			disqualifyIDs[nID] = struct{}{}
		}
	}
	qualifyIDs := make(dkg.IDs, 0, len(mpks)-len(disqualifyIDs))
	qualifyNodeIDs := make(types.NodeIDs, 0, cap(qualifyIDs))
	mpkMap := make(map[dkg.ID]*types.DKGMasterPublicKey, cap(qualifyIDs))
	idMap := make(map[types.NodeID]dkg.ID)
	for _, mpk := range mpks {
		if _, exist := disqualifyIDs[mpk.ProposerID]; exist {
			continue
		}
		mpkMap[mpk.DKGID] = mpk
		idMap[mpk.ProposerID] = mpk.DKGID
		qualifyIDs = append(qualifyIDs, mpk.DKGID)
		qualifyNodeIDs = append(qualifyNodeIDs, mpk.ProposerID)
	}
	// Recover qualify members' public key.
	pubKeys := make(map[types.NodeID]*dkg.PublicKey, len(qualifyIDs))
	for _, recvID := range qualifyIDs {
		pubShares := dkg.NewEmptyPublicKeyShares()
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
	// Recover Group Public Key.
	pubShares := make([]*dkg.PublicKeyShares, 0, len(qualifyIDs))
	for _, id := range qualifyIDs {
		pubShares = append(pubShares, &mpkMap[id].PublicKeyShares)
	}
	groupPK := dkg.RecoverGroupPublicKey(pubShares)
	return &dkgGroupPublicKey{
		round:          round,
		qualifyIDs:     qualifyIDs,
		qualifyNodeIDs: qualifyNodeIDs,
		idMap:          idMap,
		publicKeys:     pubKeys,
		threshold:      threshold,
		groupPublicKey: groupPK,
		sigToPub:       sigToPub,
	}, nil
}

func (gpk *dkgGroupPublicKey) verifySignature(
	hash common.Hash, sig crypto.Signature) bool {
	return gpk.groupPublicKey.VerifySignature(hash, sig)
}

func newTSigProtocol(
	gpk *dkgGroupPublicKey,
	hash common.Hash,
	psigType types.DKGPartialSignatureType) *tsigProtocol {
	return &tsigProtocol{
		groupPublicKey: gpk,
		hash:           hash,
		psigType:       psigType,
		sigs:           make(map[dkg.ID]dkg.PartialSignature, gpk.threshold+1),
	}
}

func (tsig *tsigProtocol) sanityCheck(psig *types.DKGPartialSignature) error {
	_, exist := tsig.groupPublicKey.publicKeys[psig.ProposerID]
	if !exist {
		return ErrNotQualifyDKGParticipant
	}
	ok, err := verifyDKGPartialSignatureSignature(
		psig, tsig.groupPublicKey.sigToPub)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectPartialSignatureSignature
	}
	if psig.Type != tsig.psigType {
		return ErrMismatchPartialSignatureType
	}
	return nil
}

func (tsig *tsigProtocol) processPartialSignature(
	psig *types.DKGPartialSignature) error {
	if psig.Round != tsig.groupPublicKey.round {
		return nil
	}
	id, exist := tsig.groupPublicKey.idMap[psig.ProposerID]
	if !exist {
		return ErrNotQualifyDKGParticipant
	}
	if err := tsig.sanityCheck(psig); err != nil {
		return err
	}
	pubKey := tsig.groupPublicKey.publicKeys[psig.ProposerID]
	if !pubKey.VerifySignature(
		tsig.hash, crypto.Signature(psig.PartialSignature)) {
		return ErrIncorrectPartialSignature
	}
	tsig.sigs[id] = psig.PartialSignature
	return nil
}

func (tsig *tsigProtocol) signature() (crypto.Signature, error) {
	if len(tsig.sigs) <= tsig.groupPublicKey.threshold {
		return nil, ErrNotEnoughtPartialSignatures
	}
	ids := make(dkg.IDs, 0, len(tsig.sigs))
	psigs := make([]dkg.PartialSignature, 0, len(tsig.sigs))
	for id, psig := range tsig.sigs {
		ids = append(ids, id)
		psigs = append(psigs, psig)
	}
	return dkg.RecoverSignature(psigs, ids)
}
