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
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
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

func (r *testCCReceiver) ProposeDKGComplaint(complaint *types.DKGComplaint) {
	prvKey, exist := r.s.prvKeys[complaint.ProposerID]
	r.s.Require().True(exist)
	var err error
	complaint.Signature, err = prvKey.Sign(hashDKGComplaint(complaint))
	r.s.Require().NoError(err)
	for _, gov := range r.govs {
		// Use Marshal/Unmarshal to do deep copy.
		data, err := json.Marshal(complaint)
		r.s.Require().NoError(err)
		complaintCopy := &types.DKGComplaint{}
		r.s.Require().NoError(json.Unmarshal(data, complaintCopy))
		gov.AddDKGComplaint(complaintCopy)
	}
}

func (r *testCCReceiver) ProposeDKGMasterPublicKey(
	mpk *types.DKGMasterPublicKey) {
	prvKey, exist := r.s.prvKeys[mpk.ProposerID]
	r.s.Require().True(exist)
	var err error
	mpk.Signature, err = prvKey.Sign(hashDKGMasterPublicKey(mpk))
	r.s.Require().NoError(err)
	for _, gov := range r.govs {
		// Use Marshal/Unmarshal to do deep copy.
		data, err := json.Marshal(mpk)
		r.s.Require().NoError(err)
		mpkCopy := types.NewDKGMasterPublicKey()
		r.s.Require().NoError(json.Unmarshal(data, mpkCopy))
		gov.AddDKGMasterPublicKey(mpkCopy)
	}
}

func (r *testCCReceiver) ProposeDKGPrivateShare(
	prv *types.DKGPrivateShare) {
	go func() {
		prvKey, exist := r.s.prvKeys[prv.ProposerID]
		r.s.Require().True(exist)
		var err error
		prv.Signature, err = prvKey.Sign(hashDKGPrivateShare(prv))
		r.s.Require().NoError(err)
		receiver, exist := r.nodes[prv.ReceiverID]
		r.s.Require().True(exist)
		err = receiver.processPrivateShare(prv)
		r.s.Require().NoError(err)
	}()
}

func (r *testCCReceiver) ProposeDKGAntiNackComplaint(
	prv *types.DKGPrivateShare) {
	go func() {
		prvKey, exist := r.s.prvKeys[prv.ProposerID]
		r.s.Require().True(exist)
		var err error
		prv.Signature, err = prvKey.Sign(hashDKGPrivateShare(prv))
		r.s.Require().NoError(err)
		for _, cc := range r.nodes {
			// Use Marshal/Unmarshal to do deep copy.
			data, err := json.Marshal(prv)
			r.s.Require().NoError(err)
			prvCopy := &types.DKGPrivateShare{}
			r.s.Require().NoError(json.Unmarshal(data, prvCopy))
			err = cc.processPrivateShare(prvCopy)
			r.s.Require().NoError(err)
		}
	}()
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

// TestConfigurationChain will test the entire DKG+TISG protocol including
// exchanging private shares, recovering share secret, creating partial sign and
// recovering threshold signature.
// All participants are good people in this test.
func (s *ConfigurationChainTestSuite) TestConfigurationChain() {
	k := 3
	n := 10
	round := uint64(1)
	s.setupNodes(n)

	cfgChains := make(map[types.NodeID]*configurationChain)
	recv := newTestCCReceiver(s)

	for _, nID := range s.nIDs {
		gov, err := test.NewGovernance(0, 50*time.Millisecond)
		s.Require().NoError(err)
		cfgChains[nID] = newConfigurationChain(nID, recv, gov)
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

	psigs := make([]*types.DKGPartialSignature, 0, n)
	hash := crypto.Keccak256Hash([]byte("🌚🌝"))
	for nID, cc := range cfgChains {
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

	tsigs := make([]crypto.Signature, 0, n)
	errs = make(chan error, n)
	tsigChan := make(chan crypto.Signature, n)
	for nID, cc := range cfgChains {
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			continue
		}
		go func(cc *configurationChain) {
			tsig, err := cc.runBlockTSig(round, hash)
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
		if _, exist := cc.gpk[round].qualifyNodeIDs[nID]; !exist {
			continue
		}
		s.Require().NoError(<-errs)
		tsig := <-tsigChan
		for _, prevTsig := range tsigs {
			s.Equal(prevTsig, tsig)
		}
	}
}

func TestConfigurationChain(t *testing.T) {
	suite.Run(t, new(ConfigurationChainTestSuite))
}
