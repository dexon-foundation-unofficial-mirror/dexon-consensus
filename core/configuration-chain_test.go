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
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

type ConfigurationChainTestSuite struct {
	suite.Suite

	nIDs    types.NodeIDs
	dkgIDs  map[types.NodeID]dkg.ID
	prvKeys map[types.NodeID]crypto.PrivateKey
}

type testCCReceiver struct {
	s *ConfigurationChainTestSuite

	nodes map[types.NodeID]*configurationChain
	govs  map[types.NodeID]Governance
}

func newTestCCReceiver(
	s *ConfigurationChainTestSuite) *testCCReceiver {
	return &testCCReceiver{
		s:     s,
		nodes: make(map[types.NodeID]*configurationChain),
		govs:  make(map[types.NodeID]Governance),
	}
}

func (r *testCCReceiver) ProposeDKGComplaint(complaint *typesDKG.Complaint) {
	prvKey, exist := r.s.prvKeys[complaint.ProposerID]
	if !exist {
		panic(errors.New("should exist"))
	}
	var err error
	complaint.Signature, err = prvKey.Sign(hashDKGComplaint(complaint))
	if err != nil {
		panic(err)
	}
	for _, gov := range r.govs {
		// Use Marshal/Unmarshal to do deep copy.
		data, err := json.Marshal(complaint)
		if err != nil {
			panic(err)
		}
		complaintCopy := &typesDKG.Complaint{}
		if err := json.Unmarshal(data, complaintCopy); err != nil {
			panic(err)
		}
		gov.AddDKGComplaint(complaintCopy.Round, complaintCopy)
	}
}

func (r *testCCReceiver) ProposeDKGMasterPublicKey(
	mpk *typesDKG.MasterPublicKey) {
	prvKey, exist := r.s.prvKeys[mpk.ProposerID]
	if !exist {
		panic(errors.New("should exist"))
	}
	var err error
	mpk.Signature, err = prvKey.Sign(hashDKGMasterPublicKey(mpk))
	if err != nil {
		panic(err)
	}
	for _, gov := range r.govs {
		// Use Marshal/Unmarshal to do deep copy.
		data, err := json.Marshal(mpk)
		if err != nil {
			panic(err)
		}
		mpkCopy := typesDKG.NewMasterPublicKey()
		if err := json.Unmarshal(data, mpkCopy); err != nil {
			panic(err)
		}
		gov.AddDKGMasterPublicKey(mpkCopy.Round, mpkCopy)
	}
}

func (r *testCCReceiver) ProposeDKGPrivateShare(
	prv *typesDKG.PrivateShare) {
	go func() {
		prvKey, exist := r.s.prvKeys[prv.ProposerID]
		if !exist {
			panic(errors.New("should exist"))
		}
		var err error
		prv.Signature, err = prvKey.Sign(hashDKGPrivateShare(prv))
		if err != nil {
			panic(err)
		}
		receiver, exist := r.nodes[prv.ReceiverID]
		if !exist {
			panic(errors.New("should exist"))
		}
		err = receiver.processPrivateShare(prv)
		if err != nil {
			panic(err)
		}
	}()
}

func (r *testCCReceiver) ProposeDKGAntiNackComplaint(
	prv *typesDKG.PrivateShare) {
	go func() {
		prvKey, exist := r.s.prvKeys[prv.ProposerID]
		if !exist {
			panic(errors.New("should exist"))
		}
		var err error
		prv.Signature, err = prvKey.Sign(hashDKGPrivateShare(prv))
		if err != nil {
			panic(err)
		}
		for _, cc := range r.nodes {
			// Use Marshal/Unmarshal to do deep copy.
			data, err := json.Marshal(prv)
			if err != nil {
				panic(err)
			}
			prvCopy := &typesDKG.PrivateShare{}
			if err := json.Unmarshal(data, prvCopy); err != nil {
				panic(err)
			}
			err = cc.processPrivateShare(prvCopy)
			if err != nil {
				panic(err)
			}
		}
	}()
}

func (r *testCCReceiver) ProposeDKGFinalize(final *typesDKG.Finalize) {
	prvKey, exist := r.s.prvKeys[final.ProposerID]
	if !exist {
		panic(errors.New("should exist"))
	}
	var err error
	final.Signature, err = prvKey.Sign(hashDKGFinalize(final))
	if err != nil {
		panic(err)
	}
	for _, gov := range r.govs {
		// Use Marshal/Unmarshal to do deep copy.
		data, err := json.Marshal(final)
		if err != nil {
			panic(err)
		}
		finalCopy := &typesDKG.Finalize{}
		if err := json.Unmarshal(data, finalCopy); err != nil {
			panic(err)
		}
		gov.AddDKGFinalize(finalCopy.Round, finalCopy)
	}
}

