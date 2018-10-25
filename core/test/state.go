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

package test

import (
	"errors"
	"math"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus-core/core/types/dkg"
	"github.com/dexon-foundation/dexon/rlp"
)

// StateChangeType is the type of state change request.
type StateChangeType uint8

var (
	// ErrDuplicatedChange means the change request is already applied.
	ErrDuplicatedChange = errors.New("duplicated change")
	// ErrForkedCRS means a different CRS for one round is proposed.
	ErrForkedCRS = errors.New("forked CRS")
	// ErrMissingPreviousCRS means previous CRS not found when
	// proposing a specific round of CRS.
	ErrMissingPreviousCRS = errors.New("missing previous CRS")
	// ErrUnknownStateChangeType means a StateChangeType is not recognized.
	ErrUnknownStateChangeType = errors.New("unknown state change type")
	// ErrProposerIsFinal means a proposer of one complaint is finalized.
	ErrProposerIsFinal = errors.New("proposer is final")
)

// Types of state change.
const (
	StateChangeNothing StateChangeType = iota
	// DKG & CRS
	StateAddCRS
	StateAddDKGComplaint
	StateAddDKGMasterPublicKey
	StateAddDKGFinal
	// Configuration related.
	StateChangeNumChains
	StateChangeLambdaBA
	StateChangeLambdaDKG
	StateChangeRoundInterval
	StateChangeMinBlockInterval
	StateChangeMaxBlockInterval
	StateChangeK
	StateChangePhiRatio
	StateChangeNotarySetSize
	StateChangeDKGSetSize
	// Node set related.
	StateAddNode
)

type crsAdditionRequest struct {
	Round uint64      `json:"round"`
	CRS   common.Hash `json:"crs"`
}

// StateChangeRequest carries information of state change request.
type StateChangeRequest struct {
	Type    StateChangeType `json:"type"`
	Payload interface{}     `json:"payload"`
}

type rawStateChangeRequest struct {
	Type    StateChangeType
	Payload rlp.RawValue
}

// State emulates what the global state in governace contract on a fullnode.
type State struct {
	// Configuration related.
	numChains        uint32
	lambdaBA         time.Duration
	lambdaDKG        time.Duration
	k                int
	phiRatio         float32
	notarySetSize    uint32
	dkgSetSize       uint32
	roundInterval    time.Duration
	minBlockInterval time.Duration
	maxBlockInterval time.Duration
	// Nodes
	nodes map[types.NodeID]crypto.PublicKey
	// DKG & CRS
	dkgComplaints       map[uint64]map[types.NodeID][]*typesDKG.Complaint
	dkgMasterPublicKeys map[uint64]map[types.NodeID]*typesDKG.MasterPublicKey
	dkgFinals           map[uint64]map[types.NodeID]*typesDKG.Finalize
	crs                 []common.Hash
	// Other stuffs
	local bool
	lock  sync.RWMutex
	// ChangeRequest(s) are organized as map, indexed by type of state change.
	// For each time to apply state change, only the last request would be
	// applied.
	pendingChangedConfigs      map[StateChangeType]interface{}
	pendingNodes               [][]byte
	pendingDKGComplaints       []*typesDKG.Complaint
	pendingDKGFinals           []*typesDKG.Finalize
	pendingDKGMasterPublicKeys []*typesDKG.MasterPublicKey
	pendingCRS                 []*crsAdditionRequest
	pendingChangesLock         sync.Mutex
}

// NewState constructs an State instance with genesis information, including:
//  - node set
//  - crs
func NewState(
	nodePubKeys []crypto.PublicKey, lambda time.Duration, local bool) *State {
	nodes := make(map[types.NodeID]crypto.PublicKey)
	for _, key := range nodePubKeys {
		nodes[types.NewNodeID(key)] = key
	}
	genesisCRS := crypto.Keccak256Hash([]byte("__ DEXON"))
	return &State{
		local:                 local,
		numChains:             uint32(len(nodes)),
		lambdaBA:              lambda,
		lambdaDKG:             lambda * 10,
		roundInterval:         lambda * 10000,
		minBlockInterval:      time.Millisecond * 1,
		maxBlockInterval:      lambda * 8,
		crs:                   []common.Hash{genesisCRS},
		nodes:                 nodes,
		phiRatio:              0.667,
		k:                     0,
		notarySetSize:         uint32(len(nodes)),
		dkgSetSize:            uint32(len(nodes)),
		pendingChangedConfigs: make(map[StateChangeType]interface{}),
		dkgFinals: make(
			map[uint64]map[types.NodeID]*typesDKG.Finalize),
		dkgComplaints: make(
			map[uint64]map[types.NodeID][]*typesDKG.Complaint),
		dkgMasterPublicKeys: make(
			map[uint64]map[types.NodeID]*typesDKG.MasterPublicKey),
	}
}

