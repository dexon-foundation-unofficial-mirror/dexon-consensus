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
	"context"
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
	ErrSkipButNoError = fmt.Errorf(
		"skip but no error")
	ErrDKGAborted = fmt.Errorf(
		"DKG is aborted")
)

// ErrMismatchDKG represent an attempt to run DKG protocol is failed because
// the register DKG protocol is mismatched, interms of round and resetCount.
type ErrMismatchDKG struct {
	expectRound, expectReset uint64
	actualRound, actualReset uint64
}

func (e ErrMismatchDKG) Error() string {
	return fmt.Sprintf(
		"mismatch DKG, abort running: expect(%d %d) actual(%d %d)",
		e.expectRound, e.expectReset, e.actualRound, e.actualReset)
}

type dkgStepFn func(round uint64, reset uint64) error

type configurationChain struct {
	ID              types.NodeID
	recv            dkgReceiver
	gov             Governance
	dkg             *dkgProtocol
	dkgRunPhases    []dkgStepFn
	logger          common.Logger
	dkgLock         sync.RWMutex
	dkgSigner       map[uint64]*dkgShareSecret
	npks            map[uint64]*typesDKG.NodePublicKeys
	complaints      []*typesDKG.Complaint
	dkgResult       sync.RWMutex
	tsig            map[common.Hash]*tsigProtocol
	tsigTouched     map[common.Hash]struct{}
	tsigReady       *sync.Cond
	cache           *utils.NodeSetCache
	db              db.Database
	notarySet       map[types.NodeID]struct{}
	mpkReady        bool
	pendingPrvShare map[types.NodeID]*typesDKG.PrivateShare
	// TODO(jimmy-dexon): add timeout to pending psig.
	pendingPsig  map[common.Hash][]*typesDKG.PartialSignature
	prevHash     common.Hash
	dkgCtx       context.Context
	dkgCtxCancel context.CancelFunc
	dkgRunning   bool
}

func newConfigurationChain(
	ID types.NodeID,
	recv dkgReceiver,
	gov Governance,
	cache *utils.NodeSetCache,
	dbInst db.Database,
	logger common.Logger) *configurationChain {
	configurationChain := &configurationChain{
		ID:          ID,
		recv:        recv,
		gov:         gov,
		logger:      logger,
		dkgSigner:   make(map[uint64]*dkgShareSecret),
		npks:        make(map[uint64]*typesDKG.NodePublicKeys),
		tsig:        make(map[common.Hash]*tsigProtocol),
		tsigTouched: make(map[common.Hash]struct{}),
		tsigReady:   sync.NewCond(&sync.Mutex{}),
		cache:       cache,
		db:          dbInst,
		pendingPsig: make(map[common.Hash][]*typesDKG.PartialSignature),
	}
	configurationChain.initDKGPhasesFunc()
	return configurationChain
}

func (cc *configurationChain) abortDKG(
	parentCtx context.Context,
	round, reset uint64) bool {
	cc.dkgLock.Lock()
	defer cc.dkgLock.Unlock()
	if cc.dkg != nil {
		return cc.abortDKGNoLock(parentCtx, round, reset)
	}
	return false
}