func (s *ConfigurationChainTestSuite) setupNodes(n int) {
	s.nIDs = make(types.NodeIDs, 0, n)
	s.prvKeys = make(map[types.NodeID]crypto.PrivateKey, n)
	s.dkgIDs = make(map[types.NodeID]dkg.ID)
	ids := make(dkg.IDs, 0, n)
	for i := 0; i < n; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		nID := types.NewNodeID(prvKey.PublicKey())
		s.nIDs = append(s.nIDs, nID)
		s.prvKeys[nID] = prvKey
		id := dkg.NewID(nID.Hash[:])
		ids = append(ids, id)
		s.dkgIDs[nID] = id
	}
}

func (s *ConfigurationChainTestSuite) runDKG(
	k, n int, round uint64) map[types.NodeID]*configurationChain {
	s.setupNodes(n)

	cfgChains := make(map[types.NodeID]*configurationChain)
	recv := newTestCCReceiver(s)

	pks := make([]crypto.PublicKey, 0, len(s.prvKeys))
	for _, prv := range s.prvKeys {
		pks = append(pks, prv.PublicKey())
	}

	for _, nID := range s.nIDs {
		gov, err := test.NewGovernance(test.NewState(
			pks, 100*time.Millisecond, &common.NullLogger{}, true), ConfigRoundShift)
		s.Require().NoError(err)
		cache := utils.NewNodeSetCache(gov)
		cfgChains[nID] = newConfigurationChain(
			nID, recv, gov, cache, &common.NullLogger{})
		recv.nodes[nID] = cfgChains[nID]
		recv.govs[nID] = gov
	}

	for _, cc := range cfgChains {
		cc.registerDKG(round, k)
	}

	for _, gov := range recv.govs {
		s.Require().Len(gov.DKGMasterPublicKeys(round), n)
	}

	errs := make(chan error, n)
	wg := sync.WaitGroup{}
	wg.Add(n)
	for _, cc := range cfgChains {
		go func(cc *configurationChain) {
			defer wg.Done()
			errs <- cc.runDKG(round)
		}(cc)
	}
	wg.Wait()
	for range cfgChains {
		s.Require().NoError(<-errs)
	}
	return cfgChains
}

func (s *ConfigurationChainTestSuite) preparePartialSignature(
	hash common.Hash,
	round uint64,
	cfgChains map[types.NodeID]*configurationChain) (
	psigs []*typesDKG.PartialSignature) {
	psigs = make([]*typesDKG.PartialSignature, 0, len(cfgChains))
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round]; !exist {
			continue
		}
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			continue
		}
		psig, err := cc.preparePartialSignature(round, hash)
		s.Require().NoError(err)
		prvKey, exist := s.prvKeys[cc.ID]
		s.Require().True(exist)
		psig.Signature, err = prvKey.Sign(hashDKGPartialSignature(psig))
		s.Require().NoError(err)
		psigs = append(psigs, psig)
	}
	return
}

// TestConfigurationChain will test the entire DKG+TISG protocol including
// exchanging private shares, recovering share secret, creating partial sign and
// recovering threshold signature.
// All participants are good people in this test.
func (s *ConfigurationChainTestSuite) TestConfigurationChain() {
	k := 4
	n := 10
	round := uint64(0)
	cfgChains := s.runDKG(k, n, round)

	hash := crypto.Keccak256Hash([]byte("🌚🌝"))
	psigs := s.preparePartialSignature(hash, round, cfgChains)

	tsigs := make([]crypto.Signature, 0, n)
	errs := make(chan error, n)
	tsigChan := make(chan crypto.Signature, n)
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round]; !exist {
			continue
		}
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			continue
		}
		go func(cc *configurationChain) {
			tsig, err := cc.runTSig(round, hash)
			// Prevent racing by collecting errors and check in main thread.
			errs <- err
			tsigChan <- tsig
		}(cc)
		for _, psig := range psigs {
			err := cc.processPartialSignature(psig)
			s.Require().NoError(err)
		}
	}
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round]; !exist {
			s.FailNow("Should be qualifyied")
		}
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			s.FailNow("Should be qualifyied")
		}
		s.Require().NoError(<-errs)
		tsig := <-tsigChan
		for _, prevTsig := range tsigs {
			s.Equal(prevTsig, tsig)
		}
	}
}

