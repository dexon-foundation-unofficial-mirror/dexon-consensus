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
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors for configuration chain..
var (
	ErrDKGNotRegistered = fmt.Errorf(
		"not yet registered in DKG protocol")
	ErrTSigAlreadyRunning = fmt.Errorf(
		"tsig is already running")
	ErrDKGNotReady = fmt.Errorf(
		"DKG is not ready")
)

type configurationChain struct {
	ID              types.NodeID
	recv            dkgReceiver
	gov             Governance
	dkg             *dkgProtocol
	logger          common.Logger
	dkgLock         sync.RWMutex
	dkgSigner       map[uint64]*dkgShareSecret
	gpk             map[uint64]*DKGGroupPublicKey
	dkgResult       sync.RWMutex
	tsig            map[common.Hash]*tsigProtocol
	tsigTouched     map[common.Hash]struct{}
	tsigReady       *sync.Cond
	cache           *utils.NodeSetCache
	db              db.Database
	dkgSet          map[types.NodeID]struct{}
	mpkReady        bool
	pendingPrvShare map[types.NodeID]*typesDKG.PrivateShare
	// TODO(jimmy-dexon): add timeout to pending psig.
	pendingPsig map[common.Hash][]*typesDKG.PartialSignature
	prevHash    common.Hash
}

func newConfigurationChain(
	ID types.NodeID,
	recv dkgReceiver,
	gov Governance,
	cache *utils.NodeSetCache,
	dbInst db.Database,
	logger common.Logger) *configurationChain {
	return &configurationChain{
		ID:          ID,
		recv:        recv,
		gov:         gov,
		logger:      logger,
		dkgSigner:   make(map[uint64]*dkgShareSecret),
		gpk:         make(map[uint64]*DKGGroupPublicKey),
		tsig:        make(map[common.Hash]*tsigProtocol),
		tsigTouched: make(map[common.Hash]struct{}),
		tsigReady:   sync.NewCond(&sync.Mutex{}),
		cache:       cache,
		db:          dbInst,
		pendingPsig: make(map[common.Hash][]*typesDKG.PartialSignature),
	}
}

func (cc *configurationChain) registerDKG(round uint64, threshold int) {
	cc.dkgLock.Lock()
	defer cc.dkgLock.Unlock()
	if cc.dkg != nil {
		cc.logger.Error("Previous DKG is not finished")
		// TODO(mission): do we have to retry DKG initiation here?
		return
	}
	dkgSet, err := cc.cache.GetDKGSet(round)
	if err != nil {
		cc.logger.Error("Error getting DKG set from cache", "error", err)
		return
	}
	cc.dkgSet = dkgSet
	cc.pendingPrvShare = make(map[types.NodeID]*typesDKG.PrivateShare)
	cc.mpkReady = false
	cc.dkg = newDKGProtocol(
		cc.ID,
		cc.recv,
		round,
		threshold)
	go func() {
		ticker := newTicker(cc.gov, round, TickerDKG)
		defer ticker.Stop()
		<-ticker.Tick()
		cc.dkgLock.Lock()
		defer cc.dkgLock.Unlock()
		cc.dkg.proposeMPKReady()
	}()
}