func (cc *configurationChain) abortDKGNoLock(
	ctx context.Context,
	round, reset uint64) bool {
	if cc.dkg.round > round ||
		(cc.dkg.round == round && cc.dkg.reset > reset) {
		cc.logger.Error("Newer DKG already is registered",
			"round", round,
			"reset", reset)
		return false
	}
	cc.logger.Error("Previous DKG is not finished",
		"round", round,
		"reset", reset,
		"previous-round", cc.dkg.round,
		"previous-reset", cc.dkg.reset)
	// Abort DKG routine in previous round.
	cc.logger.Error("Aborting DKG in previous round",
		"round", round,
		"previous-round", cc.dkg.round)
	// Notify current running DKG protocol to abort.
	if cc.dkgCtxCancel != nil {
		cc.dkgCtxCancel()
	}
	cc.dkgLock.Unlock()
	// Wait for current running DKG protocol aborting.
	for {
		cc.dkgLock.Lock()
		if cc.dkgRunning == false {
			cc.dkg = nil
			break
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
		cc.dkgLock.Unlock()
	}
	cc.logger.Error("Previous DKG aborted",
		"round", round,
		"reset", reset)
	return cc.dkg == nil
}

func (cc *configurationChain) registerDKG(
	parentCtx context.Context,
	round, reset uint64,
	threshold int) {
	cc.dkgLock.Lock()
	defer cc.dkgLock.Unlock()
	if cc.dkg != nil {
		// Make sure we only proceed when cc.dkg is nil.
		if !cc.abortDKGNoLock(parentCtx, round, reset) {
			return
		}
		select {
		case <-parentCtx.Done():
			return
		default:
		}
		if cc.dkg != nil {
			// This panic would only raise when multiple attampts to register
			// a DKG protocol at the same time.
			panic(ErrMismatchDKG{
				expectRound: round,
				expectReset: reset,
				actualRound: cc.dkg.round,
				actualReset: cc.dkg.reset,
			})
		}
	}
	notarySet, err := cc.cache.GetNotarySet(round)
	if err != nil {
		cc.logger.Error("Error getting notary set from cache", "error", err)
		return
	}
	cc.notarySet = notarySet
	cc.pendingPrvShare = make(map[types.NodeID]*typesDKG.PrivateShare)
	cc.mpkReady = false
	cc.dkg, err = recoverDKGProtocol(cc.ID, cc.recv, round, reset, cc.db)
	cc.dkgCtx, cc.dkgCtxCancel = context.WithCancel(parentCtx)
	if err != nil {
		panic(err)
	}
	if cc.dkg == nil {
		cc.dkg = newDKGProtocol(
			cc.ID,
			cc.recv,
			round,
			reset,
			threshold)

		err = cc.db.PutOrUpdateDKGProtocol(cc.dkg.toDKGProtocolInfo())
		if err != nil {
			cc.logger.Error("Error put or update DKG protocol", "error",
				err)
			return
		}
	}

	go func() {
		ticker := newTicker(cc.gov, round, TickerDKG)
		defer ticker.Stop()
		<-ticker.Tick()
		cc.dkgLock.Lock()
		defer cc.dkgLock.Unlock()
		if cc.dkg != nil && cc.dkg.round == round && cc.dkg.reset == reset {
			cc.dkg.proposeMPKReady()
		}
	}()
}

func (cc *configurationChain) runDKGPhaseOne(round uint64, reset uint64) error {
	if cc.dkg.round < round ||
		(cc.dkg.round == round && cc.dkg.reset < reset) {
		return ErrDKGNotRegistered
	}
	if cc.dkg.round != round || cc.dkg.reset != reset {
		cc.logger.Warn("DKG canceled", "round", round, "reset", reset)
		return ErrSkipButNoError
	}
	cc.logger.Debug("Calling Governance.IsDKGFinal", "round", round)
	if cc.gov.IsDKGFinal(round) {
		cc.logger.Warn("DKG already final", "round", round)
		return ErrSkipButNoError
	}
	cc.logger.Debug("Calling Governance.IsDKGMPKReady", "round", round)
	var err error
	for err == nil && !cc.gov.IsDKGMPKReady(round) {
		cc.dkgLock.Unlock()
		cc.logger.Debug("DKG MPKs are not ready yet. Try again later...",
			"nodeID", cc.ID,
			"round", round)
		select {
		case <-cc.dkgCtx.Done():
			err = ErrDKGAborted
		case <-time.After(500 * time.Millisecond):
		}
		cc.dkgLock.Lock()
	}
	return err
}

func (cc *configurationChain) runDKGPhaseTwoAndThree(
	round uint64, reset uint64) error {
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
		cc.logger.Warn("Failed to join DKG protocol",
			"round", round,
			"reset", reset)
		return ErrSkipButNoError
	}
	// Phase 2(T = 0): Exchange DKG secret key share.
	if err := cc.dkg.processMasterPublicKeys(mpks); err != nil {
		cc.logger.Error("Failed to process master public key",
			"round", round,
			"reset", reset,
			"error", err)
	}
	cc.mpkReady = true
	// The time to process private share might be long, check aborting before
	// get into that loop.
	select {
	case <-cc.dkgCtx.Done():
		return ErrDKGAborted
	default:
	}
	for _, prvShare := range cc.pendingPrvShare {
		if err := cc.dkg.processPrivateShare(prvShare); err != nil {
			cc.logger.Error("Failed to process private share",
				"round", round,
				"reset", reset,
				"error", err)
		}
	}

	// Phase 3(T = 0~λ): Propose complaint.
	// Propose complaint is done in `processMasterPublicKeys`.
	return nil
}

func (cc *configurationChain) runDKGPhaseFour() {
	// Phase 4(T = λ): Propose nack complaints.
	cc.dkg.proposeNackComplaints()
}

