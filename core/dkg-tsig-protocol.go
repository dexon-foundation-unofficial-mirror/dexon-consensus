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

package core

import (
	"fmt"
	"sync"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors for dkg module.
var (
	ErrNotDKGParticipant = fmt.Errorf(
		"not a DKG participant")
	ErrNotQualifyDKGParticipant = fmt.Errorf(
		"not a qualified DKG participant")
	ErrIDShareNotFound = fmt.Errorf(
		"private share not found for specific ID")
	ErrIncorrectPrivateShareSignature = fmt.Errorf(
		"incorrect private share signature")
	ErrMismatchPartialSignatureHash = fmt.Errorf(
		"mismatch partialSignature hash")
	ErrIncorrectPartialSignatureSignature = fmt.Errorf(
		"incorrect partialSignature signature")
	ErrIncorrectPartialSignature = fmt.Errorf(
		"incorrect partialSignature")
	ErrNotEnoughtPartialSignatures = fmt.Errorf(
		"not enough of partial signatures")
	ErrRoundAlreadyPurged = fmt.Errorf(
		"cache of round already been purged")
	ErrTSigNotReady = fmt.Errorf(
		"tsig not ready")
	ErrSelfMPKNotRegister = fmt.Errorf(
		"self mpk not registered")
	ErrUnableGetSelfPrvShare = fmt.Errorf(
		"unable to get self DKG PrivateShare")
	ErrSelfPrvShareMismatch = fmt.Errorf(
		"self privateShare does not match mpk registered")
)

// ErrUnexpectedDKGResetCount represents receiving a DKG message with unexpected
// DKG reset count.
type ErrUnexpectedDKGResetCount struct {
	expect, actual uint64
	proposerID     types.NodeID
}

func (e ErrUnexpectedDKGResetCount) Error() string {
	return fmt.Sprintf(
		"unexpected DKG reset count, from:%s expect:%d actual:%d",
		e.proposerID.String()[:6], e.expect, e.actual)
}

// ErrUnexpectedRound represents receiving a DKG message with unexpected round.
type ErrUnexpectedRound struct {
	expect, actual uint64
	proposerID     types.NodeID
}

func (e ErrUnexpectedRound) Error() string {
	return fmt.Sprintf("unexpected round, from:%s expect:%d actual:%d",
		e.proposerID.String()[:6], e.expect, e.actual)
}

type dkgReceiver interface {
	// ProposeDKGComplaint proposes a DKGComplaint.
	ProposeDKGComplaint(complaint *typesDKG.Complaint)

	// ProposeDKGMasterPublicKey propose a DKGMasterPublicKey.
	ProposeDKGMasterPublicKey(mpk *typesDKG.MasterPublicKey)

	// ProposeDKGPrivateShare propose a DKGPrivateShare.
	ProposeDKGPrivateShare(prv *typesDKG.PrivateShare)

	// ProposeDKGAntiNackComplaint propose a DKGPrivateShare as an anti complaint.
	ProposeDKGAntiNackComplaint(prv *typesDKG.PrivateShare)

	// ProposeDKGMPKReady propose a DKGMPKReady message.
	ProposeDKGMPKReady(ready *typesDKG.MPKReady)

	// ProposeDKGFinalize propose a DKGFinalize message.
	ProposeDKGFinalize(final *typesDKG.Finalize)
}

type dkgProtocol struct {
	ID                 types.NodeID
	recv               dkgReceiver
	round              uint64
	reset              uint64
	threshold          int
	idMap              map[types.NodeID]dkg.ID
	mpkMap             map[types.NodeID]*dkg.PublicKeyShares
	masterPrivateShare *dkg.PrivateKeyShares
	prvShares          *dkg.PrivateKeyShares
	prvSharesReceived  map[types.NodeID]struct{}
	nodeComplained     map[types.NodeID]struct{}
	// Complaint[from][to]'s anti is saved to antiComplaint[from][to].
	antiComplaintReceived map[types.NodeID]map[types.NodeID]struct{}
	// The completed step in `runDKG`.
	step int
}

func (d *dkgProtocol) convertFromInfo(info db.DKGProtocolInfo) {
	d.ID = info.ID
	d.idMap = info.IDMap
	d.round = info.Round
	d.threshold = int(info.Threshold)
	d.idMap = info.IDMap
	d.mpkMap = info.MpkMap
	d.prvSharesReceived = info.PrvSharesReceived
	d.nodeComplained = info.NodeComplained
	d.antiComplaintReceived = info.AntiComplaintReceived
	d.step = int(info.Step)
	d.reset = info.Reset
	if info.IsMasterPrivateShareEmpty {
		d.masterPrivateShare = nil
	} else {
		d.masterPrivateShare = &info.MasterPrivateShare
	}

	if info.IsPrvSharesEmpty {
		d.prvShares = nil
	} else {
		d.prvShares = &info.PrvShares
	}
}

func (d *dkgProtocol) toDKGProtocolInfo() db.DKGProtocolInfo {
	info := db.DKGProtocolInfo{
		ID:                    d.ID,
		Round:                 d.round,
		Threshold:             uint64(d.threshold),
		IDMap:                 d.idMap,
		MpkMap:                d.mpkMap,
		PrvSharesReceived:     d.prvSharesReceived,
		NodeComplained:        d.nodeComplained,
		AntiComplaintReceived: d.antiComplaintReceived,
		Step:                  uint64(d.step),
		Reset:                 d.reset,
	}

	if d.masterPrivateShare != nil {
		info.MasterPrivateShare = *d.masterPrivateShare
	} else {
		info.IsMasterPrivateShareEmpty = true
	}

	if d.prvShares != nil {
		info.PrvShares = *d.prvShares
	} else {
		info.IsPrvSharesEmpty = true
	}

	return info
}

type dkgShareSecret struct {
	privateKey *dkg.PrivateKey
}

// TSigVerifier is the interface verifying threshold signature.
type TSigVerifier interface {
	VerifySignature(hash common.Hash, sig crypto.Signature) bool
}

// TSigVerifierCacheInterface specifies interface used by TSigVerifierCache.
type TSigVerifierCacheInterface interface {
	// Configuration returns the configuration at a given round.
	// Return the genesis configuration if round == 0.
	Configuration(round uint64) *types.Config

	// DKGComplaints gets all the DKGComplaints of round.
	DKGComplaints(round uint64) []*typesDKG.Complaint

	// DKGMasterPublicKeys gets all the DKGMasterPublicKey of round.
	DKGMasterPublicKeys(round uint64) []*typesDKG.MasterPublicKey

	// IsDKGFinal checks if DKG is final.
	IsDKGFinal(round uint64) bool
}

// TSigVerifierCache is the cache for TSigVerifier.
type TSigVerifierCache struct {
	intf      TSigVerifierCacheInterface
	verifier  map[uint64]TSigVerifier
	minRound  uint64
	cacheSize int
	lock      sync.RWMutex
}

type tsigProtocol struct {
	nodePublicKeys *typesDKG.NodePublicKeys
	hash           common.Hash
	sigs           map[dkg.ID]dkg.PartialSignature
	threshold      int
}

func newDKGProtocol(
	ID types.NodeID,
	recv dkgReceiver,
	round uint64,
	reset uint64,
	threshold int) *dkgProtocol {

	prvShare, pubShare := dkg.NewPrivateKeyShares(threshold)

	recv.ProposeDKGMasterPublicKey(&typesDKG.MasterPublicKey{
		Round:           round,
		Reset:           reset,
		DKGID:           typesDKG.NewID(ID),
		PublicKeyShares: *pubShare.Move(),
	})

	return &dkgProtocol{
		ID:                    ID,
		recv:                  recv,
		round:                 round,
		reset:                 reset,
		threshold:             threshold,
		idMap:                 make(map[types.NodeID]dkg.ID),
		mpkMap:                make(map[types.NodeID]*dkg.PublicKeyShares),
		masterPrivateShare:    prvShare,
		prvShares:             dkg.NewEmptyPrivateKeyShares(),
		prvSharesReceived:     make(map[types.NodeID]struct{}),
		nodeComplained:        make(map[types.NodeID]struct{}),
		antiComplaintReceived: make(map[types.NodeID]map[types.NodeID]struct{}),
	}
}

func recoverDKGProtocol(
	ID types.NodeID,
	recv dkgReceiver,
	round uint64,
	reset uint64,
	coreDB db.Database) (*dkgProtocol, error) {
	dkgProtocolInfo, err := coreDB.GetDKGProtocol()
	if err != nil {
		if err == db.ErrDKGProtocolDoesNotExist {
			return nil, nil
		}
		return nil, err
	}

	dkgProtocol := dkgProtocol{
		recv: recv,
	}
	dkgProtocol.convertFromInfo(dkgProtocolInfo)

	if dkgProtocol.ID != ID || dkgProtocol.round != round || dkgProtocol.reset != reset {
		return nil, nil
	}

	return &dkgProtocol, nil
}

func (d *dkgProtocol) processMasterPublicKeys(
	mpks []*typesDKG.MasterPublicKey) (err error) {
	d.idMap = make(map[types.NodeID]dkg.ID, len(mpks))
	d.mpkMap = make(map[types.NodeID]*dkg.PublicKeyShares, len(mpks))
	d.prvSharesReceived = make(map[types.NodeID]struct{}, len(mpks))
	ids := make(dkg.IDs, len(mpks))
	for i := range mpks {
		if mpks[i].Reset != d.reset {
			return ErrUnexpectedDKGResetCount{
				expect:     d.reset,
				actual:     mpks[i].Reset,
				proposerID: mpks[i].ProposerID,
			}
		}
		nID := mpks[i].ProposerID
		d.idMap[nID] = mpks[i].DKGID
		d.mpkMap[nID] = &mpks[i].PublicKeyShares
		ids[i] = mpks[i].DKGID
	}
	d.masterPrivateShare.SetParticipants(ids)
	if err = d.verifySelfPrvShare(); err != nil {
		return
	}
	for _, mpk := range mpks {
		share, ok := d.masterPrivateShare.Share(mpk.DKGID)
		if !ok {
			err = ErrIDShareNotFound
			continue
		}
		d.recv.ProposeDKGPrivateShare(&typesDKG.PrivateShare{
			ReceiverID:   mpk.ProposerID,
			Round:        d.round,
			Reset:        d.reset,
			PrivateShare: *share,
		})
	}
	return
}

func (d *dkgProtocol) verifySelfPrvShare() error {
	selfMPK, exist := d.mpkMap[d.ID]
	if !exist {
		return ErrSelfMPKNotRegister
	}
	share, ok := d.masterPrivateShare.Share(d.idMap[d.ID])
	if !ok {
		return ErrUnableGetSelfPrvShare
	}
	ok, err := selfMPK.VerifyPrvShare(
		d.idMap[d.ID], share)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSelfPrvShareMismatch
	}
	return nil
}

