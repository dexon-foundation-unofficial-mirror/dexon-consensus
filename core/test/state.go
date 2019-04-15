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

package test

import (
	"bytes"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon/rlp"
)

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
	// ErrProposerMPKIsReady means a proposer of one mpk is ready.
	ErrProposerMPKIsReady = errors.New("proposer mpk is ready")
	// ErrProposerIsFinal means a proposer of one complaint is finalized.
	ErrProposerIsFinal = errors.New("proposer is final")
	// ErrStateConfigNotEqual means configuration part of two states is not
	// equal.
	ErrStateConfigNotEqual = errors.New("config not equal")
	// ErrStateLocalFlagNotEqual means local flag of two states is not equal.
	ErrStateLocalFlagNotEqual = errors.New("local flag not equal")
	// ErrStateNodeSetNotEqual means node sets of two states are not equal.
	ErrStateNodeSetNotEqual = errors.New("node set not equal")
	// ErrStateDKGComplaintsNotEqual means DKG complaints for two states are not
	// equal.
	ErrStateDKGComplaintsNotEqual = errors.New("dkg complaints not equal")
	// ErrStateDKGMasterPublicKeysNotEqual means DKG master public keys of two
	// states are not equal.
	ErrStateDKGMasterPublicKeysNotEqual = errors.New(
		"dkg master public keys not equal")
	// ErrStateDKGMPKReadysNotEqual means DKG readys of two states are not
	// equal.
	ErrStateDKGMPKReadysNotEqual = errors.New("dkg readys not equal")
	// ErrStateDKGFinalsNotEqual means DKG finalizations of two states are not
	// equal.
	ErrStateDKGFinalsNotEqual = errors.New("dkg finalizations not equal")
	// ErrStateDKGSuccessesNotEqual means DKG successes of two states are not
	// equal.
	ErrStateDKGSuccessesNotEqual = errors.New("dkg successes not equal")
	// ErrStateCRSsNotEqual means CRSs of two states are not equal.
	ErrStateCRSsNotEqual = errors.New("crs not equal")
	// ErrStateDKGResetCountNotEqual means dkgResetCount of two states are not
	// equal.
	ErrStateDKGResetCountNotEqual = errors.New("dkg reset count not equal")
	// ErrStatePendingChangesNotEqual means pending change requests of two
	// states are not equal.
	ErrStatePendingChangesNotEqual = errors.New("pending changes not equal")
	// ErrChangeWontApply means the state change won't be applied for some
	// reason.
	ErrChangeWontApply = errors.New("change won't apply")
	// ErrNotInRemoteMode means callers attempts to call functions for remote
	// mode when the State instance is still in local mode.
	ErrNotInRemoteMode = errors.New(
		"attempting to use remote functions in local mode")
)

type crsAdditionRequest struct {
	Round uint64      `json:"round"`
	CRS   common.Hash `json:"crs"`
}

// State emulates what the global state in governace contract on a fullnode.
type State struct {
	// Configuration related.
	lambdaBA         time.Duration
	lambdaDKG        time.Duration
	notarySetSize    uint32
	roundInterval    uint64
	minBlockInterval time.Duration
	// Nodes
	nodes map[types.NodeID]crypto.PublicKey
	// DKG & CRS
	dkgComplaints       map[uint64]map[types.NodeID][]*typesDKG.Complaint
	dkgMasterPublicKeys map[uint64]map[types.NodeID]*typesDKG.MasterPublicKey
	dkgReadys           map[uint64]map[types.NodeID]*typesDKG.MPKReady
	dkgFinals           map[uint64]map[types.NodeID]*typesDKG.Finalize
	dkgSuccesses        map[uint64]map[types.NodeID]*typesDKG.Success
	crs                 []common.Hash
	dkgResetCount       map[uint64]uint64
	// Other stuffs
	local           bool
	logger          common.Logger
	lock            sync.RWMutex
	appliedRequests map[common.Hash]struct{}
	// Pending change requests.
	ownRequests    map[common.Hash]*StateChangeRequest
	globalRequests map[common.Hash]*StateChangeRequest
}