func (cc *configurationChain) runDKGPhaseFiveAndSix(round uint64, reset uint64) {
	// Phase 5(T = 2λ): Propose Anti nack complaint.
	cc.logger.Debug("Calling Governance.DKGComplaints", "round", round)
	cc.complaints = cc.gov.DKGComplaints(round)
	if err := cc.dkg.processNackComplaints(cc.complaints); err != nil {
		cc.logger.Error("Failed to process NackComplaint",
			"round", round,
			"reset", reset,
			"error", err)
	}

	// Phase 6(T = 3λ): Rebroadcast anti nack complaint.
	// Rebroadcast is done in `processPrivateShare`.
}

func (cc *configurationChain) runDKGPhaseSeven() {
	// Phase 7(T = 4λ): Enforce complaints and nack complaints.
	cc.dkg.enforceNackComplaints(cc.complaints)
	// Enforce complaint is done in `processPrivateShare`.
}

func (cc *configurationChain) runDKGPhaseEight() {
	// Phase 8(T = 5λ): DKG finalize.
	cc.dkg.proposeFinalize()
}

func (cc *configurationChain) runDKGPhaseNine(round uint64, reset uint64) error {
	// Phase 9(T = 6λ): DKG is ready.
	// Normally, IsDKGFinal would return true here. Use this for in case of
	// unexpected network fluctuation and ensure the robustness of DKG protocol.
	cc.logger.Debug("Calling Governance.IsDKGFinal", "round", round)
	var err error
	for err == nil && !cc.gov.IsDKGFinal(round) {
		cc.dkgLock.Unlock()
		cc.logger.Debug("DKG is not ready yet. Try again later...",
			"nodeID", cc.ID.String()[:6],
			"round", round,
			"reset", reset)
		select {
		case <-cc.dkgCtx.Done():
			err = ErrDKGAborted
		case <-time.After(500 * time.Millisecond):
		}
		cc.dkgLock.Lock()
	}
	if err != nil {
		return err
	}
	cc.logger.Debug("Calling Governance.DKGMasterPublicKeys", "round", round)
	cc.logger.Debug("Calling Governance.DKGComplaints", "round", round)
	npks, err := typesDKG.NewNodePublicKeys(round,
		cc.gov.DKGMasterPublicKeys(round),
		cc.gov.DKGComplaints(round),
		cc.dkg.threshold)
	if err != nil {
		return err
	}
	qualifies := ""
	for nID := range npks.QualifyNodeIDs {
		qualifies += fmt.Sprintf("%s ", nID.String()[:6])
	}
	cc.logger.Info("Qualify Nodes",
		"nodeID", cc.ID,
		"round", round,
		"reset", reset,
		"count", len(npks.QualifyIDs),
		"qualifies", qualifies)
	if _, exist := npks.QualifyNodeIDs[cc.ID]; !exist {
		cc.logger.Warn("Self is not in Qualify Nodes",
			"round", round,
			"reset", reset)
		return nil
	}
	signer, err := cc.dkg.recoverShareSecret(npks.QualifyIDs)
	if err != nil {
		return err
	}
	// Save private shares to DB.
	if err =
		cc.db.PutDKGPrivateKey(round, reset, *signer.privateKey); err != nil {
		return err
	}
	cc.dkg.proposeSuccess()
	cc.dkgResult.Lock()
	defer cc.dkgResult.Unlock()
	cc.dkgSigner[round] = signer
	cc.npks[round] = npks
	return nil
}

func (cc *configurationChain) initDKGPhasesFunc() {
	cc.dkgRunPhases = []dkgStepFn{
		func(round uint64, reset uint64) error {
			return cc.runDKGPhaseOne(round, reset)
		},
		func(round uint64, reset uint64) error {
			return cc.runDKGPhaseTwoAndThree(round, reset)
		},
		func(round uint64, reset uint64) error {
			cc.runDKGPhaseFour()
			return nil
		},
		func(round uint64, reset uint64) error {
			cc.runDKGPhaseFiveAndSix(round, reset)
			return nil
		},
		func(round uint64, reset uint64) error {
			cc.runDKGPhaseSeven()
			return nil
		},
		func(round uint64, reset uint64) error {
			cc.runDKGPhaseEight()
			return nil
		},
		func(round uint64, reset uint64) error {
			return cc.runDKGPhaseNine(round, reset)
		},
	}
}