func (d *dkgProtocol) proposeNackComplaints() {
	for nID := range d.mpkMap {
		if _, exist := d.prvSharesReceived[nID]; exist {
			continue
		}
		d.recv.ProposeDKGComplaint(&typesDKG.Complaint{
			Round: d.round,
			Reset: d.reset,
			PrivateShare: typesDKG.PrivateShare{
				ProposerID: nID,
				Round:      d.round,
				Reset:      d.reset,
			},
		})
	}
}

func (d *dkgProtocol) processNackComplaints(complaints []*typesDKG.Complaint) (
	err error) {
	if err = d.verifySelfPrvShare(); err != nil {
		return
	}
	for _, complaint := range complaints {
		if !complaint.IsNack() {
			continue
		}
		if complaint.Reset != d.reset {
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
		d.recv.ProposeDKGAntiNackComplaint(&typesDKG.PrivateShare{
			ProposerID:   d.ID,
			ReceiverID:   complaint.ProposerID,
			Round:        d.round,
			Reset:        d.reset,
			PrivateShare: *share,
		})
	}
	return
}

func (d *dkgProtocol) enforceNackComplaints(complaints []*typesDKG.Complaint) {
	for _, complaint := range complaints {
		if !complaint.IsNack() {
			continue
		}
		if complaint.Reset != d.reset {
			continue
		}
		to := complaint.PrivateShare.ProposerID
		// Do not propose nack complaint to itself.
		if to == d.ID {
			continue
		}
		from := complaint.ProposerID
		// Nack complaint is already proposed.
		if from == d.ID {
			continue
		}
		if _, exist :=
			d.antiComplaintReceived[from][to]; !exist {
			d.recv.ProposeDKGComplaint(&typesDKG.Complaint{
				Round: d.round,
				Reset: d.reset,
				PrivateShare: typesDKG.PrivateShare{
					ProposerID: to,
					Round:      d.round,
					Reset:      d.reset,
				},
			})
		}
	}
}

func (d *dkgProtocol) sanityCheck(prvShare *typesDKG.PrivateShare) error {
	if d.round != prvShare.Round {
		return ErrUnexpectedRound{
			expect:     d.round,
			actual:     prvShare.Round,
			proposerID: prvShare.ProposerID,
		}
	}
	if d.reset != prvShare.Reset {
		return ErrUnexpectedDKGResetCount{
			expect:     d.reset,
			actual:     prvShare.Reset,
			proposerID: prvShare.ProposerID,
		}
	}
	if _, exist := d.idMap[prvShare.ProposerID]; !exist {
		return ErrNotDKGParticipant
	}
	ok, err := utils.VerifyDKGPrivateShareSignature(prvShare)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectPrivateShareSignature
	}
	return nil
}

