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
	"fmt"
	"math"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon/rlp"
)

// StateChangeType is the type of state change request.
type StateChangeType uint8

// Types of state change.
const (
	StateChangeNothing StateChangeType = iota
	// DKG & CRS
	StateAddCRS
	StateAddDKGComplaint
	StateAddDKGMasterPublicKey
	StateAddDKGMPKReady
	StateAddDKGFinal
	// Configuration related.
	StateChangeNumChains
	StateChangeLambdaBA
	StateChangeLambdaDKG
	StateChangeRoundInterval
	StateChangeMinBlockInterval
	StateChangeK
	StateChangePhiRatio
	StateChangeNotarySetSize
	StateChangeDKGSetSize
	// Node set related.
	StateAddNode
)

func (t StateChangeType) String() string {
	switch t {
	case StateChangeNothing:
		return "Nothing"
	case StateAddCRS:
		return "AddCRS"
	case StateAddDKGComplaint:
		return "AddDKGComplaint"
	case StateAddDKGMasterPublicKey:
		return "AddDKGMasterPublicKey"
	case StateAddDKGMPKReady:
		return "AddDKGMPKReady"
	case StateAddDKGFinal:
		return "AddDKGFinal"
	case StateChangeNumChains:
		return "ChangeNumChains"
	case StateChangeLambdaBA:
		return "ChangeLambdaBA"
	case StateChangeLambdaDKG:
		return "ChangeLambdaDKG"
	case StateChangeRoundInterval:
		return "ChangeRoundInterval"
	case StateChangeMinBlockInterval:
		return "ChangeMinBlockInterval"
	case StateChangeK:
		return "ChangeK"
	case StateChangePhiRatio:
		return "ChangePhiRatio"
	case StateChangeNotarySetSize:
		return "ChangeNotarySetSize"
	case StateChangeDKGSetSize:
		return "ChangeDKGSetSize"
	case StateAddNode:
		return "AddNode"
	}
	panic(fmt.Errorf("attempting to dump unknown type of state change: %d", t))
}

// StateChangeRequest carries information of state change request.
type StateChangeRequest struct {
	Type    StateChangeType `json:"type"`
	Payload interface{}     `json:"payload"`
	// The purpose of these fields are aiming to provide an unique ID for each
	// change request.
	Hash      common.Hash
	Timestamp uint64
}

// this structure is mainly for marshalling for StateChangeRequest.
type rawStateChangeRequest struct {
	Type      StateChangeType
	Payload   rlp.RawValue
	Hash      common.Hash
	Timestamp uint64
}

// NewStateChangeRequest constructs an StateChangeRequest instance.
func NewStateChangeRequest(
	t StateChangeType, payload interface{}) *StateChangeRequest {
	now := uint64(time.Now().UTC().UnixNano())
	b, err := rlp.EncodeToBytes(struct {
		Type      StateChangeType
		Payload   interface{}
		Timestamp uint64
	}{t, payload, now})
	if err != nil {
		panic(err)
	}
	return &StateChangeRequest{
		Hash:      crypto.Keccak256Hash(b),
		Type:      t,
		Payload:   payload,
		Timestamp: now,
	}
}

// Clone a StateChangeRequest instance.
func (req *StateChangeRequest) Clone() (copied *StateChangeRequest) {
	copied = &StateChangeRequest{
		Type:      req.Type,
		Hash:      req.Hash,
		Timestamp: req.Timestamp,
	}
	// NOTE: The cloned DKGx structs would be different from sources in binary
	//       level, thus would produce different hash from the source.
	//       I don't want different hash for source/copied requests thus would
	//       copy the hash from source directly.
	switch req.Type {
	case StateAddNode:
		srcBytes := req.Payload.([]byte)
		copiedBytes := make([]byte, len(srcBytes))
		copy(copiedBytes, srcBytes)
		req.Payload = copiedBytes
	case StateAddCRS:
		crsReq := req.Payload.(*crsAdditionRequest)
		copied.Payload = &crsAdditionRequest{
			Round: crsReq.Round,
			CRS:   crsReq.CRS,
		}
	case StateAddDKGMPKReady:
		copied.Payload = CloneDKGMPKReady(req.Payload.(*typesDKG.MPKReady))
	case StateAddDKGFinal:
		copied.Payload = CloneDKGFinalize(req.Payload.(*typesDKG.Finalize))
	case StateAddDKGMasterPublicKey:
		copied.Payload = CloneDKGMasterPublicKey(
			req.Payload.(*typesDKG.MasterPublicKey))
	case StateAddDKGComplaint:
		copied.Payload = CloneDKGComplaint(req.Payload.(*typesDKG.Complaint))
	default:
		copied.Payload = req.Payload
	}
	return
}

// Equal checks equality between two StateChangeRequest.
func (req *StateChangeRequest) Equal(other *StateChangeRequest) error {
	if req.Hash == other.Hash {
		return nil
	}
	return ErrStatePendingChangesNotEqual
}

// String dump the state change request into string form.
func (req *StateChangeRequest) String() (ret string) {
	ret = fmt.Sprintf("stateChangeRequest{Type:%s", req.Type)
	switch req.Type {
	case StateChangeNothing:
	case StateAddCRS:
		crsReq := req.Payload.(*crsAdditionRequest)
		ret += fmt.Sprintf(
			"Round:%v CRS:%s", crsReq.Round, crsReq.CRS.String()[:6])
	case StateAddDKGComplaint:
		ret += fmt.Sprintf("%s", req.Payload.(*typesDKG.Complaint))
	case StateAddDKGMasterPublicKey:
		ret += fmt.Sprintf("%s", req.Payload.(*typesDKG.MasterPublicKey))
	case StateAddDKGMPKReady:
		ret += fmt.Sprintf("%s", req.Payload.(*typesDKG.MPKReady))
	case StateAddDKGFinal:
		ret += fmt.Sprintf("%s", req.Payload.(*typesDKG.Finalize))
	case StateChangeNumChains:
		ret += fmt.Sprintf("%v", req.Payload.(uint32))
	case StateChangeLambdaBA:
		ret += fmt.Sprintf("%v", time.Duration(req.Payload.(uint64)))
	case StateChangeLambdaDKG:
		ret += fmt.Sprintf("%v", time.Duration(req.Payload.(uint64)))
	case StateChangeRoundInterval:
		ret += fmt.Sprintf("%v", time.Duration(req.Payload.(uint64)))
	case StateChangeMinBlockInterval:
		ret += fmt.Sprintf("%v", time.Duration(req.Payload.(uint64)))
	case StateChangeK:
		ret += fmt.Sprintf("%v", req.Payload.(uint64))
	case StateChangePhiRatio:
		ret += fmt.Sprintf("%v", math.Float32frombits(req.Payload.(uint32)))
	case StateChangeNotarySetSize:
		ret += fmt.Sprintf("%v", req.Payload.(uint32))
	case StateChangeDKGSetSize:
		ret += fmt.Sprintf("%v", req.Payload.(uint32))
	case StateAddNode:
		ret += fmt.Sprintf(
			"%s", types.NewNodeID(req.Payload.(crypto.PublicKey)).String()[:6])
	default:
		panic(fmt.Errorf(
			"attempting to dump unknown type of state change request: %v",
			req.Type))
	}
	ret += "}"
	return
}