func (s *ConfigurationChainTestSuite) TestDKGComplaintDelayAdd() {
	k := 4
	n := 10
	round := uint64(0)
	lambdaDKG := 1000 * time.Millisecond
	s.setupNodes(n)

	cfgChains := make(map[types.NodeID]*configurationChain)
	recv := newTestCCReceiver(s)

	pks := make([]crypto.PublicKey, 0, len(s.prvKeys))
	for _, prv := range s.prvKeys {
		pks = append(pks, prv.PublicKey())
	}

	for _, nID := range s.nIDs {
		state := test.NewState(
			pks, 100*time.Millisecond, &common.NullLogger{}, true)
		gov, err := test.NewGovernance(state, ConfigRoundShift)
		s.Require().NoError(err)
		s.Require().NoError(state.RequestChange(
			test.StateChangeLambdaDKG, lambdaDKG))
		cache := utils.NewNodeSetCache(gov)
		cfgChains[nID] = newConfigurationChain(
			nID, recv, gov, cache, &common.NullLogger{})
		recv.nodes[nID] = cfgChains[nID]
		recv.govs[nID] = gov
	}

	for _, cc := range cfgChains {
		cc.registerDKG(round, k)
	}

	for _, gov := range recv.govs {
		s.Require().Len(gov.DKGMasterPublicKeys(round), n)
	}

	errs := make(chan error, n)
	wg := sync.WaitGroup{}
	wg.Add(n)
	for _, cc := range cfgChains {
		go func(cc *configurationChain) {
			defer wg.Done()
			errs <- cc.runDKG(round)
		}(cc)
	}
	go func() {
		// Node 0 proposes NackComplaint to all others at 3λ but they should
		// be ignored because NackComplaint shoould be proposed before 2λ.
		time.Sleep(lambdaDKG * 3)
		nID := s.nIDs[0]
		for _, targetNode := range s.nIDs {
			if targetNode == nID {
				continue
			}
			recv.ProposeDKGComplaint(&typesDKG.Complaint{
				ProposerID: nID,
				Round:      round,
				PrivateShare: typesDKG.PrivateShare{
					ProposerID: targetNode,
					Round:      round,
				},
			})
		}
	}()
	wg.Wait()
	for range cfgChains {
		s.Require().NoError(<-errs)
	}
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round]; !exist {
			s.FailNow("Should be qualifyied")
		}
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			s.FailNow("Should be qualifyied")
		}
	}
}

func (s *ConfigurationChainTestSuite) TestMultipleTSig() {
	k := 2
	n := 7
	round := uint64(0)
	cfgChains := s.runDKG(k, n, round)

	hash1 := crypto.Keccak256Hash([]byte("Hash1"))
	hash2 := crypto.Keccak256Hash([]byte("Hash2"))

	psigs1 := s.preparePartialSignature(hash1, round, cfgChains)
	psigs2 := s.preparePartialSignature(hash2, round, cfgChains)

	tsigs1 := make([]crypto.Signature, 0, n)
	tsigs2 := make([]crypto.Signature, 0, n)

	errs := make(chan error, n*2)
	tsigChan1 := make(chan crypto.Signature, n)
	tsigChan2 := make(chan crypto.Signature, n)
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			continue
		}
		go func(cc *configurationChain) {
			tsig1, err := cc.runTSig(round, hash1)
			// Prevent racing by collecting errors and check in main thread.
			errs <- err
			tsigChan1 <- tsig1
		}(cc)
		go func(cc *configurationChain) {
			tsig2, err := cc.runTSig(round, hash2)
			// Prevent racing by collecting errors and check in main thread.
			errs <- err
			tsigChan2 <- tsig2
		}(cc)
		for _, psig := range psigs1 {
			err := cc.processPartialSignature(psig)
			s.Require().NoError(err)
		}
		for _, psig := range psigs2 {
			err := cc.processPartialSignature(psig)
			s.Require().NoError(err)
		}
	}
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			continue
		}
		s.Require().NoError(<-errs)
		tsig1 := <-tsigChan1
		for _, prevTsig := range tsigs1 {
			s.Equal(prevTsig, tsig1)
		}
		s.Require().NoError(<-errs)
		tsig2 := <-tsigChan2
		for _, prevTsig := range tsigs2 {
			s.Equal(prevTsig, tsig2)
		}
	}
}

func (s *ConfigurationChainTestSuite) TestTSigTimeout() {
	k := 2
	n := 7
	round := uint64(0)
	cfgChains := s.runDKG(k, n, round)
	timeout := 6 * time.Second

	hash := crypto.Keccak256Hash([]byte("🍯🍋"))

	psigs := s.preparePartialSignature(hash, round, cfgChains)

	errs := make(chan error, n)
	qualify := 0
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			continue
		}
		qualify++
		go func(cc *configurationChain) {
			_, err := cc.runTSig(round, hash)
			// Prevent racing by collecting errors and check in main thread.
			errs <- err
		}(cc)
		// Only 1 partial signature is provided.
		err := cc.processPartialSignature(psigs[0])
		s.Require().NoError(err)
	}
	time.Sleep(timeout)
	s.Require().Len(errs, qualify)
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			continue
		}
		s.Equal(<-errs, ErrNotEnoughtPartialSignatures)
	}
}

func TestConfigurationChain(t *testing.T) {
	suite.Run(t, new(ConfigurationChainTestSuite))
}
