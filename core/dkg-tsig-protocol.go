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
	ErrIncorrectPartialSignatureSignature = fmt.Errorf(
		"incorrect partialSignature signature")
	ErrIncorrectPartialSignature = fmt.Errorf(
		"incorrect partialSignature")
	ErrNotEnoughtPartialSignatures = fmt.Errorf(
		"not enough of partial signatures")
)

type dkgComplaintReceiver interface {
	// ProposeDKGComplaint proposes a DKGComplaint.
	ProposeDKGComplaint(complaint *types.DKGComplaint)

	// ProposeDKGMasterPublicKey propose a DKGMasterPublicKey.
	ProposeDKGMasterPublicKey(mpk *types.DKGMasterPublicKey)

	// ProposeDKGPrivateShare propose a DKGPrivateShare.
	ProposeDKGPrivateShare(to types.ValidatorID, prv *types.DKGPrivateShare)
}

type dkgProtocol struct {
	ID                 types.ValidatorID
	recv               dkgComplaintReceiver
	round              uint64
	threshold          int
	sigToPub           SigToPubFn
	idMap              map[types.ValidatorID]dkg.ID
	mpkMap             map[types.ValidatorID]*dkg.PublicKeyShares
	masterPrivateShare *dkg.PrivateKeyShares
	prvShares          *dkg.PrivateKeyShares
}

type dkgShareSecret struct {
	privateKey *dkg.PrivateKey
}

type dkgGroupPublicKey struct {
	round          uint64
	qualifyIDs     dkg.IDs
	idMap          map[types.ValidatorID]dkg.ID
	publicKeys     map[types.ValidatorID]*dkg.PublicKey
	groupPublicKey *dkg.PublicKey
	threshold      int
	sigToPub       SigToPubFn
}

type tsigProtocol struct {
	groupPublicKey *dkgGroupPublicKey
	sigs           map[dkg.ID]dkg.PartialSignature
	threshold      int
}

func newDKGID(ID types.ValidatorID) dkg.ID {
	return dkg.NewID(ID.Hash[:])
}

func newDKGProtocol(
	ID types.ValidatorID,
	recv dkgComplaintReceiver,
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
		ID:                 ID,
		recv:               recv,
		round:              round,
		threshold:          threshold,
		sigToPub:           sigToPub,
		idMap:              make(map[types.ValidatorID]dkg.ID),
		mpkMap:             make(map[types.ValidatorID]*dkg.PublicKeyShares),
		masterPrivateShare: prvShare,
		prvShares:          dkg.NewEmptyPrivateKeyShares(),
	}
}

func (d *dkgProtocol) processMasterPublicKeys(
	mpks []*types.DKGMasterPublicKey) error {
	d.idMap = make(map[types.ValidatorID]dkg.ID, len(mpks))
	d.mpkMap = make(map[types.ValidatorID]*dkg.PublicKeyShares, len(mpks))
	ids := make(dkg.IDs, len(mpks))
	for i := range mpks {
		vID := mpks[i].ProposerID
		d.idMap[vID] = mpks[i].DKGID
		d.mpkMap[vID] = &mpks[i].PublicKeyShares
		ids[i] = mpks[i].DKGID
	}
	d.masterPrivateShare.SetParticipants(ids)
	for _, mpk := range mpks {
		share, ok := d.masterPrivateShare.Share(mpk.DKGID)
		if !ok {
			return ErrIDShareNotFound
		}
		d.recv.ProposeDKGPrivateShare(mpk.ProposerID, &types.DKGPrivateShare{
			ProposerID:   d.ID,
			Round:        d.round,
			PrivateShare: *share,
		})
	}
	return nil
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
	self, exist := d.idMap[d.ID]
	// This validator is not a DKG participant, ignore the private share.
	if !exist {
		return nil
	}
	if err := d.sanityCheck(prvShare); err != nil {
		return err
	}
	mpk := d.mpkMap[prvShare.ProposerID]
	ok, err := mpk.VerifyPrvShare(self, &prvShare.PrivateShare)
	if err != nil {
		return err
	}
	if !ok {
		complaint := &types.DKGComplaint{
			ProposerID:   d.ID,
			Round:        d.round,
			PrivateShare: *prvShare,
		}
		d.recv.ProposeDKGComplaint(complaint)
	} else {
		sender := d.idMap[prvShare.ProposerID]
		if err := d.prvShares.AddShare(sender, &prvShare.PrivateShare); err != nil {
			return err
		}
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
	complaintsByID := map[types.ValidatorID]int{}
	for _, complaint := range complaints {
		complaintsByID[complaint.PrivateShare.ProposerID]++
	}
	disqualifyIDs := map[types.ValidatorID]struct{}{}
	for vID, num := range complaintsByID {
		if num > threshold {
			disqualifyIDs[vID] = struct{}{}
		}
	}
	qualifyIDs := make(dkg.IDs, 0, len(mpks)-len(disqualifyIDs))
	mpkMap := make(map[dkg.ID]*types.DKGMasterPublicKey, cap(qualifyIDs))
	idMap := make(map[types.ValidatorID]dkg.ID)
	for _, mpk := range mpks {
		if _, exist := disqualifyIDs[mpk.ProposerID]; exist {
			continue
		}
		mpkMap[mpk.DKGID] = mpk
		idMap[mpk.ProposerID] = mpk.DKGID
		qualifyIDs = append(qualifyIDs, mpk.DKGID)
	}
	// Recover qualify members' public key.
	pubKeys := make(map[types.ValidatorID]*dkg.PublicKey, len(qualifyIDs))
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

func newTSigProtocol(gpk *dkgGroupPublicKey) *tsigProtocol {
	return &tsigProtocol{
		groupPublicKey: gpk,
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
	return nil
}

func (tsig *tsigProtocol) processPartialSignature(
	hash common.Hash, psig *types.DKGPartialSignature) error {
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
	if !pubKey.VerifySignature(hash, crypto.Signature(psig.PartialSignature)) {
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
