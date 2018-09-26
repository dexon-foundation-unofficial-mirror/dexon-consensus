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
	"log"
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Errors for configuration chain..
var (
	ErrDKGNotRegistered = fmt.Errorf(
		"not yet registered in DKG protocol")
	ErrDKGNotReady = fmt.Errorf(
		"DKG is not ready")
)

type configurationChain struct {
	ID        types.NodeID
	recv      dkgReceiver
	gov       Governance
	dkg       *dkgProtocol
	dkgLock   sync.RWMutex
	dkgSigner map[uint64]*dkgShareSecret
	gpk       map[uint64]*dkgGroupPublicKey
	dkgResult sync.RWMutex
	tsig      *tsigProtocol
	tsigReady *sync.Cond
	// TODO(jimmy-dexon): add timeout to pending psig.
	pendingPsig []*types.DKGPartialSignature
	prevHash    common.Hash
}

func newConfigurationChain(
	ID types.NodeID,
	recv dkgReceiver,
	gov Governance) *configurationChain {
	return &configurationChain{
		ID:        ID,
		recv:      recv,
		gov:       gov,
		dkgSigner: make(map[uint64]*dkgShareSecret),
		gpk:       make(map[uint64]*dkgGroupPublicKey),
		tsigReady: sync.NewCond(&sync.Mutex{}),
	}
}

func (cc *configurationChain) registerDKG(round uint64, threshold int) {
	cc.dkg = newDKGProtocol(
		cc.ID,
		cc.recv,
		round,
		threshold)
}

func (cc *configurationChain) runDKG(round uint64) error {
	cc.dkgLock.Lock()
	defer cc.dkgLock.Unlock()
	if cc.dkg == nil || cc.dkg.round != round {
		return ErrDKGNotRegistered
	}
	if func() bool {
		cc.dkgResult.RLock()
		defer cc.dkgResult.RUnlock()
		_, exist := cc.gpk[round]
		return exist
	}() {
		return nil
	}

	ticker := newTicker(cc.gov, TickerDKG)
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Phase 2(T = 0): Exchange DKG secret key share.
	cc.dkg.processMasterPublicKeys(cc.gov.DKGMasterPublicKeys(round))
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
	cc.dkg.processNackComplaints(cc.gov.DKGComplaints(round))
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Phase 6(T = 3λ): Rebroadcast anti nack complaint.
	// Rebroadcast is done in `processPrivateShare`.
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	// Phase 7(T = 4λ): Enforce complaints and nack complaints.
	cc.dkg.enforceNackComplaints(cc.gov.DKGComplaints(round))
	// Enforce complaint is done in `processPrivateShare`.
	// Phase 8(T = 5λ): DKG is ready.
	cc.dkgLock.Unlock()
	<-ticker.Tick()
	cc.dkgLock.Lock()
	gpk, err := newDKGGroupPublicKey(round,
		cc.gov.DKGMasterPublicKeys(round),
		cc.gov.DKGComplaints(round),
		cc.dkg.threshold)
	if err != nil {
		return err
	}
	signer, err := cc.dkg.recoverShareSecret(gpk.qualifyIDs)
	if err != nil {
		return err
	}
	qualifies := ""
	for _, nID := range gpk.qualifyNodeIDs {
		qualifies += fmt.Sprintf("%s ", nID.String()[:6])
	}
	log.Printf("[%s] Qualify Nodes(%d): %s\n",
		cc.ID, len(gpk.qualifyIDs), qualifies)
	cc.dkgResult.Lock()
	defer cc.dkgResult.Unlock()
	cc.dkgSigner[round] = signer
	cc.gpk[round] = gpk
	return nil
}

func (cc *configurationChain) preparePartialSignature(
	round uint64, hash common.Hash) (*types.DKGPartialSignature, error) {
	signer, exist := func() (*dkgShareSecret, bool) {
		cc.dkgResult.RLock()
		defer cc.dkgResult.RUnlock()
		signer, exist := cc.dkgSigner[round]
		return signer, exist
	}()
	if !exist {
		return nil, ErrDKGNotReady
	}
	return &types.DKGPartialSignature{
		ProposerID:       cc.ID,
		Round:            round,
		PartialSignature: signer.sign(hash),
	}, nil
}

func (cc *configurationChain) runBlockTSig(
	round uint64, hash common.Hash) (crypto.Signature, error) {
	gpk, exist := func() (*dkgGroupPublicKey, bool) {
		cc.dkgResult.RLock()
		defer cc.dkgResult.RUnlock()
		gpk, exist := cc.gpk[round]
		return gpk, exist
	}()
	if !exist {
		return crypto.Signature{}, ErrDKGNotReady
	}
	cc.tsigReady.L.Lock()
	defer cc.tsigReady.L.Unlock()
	cc.tsig = newTSigProtocol(gpk, hash, types.TSigConfigurationBlock)
	pendingPsig := cc.pendingPsig
	cc.pendingPsig = []*types.DKGPartialSignature{}
	go func() {
		for _, psig := range pendingPsig {
			if err := cc.processPartialSignature(psig); err != nil {
				log.Printf("[%s] %s", cc.ID, err)
			}
		}
	}()
	var signature crypto.Signature
	var err error
	for func() bool {
		signature, err = cc.tsig.signature()
		return err == ErrNotEnoughtPartialSignatures
	}() {
		cc.tsigReady.Wait()
	}
	cc.tsig = nil
	if err != nil {
		return crypto.Signature{}, err
	}
	log.Printf("[%s] TSIG: %s\n", cc.ID, signature)
	return signature, nil
}

func (cc *configurationChain) processPrivateShare(
	prvShare *types.DKGPrivateShare) error {
	cc.dkgLock.Lock()
	defer cc.dkgLock.Unlock()
	if cc.dkg == nil {
		return nil
	}
	return cc.dkg.processPrivateShare(prvShare)
}

func (cc *configurationChain) processPartialSignature(
	psig *types.DKGPartialSignature) error {
	cc.tsigReady.L.Lock()
	defer cc.tsigReady.L.Unlock()
	if cc.tsig == nil {
		ok, err := verifyDKGPartialSignatureSignature(psig)
		if err != nil {
			return err
		}
		if !ok {
			return ErrIncorrectPartialSignatureSignature
		}
		cc.pendingPsig = append(cc.pendingPsig, psig)
		return nil
	}
	if err := cc.tsig.processPartialSignature(psig); err != nil {
		return err
	}
	cc.tsigReady.Broadcast()
	return nil
}