// Snapshot returns configration that could be snapshotted.
func (s *State) Snapshot() (*types.Config, []crypto.PublicKey) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	// Clone a node set.
	nodes := make([]crypto.PublicKey, 0, len(s.nodes))
	for _, key := range s.nodes {
		nodes = append(nodes, key)
	}
	return &types.Config{
		NumChains:        s.numChains,
		LambdaBA:         s.lambdaBA,
		LambdaDKG:        s.lambdaDKG,
		K:                s.k,
		PhiRatio:         s.phiRatio,
		NotarySetSize:    s.notarySetSize,
		DKGSetSize:       s.dkgSetSize,
		RoundInterval:    s.roundInterval,
		MinBlockInterval: s.minBlockInterval,
		MaxBlockInterval: s.maxBlockInterval,
	}, nodes
}

func (s *State) unpackPayload(
	raw *rawStateChangeRequest) (v interface{}, err error) {
	switch raw.Type {
	case StateAddCRS:
		v = &crsAdditionRequest{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateAddDKGComplaint:
		v = &typesDKG.Complaint{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateAddDKGMasterPublicKey:
		v = &typesDKG.MasterPublicKey{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateAddDKGFinal:
		v = &typesDKG.Finalize{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateChangeNumChains:
		var tmp uint32
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeLambdaBA:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeLambdaDKG:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeRoundInterval:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeMinBlockInterval:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeMaxBlockInterval:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeK:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangePhiRatio:
		var tmp uint32
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeNotarySetSize:
		var tmp uint32
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeDKGSetSize:
		var tmp uint32
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateAddNode:
		var tmp []byte
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	default:
		err = ErrUnknownStateChangeType
	}
	if err != nil {
		return
	}
	return
}

// Apply change requests, this function would also
// be called when we extract these request from delivered blocks.
func (s *State) Apply(reqsAsBytes []byte) (err error) {
	// Try to unmarshal this byte stream into []*StateChangeRequest.
	rawReqs := []*rawStateChangeRequest{}
	if err = rlp.DecodeBytes(reqsAsBytes, &rawReqs); err != nil {
		return
	}
	var reqs []*StateChangeRequest
	for _, r := range rawReqs {
		var payload interface{}
		if payload, err = s.unpackPayload(r); err != nil {
			return
		}
		reqs = append(reqs, &StateChangeRequest{
			Type:    r.Type,
			Payload: payload,
		})
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, req := range reqs {
		if err = s.applyRequest(req); err != nil {
			return
		}
	}
	return
}

// PackRequests pack current pending requests as byte slice, which
// could be sent as blocks' payload and unmarshall back to apply.
func (s *State) PackRequests() (b []byte, err error) {
	packed := []*StateChangeRequest{}
	s.pendingChangesLock.Lock()
	defer s.pendingChangesLock.Unlock()
	// Pack simple configuration changes first. There should be no
	// validity problems for those changes.
	for k, v := range s.pendingChangedConfigs {
		packed = append(packed, &StateChangeRequest{
			Type:    k,
			Payload: v,
		})
	}
	s.pendingChangedConfigs = make(map[StateChangeType]interface{})
	// For other changes, we need to check their validity.
	s.lock.RLock()
	defer s.lock.RUnlock()
	for _, bytesOfKey := range s.pendingNodes {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddNode,
			Payload: bytesOfKey,
		})
	}
	for _, comp := range s.pendingDKGComplaints {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddDKGComplaint,
			Payload: comp,
		})
	}
	for _, final := range s.pendingDKGFinals {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddDKGFinal,
			Payload: final,
		})
	}
	for _, masterPubKey := range s.pendingDKGMasterPublicKeys {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddDKGMasterPublicKey,
			Payload: masterPubKey,
		})
	}
	for _, crs := range s.pendingCRS {
		packed = append(packed, &StateChangeRequest{
			Type:    StateAddCRS,
			Payload: crs,
		})
	}
	if b, err = rlp.EncodeToBytes(packed); err != nil {
		return
	}
	return
}

