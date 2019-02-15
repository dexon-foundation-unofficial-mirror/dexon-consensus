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

package simulation

import (
	"encoding/json"
	"fmt"
)

// jsonMarshaller implements test.Marshaller to marshal simulation related
// messages.
type jsonMarshaller struct{}

// Unmarshal implements Unmarshal method of test.Marshaller interface.
func (m *jsonMarshaller) Unmarshal(
	msgType string, payload []byte) (msg interface{}, err error) {
	switch msgType {
	case "server-notif":
		var notif serverNotification
		if err = json.Unmarshal(payload, &notif); err != nil {
			break
		}
		msg = notif
	case "message":
		var m message
		if err = json.Unmarshal(payload, &m); err != nil {
			break
		}
		msg = &m
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
	case serverNotification:
		msgType = "server-notif"
	case *message:
		msgType = "message"
	default:
		err = fmt.Errorf("unknwon message type: %v", msg)
	}
	if err != nil {
		return
	}
	payload, err = json.Marshal(msg)
	return
}
