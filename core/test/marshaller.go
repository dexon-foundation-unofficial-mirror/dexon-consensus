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
	"encoding/json"
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

// DefaultMarshaller is the default marshaller for testing core.Consensus.
type DefaultMarshaller struct {
	fallback Marshaller
}

// NewDefaultMarshaller constructs an DefaultMarshaller instance.
func NewDefaultMarshaller(fallback Marshaller) *DefaultMarshaller {
	return &DefaultMarshaller{
		fallback: fallback,
	}
}

// Unmarshal implements Marshaller interface.
func (m *DefaultMarshaller) Unmarshal(
	msgType string, payload []byte) (msg interface{}, err error) {
	switch msgType {
	case "block":
		block := &types.Block{}
		if err = json.Unmarshal(payload, block); err != nil {
			break
		}
		msg = block
	case "vote":
		vote := &types.Vote{}
		if err = json.Unmarshal(payload, vote); err != nil {
			break
		}
		msg = vote
	case "agreement-result":
		result := &types.AgreementResult{}
		if err = json.Unmarshal(payload, result); err != nil {
			break
		}
		msg = result
	case "dkg-private-share":
		privateShare := &typesDKG.PrivateShare{}
		if err = json.Unmarshal(payload, privateShare); err != nil {
			break
		}
		msg = privateShare
	case "dkg-master-public-key":
		masterPublicKey := typesDKG.NewMasterPublicKey()
		if err = json.Unmarshal(payload, masterPublicKey); err != nil {
			break
		}
		msg = masterPublicKey
	case "dkg-complaint":
		complaint := &typesDKG.Complaint{}
		if err = json.Unmarshal(payload, complaint); err != nil {
			break
		}
		msg = complaint
	case "dkg-partial-signature":
		psig := &typesDKG.PartialSignature{}
		if err = json.Unmarshal(payload, psig); err != nil {
			break
		}
		msg = psig
	case "dkg-finalize":
		final := &typesDKG.Finalize{}
		if err = json.Unmarshal(payload, final); err != nil {
			break
		}
		msg = final
	case "packed-state-changes":
		packed := &packedStateChanges{}
		if err = json.Unmarshal(payload, packed); err != nil {
			break
		}
		msg = *packed
	case "pull-request":
		req := &PullRequest{}
		if err = json.Unmarshal(payload, req); err != nil {
			break
		}
		msg = req
	default:
		if m.fallback == nil {
			err = fmt.Errorf("unknown msg type: %v", msgType)
			break
		}
		msg, err = m.fallback.Unmarshal(msgType, payload)
	}
	return
}

// Marshal implements Marshaller interface.
func (m *DefaultMarshaller) Marshal(
	msg interface{}) (msgType string, payload []byte, err error) {
	switch msg.(type) {
	case *types.Block:
		msgType = "block"
		payload, err = json.Marshal(msg)
	case *types.Vote:
		msgType = "vote"
		payload, err = json.Marshal(msg)
	case *types.AgreementResult:
		msgType = "agreement-result"
		payload, err = json.Marshal(msg)
	case *typesDKG.PrivateShare:
		msgType = "dkg-private-share"
		payload, err = json.Marshal(msg)
	case *typesDKG.MasterPublicKey:
		msgType = "dkg-master-public-key"
		payload, err = json.Marshal(msg)
	case *typesDKG.Complaint:
		msgType = "dkg-complaint"
		payload, err = json.Marshal(msg)
	case *typesDKG.PartialSignature:
		msgType = "dkg-partial-signature"
		payload, err = json.Marshal(msg)
	case *typesDKG.Finalize:
		msgType = "dkg-finalize"
		payload, err = json.Marshal(msg)
	case packedStateChanges:
		msgType = "packed-state-changes"
		payload, err = json.Marshal(msg)
	case *PullRequest:
		msgType = "pull-request"
		payload, err = json.Marshal(msg)
	default:
		if m.fallback == nil {
			err = fmt.Errorf("unknwon message type: %v", msg)
			break
		}
		msgType, payload, err = m.fallback.Marshal(msg)
	}
	return
}