// NewState constructs an State instance with genesis information, including:
//  - node set
//  - crs
func NewState(
	dkgDelayRound uint64,
	nodePubKeys []crypto.PublicKey,
	lambda time.Duration,
	logger common.Logger,
	local bool) *State {
	nodes := make(map[types.NodeID]crypto.PublicKey)
	for _, key := range nodePubKeys {
		nodes[types.NewNodeID(key)] = key
	}
	genesisCRS := crypto.Keccak256Hash([]byte("__ DEXON"))
	crs := make([]common.Hash, dkgDelayRound+1)
	for i := range crs {
		crs[i] = genesisCRS
		genesisCRS = crypto.Keccak256Hash(genesisCRS[:])
	}
	return &State{
		local:            local,
		logger:           logger,
		lambdaBA:         lambda,
		lambdaDKG:        lambda * 10,
		roundInterval:    1000,
		minBlockInterval: 4 * lambda,
		crs:              crs,
		nodes:            nodes,
		notarySetSize:    uint32(len(nodes)),
		ownRequests:      make(map[common.Hash]*StateChangeRequest),
		globalRequests:   make(map[common.Hash]*StateChangeRequest),
		dkgReadys: make(
			map[uint64]map[types.NodeID]*typesDKG.MPKReady),
		dkgFinals: make(
			map[uint64]map[types.NodeID]*typesDKG.Finalize),
		dkgSuccesses: make(
			map[uint64]map[types.NodeID]*typesDKG.Success),
		dkgComplaints: make(
			map[uint64]map[types.NodeID][]*typesDKG.Complaint),
		dkgMasterPublicKeys: make(
			map[uint64]map[types.NodeID]*typesDKG.MasterPublicKey),
		dkgResetCount:   make(map[uint64]uint64),
		appliedRequests: make(map[common.Hash]struct{}),
	}
}

// SwitchToRemoteMode turn this State instance into remote mode: all changes
// are pending, and need to be packed/unpacked to apply. Once this state switch
// to remote mode, there would be no way to switch back to local mode.
func (s *State) SwitchToRemoteMode() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.local = false
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
	cfg := &types.Config{
		LambdaBA:         s.lambdaBA,
		LambdaDKG:        s.lambdaDKG,
		NotarySetSize:    s.notarySetSize,
		RoundLength:      s.roundInterval,
		MinBlockInterval: s.minBlockInterval,
	}
	s.logger.Info("Snapshot config", "config", cfg)
	return cfg, nodes
}