func (cc *configurationChain) runDKG(
	round uint64, reset uint64, event *common.Event,
	dkgBeginHeight, dkgHeight uint64) (err error) {
	// Check if corresponding DKG signer is ready.
	if _, _, err = cc.getDKGInfo(round, false); err == nil {
		return ErrSkipButNoError
	}
	cfg := utils.GetConfigWithPanic(cc.gov, round, cc.logger)
	phaseHeight := uint64(
		cfg.LambdaDKG.Nanoseconds() / cfg.MinBlockInterval.Nanoseconds())
	skipPhase := int(dkgHeight / phaseHeight)
	cc.logger.Info("Skipping DKG phase", "phase", skipPhase)
	cc.dkgLock.Lock()
	defer cc.dkgLock.Unlock()
	if cc.dkg == nil {
		return ErrDKGNotRegistered
	}
	// Make sure the existed dkgProtocol is expected one.
	if cc.dkg.round != round || cc.dkg.reset != reset {
		return ErrMismatchDKG{
			expectRound: round,
			expectReset: reset,
			actualRound: cc.dkg.round,
			actualReset: cc.dkg.reset,
		}
	}
	if cc.dkgRunning {
		panic(fmt.Errorf("duplicated call to runDKG: %d %d", round, reset))
	}
	cc.dkgRunning = true
	defer func() {
		// Here we should hold the cc.dkgLock, reset cc.dkg to nil when done.
		if cc.dkg != nil {
			cc.dkg = nil
		}
		cc.dkgRunning = false
	}()
	wg := sync.WaitGroup{}
	var dkgError error
	// Make a copy of cc.dkgCtx so each phase function can refer to the correct
	// context.
	ctx := cc.dkgCtx
	cc.dkg.step = skipPhase
	for i := skipPhase; i < len(cc.dkgRunPhases); i++ {
		wg.Add(1)
		event.RegisterHeight(dkgBeginHeight+phaseHeight*uint64(i), func(uint64) {
			go func() {
				defer wg.Done()
				cc.dkgLock.Lock()
				defer cc.dkgLock.Unlock()
				if dkgError != nil {
					return
				}
				select {
				case <-ctx.Done():
					dkgError = ErrDKGAborted
					return
				default:
				}

				err := cc.dkgRunPhases[cc.dkg.step](round, reset)
				if err == nil || err == ErrSkipButNoError {
					err = nil
					cc.dkg.step++
					err = cc.db.PutOrUpdateDKGProtocol(cc.dkg.toDKGProtocolInfo())
					if err != nil {
						cc.logger.Error("Failed to save DKG Protocol",
							"step", cc.dkg.step,
							"error", err)
					}
				}
				if err != nil && dkgError == nil {
					dkgError = err
				}
			}()
		})
	}
	cc.dkgLock.Unlock()
	wgChan := make(chan struct{}, 1)
	go func() {
		wg.Wait()
		wgChan <- struct{}{}
	}()
	select {
	case <-cc.dkgCtx.Done():
	case <-wgChan:
	}
	cc.dkgLock.Lock()
	select {
	case <-cc.dkgCtx.Done():
		return ErrDKGAborted
	default:
	}
	return dkgError
}

func (cc *configurationChain) isDKGFinal(round uint64) bool {
	if !cc.gov.IsDKGFinal(round) {
		return false
	}
	_, _, err := cc.getDKGInfo(round, false)
	return err == nil
}

func (cc *configurationChain) getDKGInfo(
	round uint64, ignoreSigner bool) (
	*typesDKG.NodePublicKeys, *dkgShareSecret, error) {
	getFromCache := func() (*typesDKG.NodePublicKeys, *dkgShareSecret) {
		cc.dkgResult.RLock()
		defer cc.dkgResult.RUnlock()
		npks := cc.npks[round]
		signer := cc.dkgSigner[round]
		return npks, signer
	}
	npks, signer := getFromCache()
	if npks == nil || (!ignoreSigner && signer == nil) {
		if err := cc.recoverDKGInfo(round, ignoreSigner); err != nil {
			return nil, nil, err
		}
		npks, signer = getFromCache()
	}
	if npks == nil || (!ignoreSigner && signer == nil) {
		return nil, nil, ErrDKGNotReady
	}
	return npks, signer, nil
}

