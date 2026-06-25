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

package sip

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"

	"github.com/livekit/media-sdk/rtp"
	"github.com/livekit/protocol/logger"
)

const (
	VideoCodecH264 = "h264"
)

type VideoCodecConfig struct {
	MimeType    string
	SDPName     string
	PayloadType uint8
	ClockRate   uint32
	FMTPLine    string
	Remote      netip.AddrPort
}

type VideoBridgeParams struct {
	Log   logger.Logger
	Room  RoomInterface
	Media *MediaPort
	Codec VideoCodecConfig
}

type VideoBridge interface {
	Start(ctx context.Context) error
	Close()
}

type VideoBridgeFactory func(params VideoBridgeParams) (VideoBridge, error)

var videoBridges = struct {
	sync.RWMutex
	byName map[string]VideoBridgeFactory
}{
	byName: make(map[string]VideoBridgeFactory),
}

func RegisterVideoBridge(name string, factory VideoBridgeFactory) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		panic("sip: video bridge name is empty")
	}
	if factory == nil {
		panic("sip: video bridge factory is nil")
	}
	videoBridges.Lock()
	defer videoBridges.Unlock()
	if _, ok := videoBridges.byName[name]; ok {
		panic(fmt.Sprintf("sip: video bridge %q already registered", name))
	}
	videoBridges.byName[name] = factory
}

func getVideoBridge(name string) (VideoBridgeFactory, bool) {
	videoBridges.RLock()
	defer videoBridges.RUnlock()
	f, ok := videoBridges.byName[strings.ToLower(strings.TrimSpace(name))]
	return f, ok
}

func init() {
	RegisterVideoBridge(VideoCodecH264, func(params VideoBridgeParams) (VideoBridge, error) {
		if strings.ToLower(params.Codec.SDPName) != VideoCodecH264 {
			return nil, fmt.Errorf("unsupported video codec %q", params.Codec.SDPName)
		}
		return &h264PassthroughBridge{params: params}, nil
	})
}

type h264PassthroughBridge struct {
	params VideoBridgeParams
	in     rtp.HandlerCloser
}

func (b *h264PassthroughBridge) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if b.params.Room == nil || b.params.Media == nil {
		return errors.New("missing room or media for video bridge")
	}
	in, err := b.params.Room.NewParticipantVideoTrack(b.params.Codec)
	if err != nil {
		return err
	}
	b.in = in
	b.params.Media.WriteVideoTo(in)
	b.params.Room.SetVideoOutput(b.params.Media.GetVideoWriter())
	return nil
}

func (b *h264PassthroughBridge) Close() {
	if b.params.Room != nil {
		b.params.Room.SetVideoOutput(nil)
	}
	if b.params.Media != nil {
		b.params.Media.WriteVideoTo(nil)
	}
	if b.in != nil {
		b.in.Close()
		b.in = nil
	}
}