func (cc *configurationChain) runDKG(round uint64) error {
	// Check if corresponding DKG signer is ready.
	if _, _, err := cc.getDKGInfo(round); err == nil {
		return nil
	}
	cc.dkgLock.Lock()
	defer cc.dkgLock.Unlock()
	if cc.dkg == nil || cc.dkg.round != round {
		if cc.dkg != nil && cc.dkg.round > round {
			cc.logger.Warn("DKG canceled", "round", round)
			return nil
		}
		return ErrDKGNotRegistered
	}
	cc.logger.Debug("Calling Governance.IsDKGFinal", "round", round)
	if cc.gov.IsDKGFinal(round) {
		cc.logger.Warn("DKG already final", "round", round)
		return nil
	}
	cc.logger.Debug("Calling Governance.IsDKGMPKReady", "round", round)
	for !cc.gov.IsDKGMPKReady(round) {
		cc.logger.Debug("DKG MPKs are not ready yet. Try again later...",
			"nodeID", cc.ID)
		cc.dkgLock.Unlock()
		time.Sleep(500 * time.Millisecond)
		cc.dkgLock.Lock()
	}
	ticker := newTicker(cc.gov, round, TickerDKG)
	defer ticker.Stop()
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Check if this node successfully join the protocol.
	cc.logger.Debug("Calling Governance.DKGMasterPublicKeys", "round", round)
	mpks := cc.gov.DKGMasterPublicKeys(round)
	inProtocol := false
	for _, mpk := range mpks {
		if mpk.ProposerID == cc.ID {
			inProtocol = true
			break
		}
	}
	if !inProtocol {
		cc.logger.Warn("Failed to join DKG protocol", "round", round)
		return nil
	}
	// Phase 2(T = 0): Exchange DKG secret key share.
	cc.dkg.processMasterPublicKeys(mpks)
	cc.mpkReady = true
	for _, prvShare := range cc.pendingPrvShare {
		if err := cc.dkg.processPrivateShare(prvShare); err != nil {
			cc.logger.Error("Failed to process private share",
				"error", err)
		}
	}
	// Phase 3(T = 0~λ): Propose complaint.
	// Propose complaint is done in `processMasterPublicKeys`.
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Phase 4(T = λ): Propose nack complaints.
	cc.dkg.proposeNackComplaints()
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Phase 5(T = 2λ): Propose Anti nack complaint.
	cc.logger.Debug("Calling Governance.DKGComplaints", "round", round)
	complaints := cc.gov.DKGComplaints(round)
	cc.dkg.processNackComplaints(complaints)
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Phase 6(T = 3λ): Rebroadcast anti nack complaint.
	// Rebroadcast is done in `processPrivateShare`.
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Phase 7(T = 4λ): Enforce complaints and nack complaints.
	cc.dkg.enforceNackComplaints(complaints)
	// Enforce complaint is done in `processPrivateShare`.
	// Phase 8(T = 5λ): DKG finalize.
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	cc.dkg.proposeFinalize()
	// Phase 9(T = 6λ): DKG is ready.
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Normally, IsDKGFinal would return true here. Use this for in case of
	// unexpected network fluctuation and ensure the robustness of DKG protocol.
	cc.logger.Debug("Calling Governance.IsDKGFinal", "round", round)
	for !cc.gov.IsDKGFinal(round) {
		cc.logger.Debug("DKG is not ready yet. Try again later...",
			"nodeID", cc.ID)
		time.Sleep(500 * time.Millisecond)
	}
	cc.logger.Debug("Calling Governance.DKGMasterPublicKeys", "round", round)
	cc.logger.Debug("Calling Governance.DKGComplaints", "round", round)
	gpk, err := NewDKGGroupPublicKey(round,
		cc.gov.DKGMasterPublicKeys(round),
		cc.gov.DKGComplaints(round),
		cc.dkg.threshold)
	if err != nil {
		return err
	}
	qualifies := ""
	for nID := range gpk.qualifyNodeIDs {
		qualifies += fmt.Sprintf("%s ", nID.String()[:6])
	}
	cc.logger.Info("Qualify Nodes",
		"nodeID", cc.ID,
		"round", round,
		"count", len(gpk.qualifyIDs),
		"qualifies", qualifies)
	if _, exist := gpk.qualifyNodeIDs[cc.ID]; !exist {
		cc.logger.Warn("Self is not in Qualify Nodes")
		return nil
	}
	signer, err := cc.dkg.recoverShareSecret(gpk.qualifyIDs)
	if err != nil {
		return err
	}
	// Save private shares to DB.
	if err = cc.db.PutDKGPrivateKey(round, *signer.privateKey); err != nil {
		return err
	}
	cc.dkgResult.Lock()
	defer cc.dkgResult.Unlock()
	cc.dkgSigner[round] = signer
	cc.gpk[round] = gpk
	return nil
}

func (cc *configurationChain) isDKGFinal(round uint64) bool {
	if !cc.gov.IsDKGFinal(round) {
		return false
	}
	_, _, err := cc.getDKGInfo(round)
	return err == nil
}

func (cc *configurationChain) getDKGInfo(
	round uint64) (*DKGGroupPublicKey, *dkgShareSecret, error) {
	getFromCache := func() (*DKGGroupPublicKey, *dkgShareSecret) {
		cc.dkgResult.RLock()
		defer cc.dkgResult.RUnlock()
		gpk := cc.gpk[round]
		signer := cc.dkgSigner[round]
		return gpk, signer
	}
	gpk, signer := getFromCache()
	if gpk == nil || signer == nil {
		if err := cc.recoverDKGInfo(round); err != nil {
			return nil, nil, err
		}
		gpk, signer = getFromCache()
	}
	if gpk == nil || signer == nil {
		return nil, nil, ErrDKGNotReady
	}
	return gpk, signer, nil
}

func (cc *configurationChain) recoverDKGInfo(round uint64) error {
	cc.dkgResult.Lock()
	defer cc.dkgResult.Unlock()
	_, signerExists := cc.dkgSigner[round]
	_, gpkExists := cc.gpk[round]
	if signerExists && gpkExists {
		return nil
	}
	if !cc.gov.IsDKGFinal(round) {
		return ErrDKGNotReady
	}

	threshold := getDKGThreshold(
		utils.GetConfigWithPanic(cc.gov, round, cc.logger))
	// Restore group public key.
	gpk, err := NewDKGGroupPublicKey(round,
		cc.gov.DKGMasterPublicKeys(round),
		cc.gov.DKGComplaints(round),
		threshold)
	if err != nil {
		return err
	}
	// Restore DKG share secret, this segment of code is copied from
	// dkgProtocol.recoverShareSecret.
	if len(gpk.qualifyIDs) < threshold {
		return ErrNotReachThreshold
	}
	// Check if we have private shares in DB.
	prvKey, err := cc.db.GetDKGPrivateKey(round)
	if err != nil {
		return err
	}
	cc.gpk[round] = gpk
	cc.dkgSigner[round] = &dkgShareSecret{
		privateKey: &prvKey,
	}
	return nil
}