func (cc *configurationChain) recoverDKGInfo(
	round uint64, ignoreSigner bool) error {
	var npksExists, signerExists bool
	func() {
		cc.dkgResult.Lock()
		defer cc.dkgResult.Unlock()
		_, signerExists = cc.dkgSigner[round]
		_, npksExists = cc.npks[round]
	}()
	if signerExists && npksExists {
		return nil
	}
	if !cc.gov.IsDKGFinal(round) {
		return ErrDKGNotReady
	}

	threshold := utils.GetDKGThreshold(
		utils.GetConfigWithPanic(cc.gov, round, cc.logger))
	cc.logger.Debug("Calling Governance.DKGMasterPublicKeys for recoverDKGInfo",
		"round", round)
	mpk := cc.gov.DKGMasterPublicKeys(round)
	cc.logger.Debug("Calling Governance.DKGComplaints for recoverDKGInfo",
		"round", round)
	comps := cc.gov.DKGComplaints(round)
	qualifies, _, err := typesDKG.CalcQualifyNodes(mpk, comps, threshold)
	if err != nil {
		return err
	}
	if len(qualifies) <
		utils.GetDKGValidThreshold(utils.GetConfigWithPanic(
			cc.gov, round, cc.logger)) {
		return typesDKG.ErrNotReachThreshold
	}

	if !npksExists {
		npks, err := typesDKG.NewNodePublicKeys(round,
			cc.gov.DKGMasterPublicKeys(round),
			cc.gov.DKGComplaints(round),
			threshold)
		if err != nil {
			cc.logger.Warn("Failed to create DKGNodePublicKeys",
				"round", round, "error", err)
			return err
		}
		func() {
			cc.dkgResult.Lock()
			defer cc.dkgResult.Unlock()
			cc.npks[round] = npks
		}()
	}
	if !signerExists && !ignoreSigner {
		reset := cc.gov.DKGResetCount(round)
		// Check if we have private shares in DB.
		prvKey, err := cc.db.GetDKGPrivateKey(round, reset)
		if err != nil {
			cc.logger.Warn("Failed to create DKGPrivateKey",
				"round", round, "error", err)
			dkgProtocolInfo, err := cc.db.GetDKGProtocol()
			if err != nil {
				cc.logger.Warn("Unable to recover DKGProtocolInfo",
					"round", round, "error", err)
				return err
			}
			if dkgProtocolInfo.Round != round {
				cc.logger.Warn("DKGProtocolInfo round mismatch",
					"round", round, "infoRound", dkgProtocolInfo.Round)
				return err
			}
			prvKeyRecover, err :=
				dkgProtocolInfo.PrvShares.RecoverPrivateKey(qualifies)
			if err != nil {
				cc.logger.Warn("Failed to recover DKGPrivateKey",
					"round", round, "error", err)
				return err
			}
			if err = cc.db.PutDKGPrivateKey(
				round, reset, *prvKeyRecover); err != nil {
				cc.logger.Warn("Failed to save DKGPrivateKey",
					"round", round, "error", err)
			}
			prvKey = *prvKeyRecover
		}
		func() {
			cc.dkgResult.Lock()
			defer cc.dkgResult.Unlock()
			cc.dkgSigner[round] = &dkgShareSecret{
				privateKey: &prvKey,
			}
		}()
	}
	return nil
}

func (cc *configurationChain) preparePartialSignature(
	round uint64, hash common.Hash) (*typesDKG.PartialSignature, error) {
	_, signer, _ := cc.getDKGInfo(round, false)
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
	round uint64, hash common.Hash, wait time.Duration) (
	crypto.Signature, error) {
	npks, _, _ := cc.getDKGInfo(round, false)
	if npks == nil {
		return crypto.Signature{}, ErrDKGNotReady
	}
	cc.tsigReady.L.Lock()
	defer cc.tsigReady.L.Unlock()
	if _, exist := cc.tsig[hash]; exist {
		return crypto.Signature{}, ErrTSigAlreadyRunning
	}
	cc.tsig[hash] = newTSigProtocol(npks, hash)
	pendingPsig := cc.pendingPsig[hash]
	delete(cc.pendingPsig, hash)
	go func() {
		for _, psig := range pendingPsig {
			if err := cc.processPartialSignature(psig); err != nil {
				cc.logger.Error("Failed to process partial signature",
					"nodeID", cc.ID,
					"error", err)
			}
		}
	}()
	timeout := make(chan struct{}, 1)
	go func() {
		time.Sleep(wait)
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
	sig, err := cc.runTSig(round, crs, cc.gov.Configuration(round).LambdaDKG*5)
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
	if _, exist := cc.notarySet[prvShare.ProposerID]; !exist {
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
