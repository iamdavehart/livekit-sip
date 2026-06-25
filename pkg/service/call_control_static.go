// Copyright 2026 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"context"
	"crypto/rand"
	"strconv"
	"time"

	msdk "github.com/livekit/media-sdk"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	"github.com/livekit/protocol/rpc"

	"github.com/livekit/sip/pkg/config"
	"github.com/livekit/sip/pkg/sip"
)

type StaticCallControl struct {
	conf *config.Config
	log  logger.Logger
}

func NewStaticCallControl(conf *config.Config, log logger.Logger) *StaticCallControl {
	return &StaticCallControl{conf: conf, log: log}
}

func (p *StaticCallControl) Init(_ context.Context) error {
	return nil
}

func (p *StaticCallControl) Start(_ rpc.SIPInternalServerImpl) error {
	return nil
}

func (p *StaticCallControl) Stop() {
}

func (p *StaticCallControl) StateHandler(_ string, _ *rpc.SIPCallObservability, _ *livekit.SIPCallInfo) sip.StateHandler {
	return sip.NewRPCStateHandler(nil)
}

func (p *StaticCallControl) GetAuthCredentials(_ context.Context, _ *rpc.SIPCall) (sip.AuthInfo, error) {
	return sip.AuthInfo{
		Result:    sip.AuthAccept,
		ProjectID: p.conf.Control.Static.ProjectID,
		TrunkID:   p.conf.Control.Static.TrunkID,
	}, nil
}

func (p *StaticCallControl) DispatchCall(_ context.Context, info *sip.CallInfo) sip.CallDispatch {
	static := p.conf.Control.Static
	roomName := static.RoomName
	if roomName == "" {
		roomName = "sip-static"
	}
	roomName = roomName + "-" + randomStaticRoomSuffix(6)
	identity := static.ParticipantIdentity
	if identity == "" && info != nil && info.Call != nil {
		identity = info.Call.LkCallId
	}
	if identity == "" {
		identity = "sip-static"
	}
	name := static.ParticipantName
	if name == "" {
		name = identity
	}
	attrs := make(map[string]string, len(static.Attributes))
	for k, v := range static.Attributes {
		attrs[k] = v
	}
	return sip.CallDispatch{
		Result:    sip.DispatchAccept,
		ProjectID: static.ProjectID,
		TrunkID:   static.TrunkID,
		Room: sip.RoomConfig{
			WsUrl:    p.conf.WsUrl,
			RoomName: roomName,
			Participant: sip.ParticipantConfig{
				Identity:   identity,
				Name:       name,
				Metadata:   static.ParticipantMetadata,
				Attributes: attrs,
			},
		},
		MediaConfig: &livekit.SIPMediaConfig{},
	}
}

func randomStaticRoomSuffix(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err == nil {
		for i, b := range buf {
			buf[i] = alphabet[int(b)%len(alphabet)]
		}
		return string(buf)
	}

	s := strconv.FormatInt(time.Now().UnixNano(), 36)
	if len(s) >= n {
		return s[len(s)-n:]
	}
	for len(s) < n {
		s = "0" + s
	}
	return s
}

func (p *StaticCallControl) GetMediaProcessor(_ []livekit.SIPFeature, _ map[string]string, _ string, _ sip.MediaProcessorOpts) msdk.PCM16Processor {
	return nil
}

func (p *StaticCallControl) RegisterTransferSIPParticipantTopic(_ string) error {
	return nil
}

func (p *StaticCallControl) DeregisterTransferSIPParticipantTopic(_ string) {
}

func (p *StaticCallControl) OnInboundInfo(_ logger.Logger, _ *rpc.SIPCall, _ sip.Headers) {
}

func (p *StaticCallControl) OnSessionEnd(_ context.Context, id *sip.CallIdentifier, _ *sip.CallState, reason string) {
	p.log.Infow("SIP call ended", "callID", id.CallID, "sipCallID", id.SipCallID, "reason", reason)
}
