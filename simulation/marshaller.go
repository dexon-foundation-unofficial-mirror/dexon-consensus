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

package simulation

import (
	"encoding/json"
	"fmt"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/dkg"
)

// jsonMarshaller implements test.Marshaller to marshal simulation related
// messages.
type jsonMarshaller struct{}

// Unmarshal implements Unmarshal method of test.Marshaller interface.
func (m *jsonMarshaller) Unmarshal(
	msgType string, payload []byte) (msg interface{}, err error) {

	switch msgType {
	case "blocklist":
		var blocks BlockList
		if err = json.Unmarshal(payload, &blocks); err != nil {
			break
		}
		msg = &blocks
	case "message":
		var m message
		if err = json.Unmarshal(payload, &m); err != nil {
			break
		}
		msg = &m
	case "info-status":
		var status infoStatus
		if err = json.Unmarshal(payload, &status); err != nil {
			break
		}
		msg = status
	case "block":
		block := &types.Block{}
		if err = json.Unmarshal(payload, block); err != nil {
			break
		}
		msg = block
	case "witness-ack":
		nAck := &types.WitnessAck{}
		if err = json.Unmarshal(payload, nAck); err != nil {
			break
		}
		msg = nAck
	case "vote":
		vote := &types.Vote{}
		if err = json.Unmarshal(payload, vote); err != nil {
			break
		}
		msg = vote
	case "dkg-private-share":
		privateShare := &types.DKGPrivateShare{}
		if err = json.Unmarshal(payload, privateShare); err != nil {
			break
		}
		msg = privateShare
	case "dkg-master-public-key":
		masterPublicKey := &types.DKGMasterPublicKey{
			PublicKeyShares: *dkg.NewEmptyPublicKeyShares(),
		}
		if err = json.Unmarshal(payload, masterPublicKey); err != nil {
			break
		}
		msg = masterPublicKey
	case "dkg-complaint":
		complaint := &types.DKGComplaint{}
		if err = json.Unmarshal(payload, complaint); err != nil {
			break
		}
		msg = complaint
	default:
		err = fmt.Errorf("unrecognized message type: %v", msgType)
	}
	if err != nil {
		return
	}
	return
}

// Marshal implements Marshal method of test.Marshaller interface.
func (m *jsonMarshaller) Marshal(msg interface{}) (
	msgType string, payload []byte, err error) {

	switch msg.(type) {
	case *BlockList:
		msgType = "blocklist"
	case *message:
		msgType = "message"
	case infoStatus:
		msgType = "info-status"
	case *types.Block:
		msgType = "block"
	case *types.WitnessAck:
		msgType = "witness-ack"
	case *types.Vote:
		msgType = "vote"
	case *types.DKGPrivateShare:
		msgType = "dkg-private-share"
	case *types.DKGMasterPublicKey:
		msgType = "dkg-master-public-key"
	case *types.DKGComplaint:
		msgType = "dkg-complaint"
	default:
		err = fmt.Errorf("unknwon message type: %v", msg)
	}
	if err != nil {
		return
	}
	payload, err = json.Marshal(msg)
	return
}