// AttachLogger allows to attach custom logger.
func (s *State) AttachLogger(logger common.Logger) {
	s.logger = logger
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
	case StateAddDKGMPKReady:
		v = &typesDKG.MPKReady{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateAddDKGFinal:
		v = &typesDKG.Finalize{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateAddDKGSuccess:
		v = &typesDKG.Success{}
		err = rlp.DecodeBytes(raw.Payload, v)
	case StateResetDKG:
		var tmp common.Hash
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
	case StateChangeRoundLength:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeMinBlockInterval:
		var tmp uint64
		err = rlp.DecodeBytes(raw.Payload, &tmp)
		v = tmp
	case StateChangeNotarySetSize:
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

func (s *State) unpackRequests(
	b []byte) (reqs []*StateChangeRequest, err error) {
	// Try to unmarshal this byte stream into []*StateChangeRequest.
	rawReqs := []*rawStateChangeRequest{}
	if err = rlp.DecodeBytes(b, &rawReqs); err != nil {
		return
	}
	for _, r := range rawReqs {
		var payload interface{}
		if payload, err = s.unpackPayload(r); err != nil {
			return
		}
		reqs = append(reqs, &StateChangeRequest{
			Type:      r.Type,
			Payload:   payload,
			Hash:      r.Hash,
			Timestamp: r.Timestamp,
		})
	}
	return
}

// Equal checks equality between State instance.
func (s *State) Equal(other *State) error {
	// Check configuration part.
	configEqual := s.lambdaBA == other.lambdaBA &&
		s.lambdaDKG == other.lambdaDKG &&
		s.notarySetSize == other.notarySetSize &&
		s.roundInterval == other.roundInterval &&
		s.minBlockInterval == other.minBlockInterval
	if !configEqual {
		return ErrStateConfigNotEqual
	}
	// Check local flag.
	if s.local != other.local {
		return ErrStateLocalFlagNotEqual
	}
	// Check node set.
	if len(s.nodes) != len(other.nodes) {
		return ErrStateNodeSetNotEqual
	}
	for nID, key := range s.nodes {
		otherKey, exists := other.nodes[nID]
		if !exists {
			return ErrStateNodeSetNotEqual
		}
		if bytes.Compare(key.Bytes(), otherKey.Bytes()) != 0 {
			return ErrStateNodeSetNotEqual
		}
	}
	// Check DKG Complaints, here I assume the addition sequence of complaints
	// proposed by one node would be identical on each node (this should be true
	// when state change requests are carried by blocks and executed in order).
	if len(s.dkgComplaints) != len(other.dkgComplaints) {
		return ErrStateDKGComplaintsNotEqual
	}
	for round, compsForRound := range s.dkgComplaints {
		otherCompsForRound, exists := other.dkgComplaints[round]
		if !exists {
			return ErrStateDKGComplaintsNotEqual
		}
		if len(compsForRound) != len(otherCompsForRound) {
			return ErrStateDKGComplaintsNotEqual
		}
		for nID, comps := range compsForRound {
			otherComps, exists := otherCompsForRound[nID]
			if !exists {
				return ErrStateDKGComplaintsNotEqual
			}
			if len(comps) != len(otherComps) {
				return ErrStateDKGComplaintsNotEqual
			}
			for idx, comp := range comps {
				if !comp.Equal(otherComps[idx]) {
					return ErrStateDKGComplaintsNotEqual
				}
			}
		}
	}
	// Check DKG master public keys.
	if len(s.dkgMasterPublicKeys) != len(other.dkgMasterPublicKeys) {
		return ErrStateDKGMasterPublicKeysNotEqual
	}
	for round, mKeysForRound := range s.dkgMasterPublicKeys {
		otherMKeysForRound, exists := other.dkgMasterPublicKeys[round]
		if !exists {
			return ErrStateDKGMasterPublicKeysNotEqual
		}
		if len(mKeysForRound) != len(otherMKeysForRound) {
			return ErrStateDKGMasterPublicKeysNotEqual
		}
		for nID, mKey := range mKeysForRound {
			otherMKey, exists := otherMKeysForRound[nID]
			if !exists {
				return ErrStateDKGMasterPublicKeysNotEqual
			}
			if !mKey.Equal(otherMKey) {
				return ErrStateDKGMasterPublicKeysNotEqual
			}
		}
	}
	// Check DKG readys.
	if len(s.dkgReadys) != len(other.dkgReadys) {
		return ErrStateDKGMPKReadysNotEqual
	}
	for round, readysForRound := range s.dkgReadys {
		otherReadysForRound, exists := other.dkgReadys[round]
		if !exists {
			return ErrStateDKGMPKReadysNotEqual
		}
		if len(readysForRound) != len(otherReadysForRound) {
			return ErrStateDKGMPKReadysNotEqual
		}
		for nID, ready := range readysForRound {
			otherReady, exists := otherReadysForRound[nID]
			if !exists {
				return ErrStateDKGMPKReadysNotEqual
			}
			if !ready.Equal(otherReady) {
				return ErrStateDKGMPKReadysNotEqual
			}
		}
	}
	// Check DKG finals.
	if len(s.dkgFinals) != len(other.dkgFinals) {
		return ErrStateDKGFinalsNotEqual
	}
	for round, finalsForRound := range s.dkgFinals {
		otherFinalsForRound, exists := other.dkgFinals[round]
		if !exists {
			return ErrStateDKGFinalsNotEqual
		}
		if len(finalsForRound) != len(otherFinalsForRound) {
			return ErrStateDKGFinalsNotEqual
		}
		for nID, final := range finalsForRound {
			otherFinal, exists := otherFinalsForRound[nID]
			if !exists {
				return ErrStateDKGFinalsNotEqual
			}
			if !final.Equal(otherFinal) {
				return ErrStateDKGFinalsNotEqual
			}
		}
	}
	// Check DKG successes.
	if len(s.dkgSuccesses) != len(other.dkgSuccesses) {
		return ErrStateDKGSuccessesNotEqual
	}
	for round, successesForRound := range s.dkgSuccesses {
		otherSuccessesForRound, exists := other.dkgSuccesses[round]
		if !exists {
			return ErrStateDKGSuccessesNotEqual
		}
		if len(successesForRound) != len(otherSuccessesForRound) {
			return ErrStateDKGSuccessesNotEqual
		}
		for nID, success := range successesForRound {
			otherSuccesse, exists := otherSuccessesForRound[nID]
			if !exists {
				return ErrStateDKGSuccessesNotEqual
			}
			if !success.Equal(otherSuccesse) {
				return ErrStateDKGSuccessesNotEqual
			}
		}
	}
	// Check CRS part.
	if len(s.crs) != len(other.crs) {
		return ErrStateCRSsNotEqual
	}
	for idx, crs := range s.crs {
		if crs != other.crs[idx] {
			return ErrStateCRSsNotEqual
		}
	}
	// Check dkgResetCount.
	if len(s.dkgResetCount) != len(other.dkgResetCount) {
		return ErrStateDKGResetCountNotEqual
	}
	for idx, count := range s.dkgResetCount {
		if count != other.dkgResetCount[idx] {
			return ErrStateDKGResetCountNotEqual
		}
	}
	// Check pending changes.
	checkPending := func(
		src, target map[common.Hash]*StateChangeRequest) error {
		if len(src) != len(target) {
			return ErrStatePendingChangesNotEqual
		}
		for k, v := range src {
			otherV, exists := target[k]
			if !exists {
				return ErrStatePendingChangesNotEqual
			}
			if err := v.Equal(otherV); err != nil {
				return err
			}
		}
		return nil
	}
	if err := checkPending(s.ownRequests, other.ownRequests); err != nil {
		return err
	}
	if err := checkPending(s.globalRequests, other.globalRequests); err != nil {
		return err
	}
	return nil
}

// Clone returns a copied State instance.
func (s *State) Clone() (copied *State) {
	// Clone configuration parts.
	copied = &State{
		lambdaBA:         s.lambdaBA,
		lambdaDKG:        s.lambdaDKG,
		notarySetSize:    s.notarySetSize,
		roundInterval:    s.roundInterval,
		minBlockInterval: s.minBlockInterval,
		local:            s.local,
		logger:           s.logger,
		nodes:            make(map[types.NodeID]crypto.PublicKey),
		dkgComplaints: make(
			map[uint64]map[types.NodeID][]*typesDKG.Complaint),
		dkgMasterPublicKeys: make(
			map[uint64]map[types.NodeID]*typesDKG.MasterPublicKey),
		dkgReadys:       make(map[uint64]map[types.NodeID]*typesDKG.MPKReady),
		dkgFinals:       make(map[uint64]map[types.NodeID]*typesDKG.Finalize),
		dkgSuccesses:    make(map[uint64]map[types.NodeID]*typesDKG.Success),
		appliedRequests: make(map[common.Hash]struct{}),
	}
	// Nodes
	for nID, key := range s.nodes {
		copied.nodes[nID] = key
	}
	// DKG & CRS
	for round, complaintsForRound := range s.dkgComplaints {
		copied.dkgComplaints[round] =
			make(map[types.NodeID][]*typesDKG.Complaint)
		for nID, comps := range complaintsForRound {
			tmpComps := []*typesDKG.Complaint{}
			for _, comp := range comps {
				tmpComps = append(tmpComps, CloneDKGComplaint(comp))
			}
			copied.dkgComplaints[round][nID] = tmpComps
		}
	}
	for round, mKeysForRound := range s.dkgMasterPublicKeys {
		copied.dkgMasterPublicKeys[round] =
			make(map[types.NodeID]*typesDKG.MasterPublicKey)
		for nID, mKey := range mKeysForRound {
			copied.dkgMasterPublicKeys[round][nID] =
				CloneDKGMasterPublicKey(mKey)
		}
	}
	for round, readysForRound := range s.dkgReadys {
		copied.dkgReadys[round] = make(map[types.NodeID]*typesDKG.MPKReady)
		for nID, ready := range readysForRound {
			copied.dkgReadys[round][nID] = CloneDKGMPKReady(ready)
		}
	}
	for round, finalsForRound := range s.dkgFinals {
		copied.dkgFinals[round] = make(map[types.NodeID]*typesDKG.Finalize)
		for nID, final := range finalsForRound {
			copied.dkgFinals[round][nID] = CloneDKGFinalize(final)
		}
	}
	for round, successesForRound := range s.dkgSuccesses {
		copied.dkgSuccesses[round] = make(map[types.NodeID]*typesDKG.Success)
		for nID, success := range successesForRound {
			copied.dkgSuccesses[round][nID] = CloneDKGSuccess(success)
		}
	}
	for _, crs := range s.crs {
		copied.crs = append(copied.crs, crs)
	}
	copied.dkgResetCount = make(map[uint64]uint64, len(s.dkgResetCount))
	for round, count := range s.dkgResetCount {
		copied.dkgResetCount[round] = count
	}
	for hash := range s.appliedRequests {
		copied.appliedRequests[hash] = struct{}{}
	}
	// Pending Changes
	copied.ownRequests = make(map[common.Hash]*StateChangeRequest)
	for k, req := range s.ownRequests {
		copied.ownRequests[k] = req.Clone()
	}
	copied.globalRequests = make(map[common.Hash]*StateChangeRequest)
	for k, req := range s.globalRequests {
		copied.globalRequests[k] = req.Clone()
	}
	return
}

type reqByTime []*StateChangeRequest

func (req reqByTime) Len() int      { return len(req) }
func (req reqByTime) Swap(i, j int) { req[i], req[j] = req[j], req[i] }
func (req reqByTime) Less(i, j int) bool {
	return req[i].Timestamp < req[j].Timestamp
}

// Apply change requests, this function would also
// be called when we extract these request from delivered blocks.
func (s *State) Apply(reqsAsBytes []byte) (err error) {
	if len(reqsAsBytes) == 0 {
		return
	}
	// Try to unmarshal this byte stream into []*StateChangeRequest.
	reqs, err := s.unpackRequests(reqsAsBytes)
	if err != nil {
		return
	}
	sort.Sort(reqByTime(reqs))
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, req := range reqs {
		s.logger.Debug("Apply Request", "req", req)
		// Remove this request from pending set once it's about to apply.
		delete(s.globalRequests, req.Hash)
		delete(s.ownRequests, req.Hash)
		if _, exist := s.appliedRequests[req.Hash]; exist {
			continue
		}
		if err = s.isValidRequest(req); err != nil {
			if err == ErrDuplicatedChange {
				err = nil
				continue
			}
			return
		}
		if err = s.applyRequest(req); err != nil {
			return
		}
		s.appliedRequests[req.Hash] = struct{}{}
	}
	return
}

// AddRequestsFromOthers add requests from others, they won't be packed by
// 'PackOwnRequests'.
func (s *State) AddRequestsFromOthers(reqsAsBytes []byte) (err error) {
	if s.local {
		err = ErrNotInRemoteMode
		return
	}
	reqs, err := s.unpackRequests(reqsAsBytes)
	if err != nil {
		return
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, req := range reqs {
		s.globalRequests[req.Hash] = req
	}
	return
}

// PackRequests pack all current pending requests, include those from others.
func (s *State) PackRequests() (b []byte, err error) {
	if s.local {
		// Convert own requests to global one for packing.
		if _, err = s.PackOwnRequests(); err != nil {
			return
		}
	}
	// Pack requests in global pool.
	packed := []*StateChangeRequest{}
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, v := range s.globalRequests {
		s.logger.Debug("Pack Request", "req", v)
		packed = append(packed, v)
	}
	return rlp.EncodeToBytes(packed)
}

// PackOwnRequests pack current pending requests as byte slice, which
// could be sent as blocks' payload and unmarshall back to apply.
//
// Once a request is packed as own request, it would be turned into a normal
// pending request and won't be packed by this function. This would ensure
// each request broadcasted(gossip) once.
//
// This function is not required to call in local mode.
func (s *State) PackOwnRequests() (b []byte, err error) {
	packed := []*StateChangeRequest{}
	s.lock.Lock()
	defer s.lock.Unlock()
	for k, v := range s.ownRequests {
		delete(s.ownRequests, k)
		s.globalRequests[k] = v
		packed = append(packed, v)
	}
	if b, err = rlp.EncodeToBytes(packed); err != nil {
		return
	}
	return
}

// isValidRequest checks if this request is valid to proceed or not.
func (s *State) isValidRequest(req *StateChangeRequest) error {
	// NOTE: there would be no lock in this helper, callers should be
	//       responsible for acquiring appropriate lock.
	switch req.Type {
	case StateAddDKGMPKReady:
		ready := req.Payload.(*typesDKG.MPKReady)
		if ready.Reset != s.dkgResetCount[ready.Round] {
			return ErrChangeWontApply
		}
	case StateAddDKGFinal:
		final := req.Payload.(*typesDKG.Finalize)
		if final.Reset != s.dkgResetCount[final.Round] {
			return ErrChangeWontApply
		}
	case StateAddDKGSuccess:
		success := req.Payload.(*typesDKG.Success)
		if success.Reset != s.dkgResetCount[success.Round] {
			return ErrChangeWontApply
		}
	case StateAddDKGMasterPublicKey:
		mpk := req.Payload.(*typesDKG.MasterPublicKey)
		if mpk.Reset != s.dkgResetCount[mpk.Round] {
			return ErrChangeWontApply
		}
		// If we've received identical MPK, ignore it.
		mpkForRound, exists := s.dkgMasterPublicKeys[mpk.Round]
		if exists {
			if oldMpk, exists := mpkForRound[mpk.ProposerID]; exists {
				if !oldMpk.Equal(mpk) {
					return ErrDuplicatedChange
				}
				return ErrChangeWontApply
			}
		}
		// If we've received MPK from that proposer, we would ignore
		// its mpk.
		if _, exists := s.dkgReadys[mpk.Round][mpk.ProposerID]; exists {
			return ErrProposerMPKIsReady
		}
	case StateAddDKGComplaint:
		comp := req.Payload.(*typesDKG.Complaint)
		if comp.Reset != s.dkgResetCount[comp.Round] {
			return ErrChangeWontApply
		}
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
	case StateResetDKG:
		newCRS := req.Payload.(common.Hash)
		if s.crs[len(s.crs)-1].Equal(newCRS) {
			return ErrDuplicatedChange
		}
		// TODO(mission): find a smart way to make sure the caller call request
		//                this change with correct resetCount.
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
	case StateAddDKGMPKReady:
		ready := req.Payload.(*typesDKG.MPKReady)
		if _, exists := s.dkgReadys[ready.Round]; !exists {
			s.dkgReadys[ready.Round] = make(map[types.NodeID]*typesDKG.MPKReady)
		}
		s.dkgReadys[ready.Round][ready.ProposerID] = ready
	case StateAddDKGFinal:
		final := req.Payload.(*typesDKG.Finalize)
		if _, exists := s.dkgFinals[final.Round]; !exists {
			s.dkgFinals[final.Round] = make(map[types.NodeID]*typesDKG.Finalize)
		}
		s.dkgFinals[final.Round][final.ProposerID] = final
	case StateAddDKGSuccess:
		success := req.Payload.(*typesDKG.Success)
		if _, exists := s.dkgSuccesses[success.Round]; !exists {
			s.dkgSuccesses[success.Round] =
				make(map[types.NodeID]*typesDKG.Success)
		}
		s.dkgSuccesses[success.Round][success.ProposerID] = success
	case StateResetDKG:
		round := uint64(len(s.crs) - 1)
		s.crs[round] = req.Payload.(common.Hash)
		s.dkgResetCount[round]++
		delete(s.dkgMasterPublicKeys, round)
		delete(s.dkgReadys, round)
		delete(s.dkgComplaints, round)
		delete(s.dkgFinals, round)
		delete(s.dkgSuccesses, round)
	case StateChangeLambdaBA:
		s.lambdaBA = time.Duration(req.Payload.(uint64))
	case StateChangeLambdaDKG:
		s.lambdaDKG = time.Duration(req.Payload.(uint64))
	case StateChangeRoundLength:
		s.roundInterval = req.Payload.(uint64)
	case StateChangeMinBlockInterval:
		s.minBlockInterval = time.Duration(req.Payload.(uint64))
	case StateChangeNotarySetSize:
		s.notarySetSize = req.Payload.(uint32)
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
	s.logger.Info("Request Change to State", "type", t, "value", payload)
	// Patch input parameter's type.
	switch t {
	case StateAddNode:
		payload = payload.(crypto.PublicKey).Bytes()
	case StateChangeLambdaBA,
		StateChangeLambdaDKG,
		StateChangeMinBlockInterval:
		payload = uint64(payload.(time.Duration))
	// These cases for for type assertion, make sure callers pass expected types.
	case StateAddCRS:
		payload = payload.(*crsAdditionRequest)
	case StateAddDKGMPKReady:
		payload = payload.(*typesDKG.MPKReady)
	case StateAddDKGFinal:
		payload = payload.(*typesDKG.Finalize)
	case StateAddDKGSuccess:
		payload = payload.(*typesDKG.Success)
	case StateAddDKGMasterPublicKey:
		payload = payload.(*typesDKG.MasterPublicKey)
	case StateAddDKGComplaint:
		payload = payload.(*typesDKG.Complaint)
	case StateResetDKG:
		payload = payload.(common.Hash)
	}
	req := NewStateChangeRequest(t, payload)
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.local {
		if err = s.isValidRequest(req); err != nil {
			return
		}
		err = s.applyRequest(req)
	} else {
		if err = s.isValidRequest(req); err != nil {
			return
		}
		s.ownRequests[req.Hash] = req
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
			tmpComps = append(tmpComps, CloneDKGComplaint(comp))
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
		mpks = append(mpks, CloneDKGMasterPublicKey(mpk))
	}
	return mpks
}

// IsDKGMPKReady checks if current received dkg readys exceeds threshold.
// This information won't be snapshot, thus can't be cached in test.Governance.
func (s *State) IsDKGMPKReady(round uint64, threshold int) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.dkgReadys[round]) >= threshold
}

// IsDKGFinal checks if current received dkg finals exceeds threshold.
// This information won't be snapshot, thus can't be cached in test.Governance.
func (s *State) IsDKGFinal(round uint64, threshold int) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.dkgFinals[round]) >= threshold
}

// IsDKGSuccess checks if current received dkg successes exceeds threshold.
// This information won't be snapshot, thus can't be cached in test.Governance.
func (s *State) IsDKGSuccess(round uint64, threshold int) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.dkgSuccesses[round]) >= threshold
}

// DKGResetCount returns the reset count for DKG of given round.
func (s *State) DKGResetCount(round uint64) uint64 {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.dkgResetCount[round]
}