func (d *dkgProtocol) processPrivateShare(
	prvShare *typesDKG.PrivateShare) error {
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
		complaint := &typesDKG.Complaint{
			Round:        d.round,
			Reset:        d.reset,
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
		}
		if _, exist :=
			d.antiComplaintReceived[prvShare.ReceiverID][prvShare.ProposerID]; !exist {
			d.recv.ProposeDKGAntiNackComplaint(prvShare)
			d.antiComplaintReceived[prvShare.ReceiverID][prvShare.ProposerID] =
				struct{}{}
		}
	}
	return nil
}

func (d *dkgProtocol) proposeMPKReady() {
	d.recv.ProposeDKGMPKReady(&typesDKG.MPKReady{
		ProposerID: d.ID,
		Round:      d.round,
		Reset:      d.reset,
	})
}

func (d *dkgProtocol) proposeFinalize() {
	d.recv.ProposeDKGFinalize(&typesDKG.Finalize{
		ProposerID: d.ID,
		Round:      d.round,
		Reset:      d.reset,
	})
}

func (d *dkgProtocol) recoverShareSecret(qualifyIDs dkg.IDs) (
	*dkgShareSecret, error) {
	if len(qualifyIDs) < d.threshold {
		return nil, typesDKG.ErrNotReachThreshold
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

// NewTSigVerifierCache creats a TSigVerifierCache instance.
func NewTSigVerifierCache(
	intf TSigVerifierCacheInterface, cacheSize int) *TSigVerifierCache {
	return &TSigVerifierCache{
		intf:      intf,
		verifier:  make(map[uint64]TSigVerifier),
		cacheSize: cacheSize,
	}
}

// UpdateAndGet calls Update and then Get.
func (tc *TSigVerifierCache) UpdateAndGet(round uint64) (
	TSigVerifier, bool, error) {
	ok, err := tc.Update(round)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	v, ok := tc.Get(round)
	return v, ok, nil
}

// Purge the cache.
func (tc *TSigVerifierCache) Purge(round uint64) {
	tc.lock.Lock()
	defer tc.lock.Unlock()
	delete(tc.verifier, round)
}

// Update the cache and returns if success.
func (tc *TSigVerifierCache) Update(round uint64) (bool, error) {
	tc.lock.Lock()
	defer tc.lock.Unlock()
	if round < tc.minRound {
		return false, ErrRoundAlreadyPurged
	}
	if _, exist := tc.verifier[round]; exist {
		return true, nil
	}
	if !tc.intf.IsDKGFinal(round) {
		return false, nil
	}
	gpk, err := typesDKG.NewGroupPublicKey(round,
		tc.intf.DKGMasterPublicKeys(round),
		tc.intf.DKGComplaints(round),
		utils.GetDKGThreshold(utils.GetConfigWithPanic(tc.intf, round, nil)))
	if err != nil {
		return false, err
	}
	if len(tc.verifier) == 0 {
		tc.minRound = round
	}
	tc.verifier[round] = gpk
	if len(tc.verifier) > tc.cacheSize {
		delete(tc.verifier, tc.minRound)
	}
	for {
		if _, exist := tc.verifier[tc.minRound]; !exist {
			tc.minRound++
		} else {
			break
		}
	}
	return true, nil
}

// Delete the cache of given round.
func (tc *TSigVerifierCache) Delete(round uint64) {
	tc.lock.Lock()
	defer tc.lock.Unlock()
	delete(tc.verifier, round)
}

// Get the TSigVerifier of round and returns if it exists.
func (tc *TSigVerifierCache) Get(round uint64) (TSigVerifier, bool) {
	tc.lock.RLock()
	defer tc.lock.RUnlock()
	verifier, exist := tc.verifier[round]
	return verifier, exist
}

func newTSigProtocol(
	npks *typesDKG.NodePublicKeys,
	hash common.Hash) *tsigProtocol {
	return &tsigProtocol{
		nodePublicKeys: npks,
		hash:           hash,
		sigs:           make(map[dkg.ID]dkg.PartialSignature, npks.Threshold+1),
	}
}

func (tsig *tsigProtocol) sanityCheck(psig *typesDKG.PartialSignature) error {
	_, exist := tsig.nodePublicKeys.PublicKeys[psig.ProposerID]
	if !exist {
		return ErrNotQualifyDKGParticipant
	}
	ok, err := utils.VerifyDKGPartialSignatureSignature(psig)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectPartialSignatureSignature
	}
	if psig.Hash != tsig.hash {
		return ErrMismatchPartialSignatureHash
	}
	return nil
}

func (tsig *tsigProtocol) processPartialSignature(
	psig *typesDKG.PartialSignature) error {
	if psig.Round != tsig.nodePublicKeys.Round {
		return nil
	}
	id, exist := tsig.nodePublicKeys.IDMap[psig.ProposerID]
	if !exist {
		return ErrNotQualifyDKGParticipant
	}
	if err := tsig.sanityCheck(psig); err != nil {
		return err
	}
	pubKey := tsig.nodePublicKeys.PublicKeys[psig.ProposerID]
	if !pubKey.VerifySignature(
		tsig.hash, crypto.Signature(psig.PartialSignature)) {
		return ErrIncorrectPartialSignature
	}
	tsig.sigs[id] = psig.PartialSignature
	return nil
}

func (tsig *tsigProtocol) signature() (crypto.Signature, error) {
	if len(tsig.sigs) < tsig.nodePublicKeys.Threshold {
		return crypto.Signature{}, ErrNotEnoughtPartialSignatures
	}
	ids := make(dkg.IDs, 0, len(tsig.sigs))
	psigs := make([]dkg.PartialSignature, 0, len(tsig.sigs))
	for id, psig := range tsig.sigs {
		ids = append(ids, id)
		psigs = append(psigs, psig)
	}
	return dkg.RecoverSignature(psigs, ids)
}