// isValidRequest checks if this request is valid to proceed or not.
func (s *State) isValidRequest(req *StateChangeRequest) (err error) {
	// NOTE: there would be no lock in this helper, callers should be
	//       responsible for acquiring appropriate lock.
	switch req.Type {
	case StateAddDKGComplaint:
		comp := req.Payload.(*typesDKG.Complaint)
		// If we've received DKG final from that proposer, we would ignore
		// its complaint.
		if _, exists := s.dkgFinals[comp.Round][comp.ProposerID]; exists {
			return ErrProposerIsFinal
		}
		// If we've received identical complaint, ignore it.
		compForRound, exists := s.dkgComplaints[comp.Round]
		if !exists {
			break
		}
		comps, exists := compForRound[comp.ProposerID]
		if !exists {
			break
		}
		for _, tmpComp := range comps {
			if tmpComp == comp {
				return ErrDuplicatedChange
			}
		}
	case StateAddCRS:
		crsReq := req.Payload.(*crsAdditionRequest)
		if uint64(len(s.crs)) > crsReq.Round {
			if !s.crs[crsReq.Round].Equal(crsReq.CRS) {
				return ErrForkedCRS
			}
			return ErrDuplicatedChange
		} else if uint64(len(s.crs)) == crsReq.Round {
			return nil
		} else {
			return ErrMissingPreviousCRS
		}
	}
	return nil
}

// applyRequest applies a single StateChangeRequest.
func (s *State) applyRequest(req *StateChangeRequest) error {
	// NOTE: there would be no lock in this helper, callers should be
	//       responsible for acquiring appropriate lock.
	switch req.Type {
	case StateAddNode:
		pubKey, err := ecdsa.NewPublicKeyFromByteSlice(req.Payload.([]byte))
		if err != nil {
			return err
		}
		s.nodes[types.NewNodeID(pubKey)] = pubKey
	case StateAddCRS:
		crsRequest := req.Payload.(*crsAdditionRequest)
		if crsRequest.Round != uint64(len(s.crs)) {
			return ErrDuplicatedChange
		}
		s.crs = append(s.crs, crsRequest.CRS)
	case StateAddDKGComplaint:
		comp := req.Payload.(*typesDKG.Complaint)
		if _, exists := s.dkgComplaints[comp.Round]; !exists {
			s.dkgComplaints[comp.Round] = make(
				map[types.NodeID][]*typesDKG.Complaint)
		}
		s.dkgComplaints[comp.Round][comp.ProposerID] = append(
			s.dkgComplaints[comp.Round][comp.ProposerID], comp)
	case StateAddDKGMasterPublicKey:
		mKey := req.Payload.(*typesDKG.MasterPublicKey)
		if _, exists := s.dkgMasterPublicKeys[mKey.Round]; !exists {
			s.dkgMasterPublicKeys[mKey.Round] = make(
				map[types.NodeID]*typesDKG.MasterPublicKey)
		}
		s.dkgMasterPublicKeys[mKey.Round][mKey.ProposerID] = mKey
	case StateAddDKGFinal:
		final := req.Payload.(*typesDKG.Finalize)
		if _, exists := s.dkgFinals[final.Round]; !exists {
			s.dkgFinals[final.Round] = make(map[types.NodeID]*typesDKG.Finalize)
		}
		s.dkgFinals[final.Round][final.ProposerID] = final
	case StateChangeNumChains:
		s.numChains = req.Payload.(uint32)
	case StateChangeLambdaBA:
		s.lambdaBA = time.Duration(req.Payload.(uint64))
	case StateChangeLambdaDKG:
		s.lambdaDKG = time.Duration(req.Payload.(uint64))
	case StateChangeRoundInterval:
		s.roundInterval = time.Duration(req.Payload.(uint64))
	case StateChangeMinBlockInterval:
		s.minBlockInterval = time.Duration(req.Payload.(uint64))
	case StateChangeMaxBlockInterval:
		s.maxBlockInterval = time.Duration(req.Payload.(uint64))
	case StateChangeK:
		s.k = int(req.Payload.(uint64))
	case StateChangePhiRatio:
		s.phiRatio = math.Float32frombits(req.Payload.(uint32))
	case StateChangeNotarySetSize:
		s.notarySetSize = req.Payload.(uint32)
	case StateChangeDKGSetSize:
		s.dkgSetSize = req.Payload.(uint32)
	default:
		return errors.New("you are definitely kidding me")
	}
	return nil
}