func (cc *configurationChain) preparePartialSignature(
	round uint64, hash common.Hash) (*typesDKG.PartialSignature, error) {
	_, signer, _ := cc.getDKGInfo(round)
	if signer == nil {
		return nil, ErrDKGNotReady
	}
	return &typesDKG.PartialSignature{
		ProposerID:       cc.ID,
		Round:            round,
		Hash:             hash,
		PartialSignature: signer.sign(hash),
	}, nil
}

func (cc *configurationChain) touchTSigHash(hash common.Hash) (first bool) {
	cc.tsigReady.L.Lock()
	defer cc.tsigReady.L.Unlock()
	_, exist := cc.tsigTouched[hash]
	cc.tsigTouched[hash] = struct{}{}
	return !exist
}

func (cc *configurationChain) untouchTSigHash(hash common.Hash) {
	cc.tsigReady.L.Lock()
	defer cc.tsigReady.L.Unlock()
	delete(cc.tsigTouched, hash)
}

func (cc *configurationChain) runTSig(
	round uint64, hash common.Hash) (
	crypto.Signature, error) {
	gpk, _, _ := cc.getDKGInfo(round)
	if gpk == nil {
		return crypto.Signature{}, ErrDKGNotReady
	}
	cc.tsigReady.L.Lock()
	defer cc.tsigReady.L.Unlock()
	if _, exist := cc.tsig[hash]; exist {
		return crypto.Signature{}, ErrTSigAlreadyRunning
	}
	cc.tsig[hash] = newTSigProtocol(gpk, hash)
	pendingPsig := cc.pendingPsig[hash]
	delete(cc.pendingPsig, hash)
	go func() {
		for _, psig := range pendingPsig {
			if err := cc.processPartialSignature(psig); err != nil {
				cc.logger.Error("failed to process partial signature",
					"nodeID", cc.ID,
					"error", err)
			}
		}
	}()
	timeout := make(chan struct{}, 1)
	go func() {
		// TODO(jimmy-dexon): make timeout configurable.
		time.Sleep(5 * time.Second)
		timeout <- struct{}{}
		cc.tsigReady.Broadcast()
	}()
	var signature crypto.Signature
	var err error
	for func() bool {
		signature, err = cc.tsig[hash].signature()
		select {
		case <-timeout:
			return false
		default:
		}
		return err == ErrNotEnoughtPartialSignatures
	}() {
		cc.tsigReady.Wait()
	}
	delete(cc.tsig, hash)
	if err != nil {
		return crypto.Signature{}, err
	}
	return signature, nil
}

func (cc *configurationChain) runCRSTSig(
	round uint64, crs common.Hash) ([]byte, error) {
	sig, err := cc.runTSig(round, crs)
	cc.logger.Info("CRS",
		"nodeID", cc.ID,
		"round", round+1,
		"signature", sig)
	return sig.Signature[:], err
}

func (cc *configurationChain) processPrivateShare(
	prvShare *typesDKG.PrivateShare) error {
	cc.dkgLock.Lock()
	defer cc.dkgLock.Unlock()
	if cc.dkg == nil {
		return nil
	}
	if _, exist := cc.dkgSet[prvShare.ProposerID]; !exist {
		return ErrNotDKGParticipant
	}
	if !cc.mpkReady {
		// TODO(jimmy-dexon): remove duplicated signature check in dkg module.
		ok, err := utils.VerifyDKGPrivateShareSignature(prvShare)
		if err != nil {
			return err
		}
		if !ok {
			return ErrIncorrectPrivateShareSignature
		}
		cc.pendingPrvShare[prvShare.ProposerID] = prvShare
		return nil
	}
	return cc.dkg.processPrivateShare(prvShare)
}

func (cc *configurationChain) processPartialSignature(
	psig *typesDKG.PartialSignature) error {
	cc.tsigReady.L.Lock()
	defer cc.tsigReady.L.Unlock()
	if _, exist := cc.tsig[psig.Hash]; !exist {
		ok, err := utils.VerifyDKGPartialSignatureSignature(psig)
		if err != nil {
			return err
		}
		if !ok {
			return ErrIncorrectPartialSignatureSignature
		}
		cc.pendingPsig[psig.Hash] = append(cc.pendingPsig[psig.Hash], psig)
		return nil
	}
	if err := cc.tsig[psig.Hash].processPartialSignature(psig); err != nil {
		return err
	}
	cc.tsigReady.Broadcast()
	return nil
}
