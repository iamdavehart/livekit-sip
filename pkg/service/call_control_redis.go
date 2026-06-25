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
	"fmt"

	msdk "github.com/livekit/media-sdk"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	"github.com/livekit/protocol/redis"
	"github.com/livekit/protocol/rpc"
	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/pkg/middleware/otelpsrpc"

	"github.com/livekit/sip/pkg/config"
	"github.com/livekit/sip/pkg/sip"
)

type RedisCallControl struct {
	conf         *config.Config
	log          logger.Logger
	bus          psrpc.MessageBus
	psrpcClient  rpc.IOInfoSIPClient
	rpcSIPServer rpc.SIPInternalServer
}

func NewRedisCallControl(conf *config.Config, log logger.Logger) *RedisCallControl {
	return &RedisCallControl{conf: conf, log: log}
}

func NewRedisCallControlWithClient(conf *config.Config, log logger.Logger, psrpcClient rpc.IOInfoSIPClient, bus psrpc.MessageBus) *RedisCallControl {
	return &RedisCallControl{
		conf:        conf,
		log:         log,
		bus:         bus,
		psrpcClient: psrpcClient,
	}
}

func (p *RedisCallControl) Init(_ context.Context) error {
	if p.psrpcClient != nil && p.bus != nil {
		return nil
	}

	rc, err := redis.GetRedisClient(p.conf.Redis)
	if err != nil {
		return err
	}

	p.bus = psrpc.NewRedisMessageBus(rc)
	p.psrpcClient, err = rpc.NewIOInfoSIPClient(p.bus,
		otelpsrpc.ClientOptions(otelpsrpc.Config{}),
	)
	return err
}

func (p *RedisCallControl) Start(server rpc.SIPInternalServerImpl) error {
	if p.bus == nil {
		return fmt.Errorf("redis control provider message bus is not initialized")
	}
	if server == nil {
		return nil
	}

	var err error
	p.rpcSIPServer, err = rpc.NewSIPInternalServer(server, p.bus,
		otelpsrpc.ServerOptions(otelpsrpc.Config{}),
	)
	if err != nil {
		return err
	}

	if err = p.rpcSIPServer.RegisterCreateSIPParticipantTopic(p.conf.ClusterID); err != nil {
		p.rpcSIPServer.Shutdown()
		p.rpcSIPServer = nil
		return err
	}
	return nil
}

func (p *RedisCallControl) Stop() {
	if p.rpcSIPServer != nil {
		p.rpcSIPServer.DeregisterCreateSIPParticipantTopic(p.conf.ClusterID)
		p.rpcSIPServer.Shutdown()
		p.rpcSIPServer = nil
	}
}

func (p *RedisCallControl) StateHandler(_ string, _ *rpc.SIPCallObservability, _ *livekit.SIPCallInfo) sip.StateHandler {
	return sip.NewRPCStateHandler(p.psrpcClient)
}

func (p *RedisCallControl) GetAuthCredentials(ctx context.Context, call *rpc.SIPCall) (sip.AuthInfo, error) {
	return GetAuthCredentials(ctx, p.psrpcClient, call)
}

func (p *RedisCallControl) DispatchCall(ctx context.Context, info *sip.CallInfo) sip.CallDispatch {
	return DispatchCall(ctx, p.psrpcClient, p.log, info)
}

func (p *RedisCallControl) GetMediaProcessor(_ []livekit.SIPFeature, _ map[string]string, _ string, _ sip.MediaProcessorOpts) msdk.PCM16Processor {
	return nil
}

func (p *RedisCallControl) RegisterTransferSIPParticipantTopic(sipCallId string) error {
	if p.rpcSIPServer != nil {
		return p.rpcSIPServer.RegisterTransferSIPParticipantTopic(sipCallId)
	}

	return psrpc.NewErrorf(psrpc.Internal, "RPC server not started")
}

func (p *RedisCallControl) DeregisterTransferSIPParticipantTopic(sipCallId string) {
	if p.rpcSIPServer != nil {
		p.rpcSIPServer.DeregisterTransferSIPParticipantTopic(sipCallId)
	}
}

func (p *RedisCallControl) OnInboundInfo(_ logger.Logger, _ *rpc.SIPCall, _ sip.Headers) {
}

func (p *RedisCallControl) OnSessionEnd(_ context.Context, callIdentifier *sip.CallIdentifier, _ *sip.CallState, reason string) {
	p.log.Infow("SIP call ended", "callID", callIdentifier.CallID, "reason", reason)
}