// ProposeCRS propose a new CRS for a specific round.
func (s *State) ProposeCRS(round uint64, crs common.Hash) (err error) {
	err = s.RequestChange(StateAddCRS, &crsAdditionRequest{
		Round: round,
		CRS:   crs,
	})
	return
}

// RequestChange submits a state change request.
func (s *State) RequestChange(
	t StateChangeType, payload interface{}) (err error) {
	// Patch input parameter's type.
	switch t {
	case StateAddNode:
		payload = payload.(crypto.PublicKey).Bytes()
	case StateChangeLambdaBA,
		StateChangeLambdaDKG,
		StateChangeRoundInterval,
		StateChangeMinBlockInterval,
		StateChangeMaxBlockInterval:
		payload = uint64(payload.(time.Duration))
	case StateChangeK:
		payload = uint64(payload.(int))
	case StateChangePhiRatio:
		payload = math.Float32bits(payload.(float32))
	}
	req := &StateChangeRequest{
		Type:    t,
		Payload: payload,
	}
	if s.local {
		err = func() error {
			s.lock.Lock()
			defer s.lock.Unlock()
			if err := s.isValidRequest(req); err != nil {
				return err
			}
			return s.applyRequest(req)
		}()
		return
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if err = s.isValidRequest(req); err != nil {
		return
	}
	s.pendingChangesLock.Lock()
	defer s.pendingChangesLock.Unlock()
	switch t {
	case StateAddNode:
		s.pendingNodes = append(s.pendingNodes, payload.([]byte))
	case StateAddCRS:
		s.pendingCRS = append(s.pendingCRS, payload.(*crsAdditionRequest))
	case StateAddDKGComplaint:
		s.pendingDKGComplaints = append(
			s.pendingDKGComplaints, payload.(*typesDKG.Complaint))
	case StateAddDKGMasterPublicKey:
		s.pendingDKGMasterPublicKeys = append(
			s.pendingDKGMasterPublicKeys, payload.(*typesDKG.MasterPublicKey))
	case StateAddDKGFinal:
		s.pendingDKGFinals = append(
			s.pendingDKGFinals, payload.(*typesDKG.Finalize))
	default:
		s.pendingChangedConfigs[t] = payload
	}
	return
}

// CRS access crs proposed for that round.
func (s *State) CRS(round uint64) common.Hash {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if round >= uint64(len(s.crs)) {
		return common.Hash{}
	}
	return s.crs[round]
}

// DKGComplaints access current received dkg complaints for that round.
// This information won't be snapshot, thus can't be cached in test.Governance.
func (s *State) DKGComplaints(round uint64) []*typesDKG.Complaint {
	s.lock.RLock()
	defer s.lock.RUnlock()
	comps, exists := s.dkgComplaints[round]
	if !exists {
		return nil
	}
	tmpComps := make([]*typesDKG.Complaint, 0, len(comps))
	for _, compProp := range comps {
		for _, comp := range compProp {
			bytes, err := rlp.EncodeToBytes(comp)
			if err != nil {
				panic(err)
			}
			compCopy := &typesDKG.Complaint{}
			if err = rlp.DecodeBytes(bytes, compCopy); err != nil {
				panic(err)
			}
			tmpComps = append(tmpComps, compCopy)
		}
	}
	return tmpComps
}

// DKGMasterPublicKeys access current received dkg master public keys for that
// round. This information won't be snapshot, thus can't be cached in
// test.Governance.
func (s *State) DKGMasterPublicKeys(round uint64) []*typesDKG.MasterPublicKey {
	s.lock.RLock()
	defer s.lock.RUnlock()
	masterPublicKeys, exists := s.dkgMasterPublicKeys[round]
	if !exists {
		return nil
	}
	mpks := make([]*typesDKG.MasterPublicKey, 0, len(masterPublicKeys))
	for _, mpk := range masterPublicKeys {
		// Return a deep copied master public keys.
		b, err := rlp.EncodeToBytes(mpk)
		if err != nil {
			panic(err)
		}
		mpkCopy := typesDKG.NewMasterPublicKey()
		if err = rlp.DecodeBytes(b, mpkCopy); err != nil {
			panic(err)
		}
		mpks = append(mpks, mpkCopy)
	}
	return mpks
}

// IsDKGFinal checks if current received dkg finals exceeds threshold.
// This information won't be snapshot, thus can't be cached in test.Governance.
func (s *State) IsDKGFinal(round uint64, threshold int) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.dkgFinals[round]) > threshold
}
