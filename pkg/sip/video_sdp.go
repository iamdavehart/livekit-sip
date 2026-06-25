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
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	psdp "github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

var errNoSupportedVideo = errors.New("no supported video media")

func getVideoMedia(s *psdp.SessionDescription) *psdp.MediaDescription {
	if s == nil {
		return nil
	}
	for _, m := range s.MediaDescriptions {
		if m.MediaName.Media == "video" {
			return m
		}
	}
	return nil
}

func getMediaDest(s *psdp.SessionDescription, m *psdp.MediaDescription) (netip.AddrPort, error) {
	if s == nil || m == nil {
		return netip.AddrPort{}, errors.New("no media in sdp")
	}
	ci := m.ConnectionInformation
	if ci == nil {
		ci = s.ConnectionInformation
	}
	var addr string
	if ci != nil && ci.NetworkType == "IN" && ci.Address != nil {
		addr = ci.Address.Address
	} else if s.Origin.NetworkType == "IN" {
		addr = s.Origin.UnicastAddress
	}
	if addr == "" {
		return netip.AddrPort{}, errors.New("no destination address in sdp")
	}
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("invalid destination address %q: %w", addr, err)
	}
	return netip.AddrPortFrom(ip, uint16(m.MediaName.Port.Value)), nil
}

func negotiateVideoOffer(offer *psdp.SessionDescription, localPort int) (*VideoCodecConfig, *psdp.MediaDescription, error) {
	video := getVideoMedia(offer)
	if video == nil || video.MediaName.Port.Value == 0 {
		return nil, nil, errNoSupportedVideo
	}
	pt, rtpmap, fmtp, err := selectH264(video)
	if err != nil {
		return nil, rejectVideo(video), err
	}
	remote, err := getMediaDest(offer, video)
	if err != nil {
		return nil, rejectVideo(video), err
	}
	if !remote.IsValid() || remote.Port() == 0 {
		return nil, rejectVideo(video), fmt.Errorf("invalid video address %q", remote)
	}

	protos := append([]string(nil), video.MediaName.Protos...)
	if len(protos) == 0 {
		protos = []string{"RTP", "AVP"}
	}
	answer := &psdp.MediaDescription{
		MediaName: psdp.MediaName{
			Media:   "video",
			Port:    psdp.RangedPort{Value: localPort},
			Protos:  protos,
			Formats: []string{strconv.Itoa(int(pt))},
		},
		Attributes: []psdp.Attribute{
			{Key: "rtpmap", Value: rtpmap},
			{Key: "sendrecv"},
		},
	}
	if fmtp != "" {
		answer.Attributes = append(answer.Attributes, psdp.Attribute{Key: "fmtp", Value: fmtp})
	}
	fmtpLine := ""
	if parts := strings.SplitN(fmtp, " ", 2); len(parts) == 2 {
		fmtpLine = parts[1]
	}
	return &VideoCodecConfig{
		MimeType:    webrtc.MimeTypeH264,
		SDPName:     VideoCodecH264,
		PayloadType: pt,
		ClockRate:   90000,
		FMTPLine:    fmtpLine,
		Remote:      remote,
	}, answer, nil
}

func rejectVideo(video *psdp.MediaDescription) *psdp.MediaDescription {
	if video == nil {
		return nil
	}
	formats := append([]string(nil), video.MediaName.Formats...)
	if len(formats) == 0 {
		formats = []string{"0"}
	}
	protos := append([]string(nil), video.MediaName.Protos...)
	if len(protos) == 0 {
		protos = []string{"RTP", "AVP"}
	}
	return &psdp.MediaDescription{
		MediaName: psdp.MediaName{
			Media:   "video",
			Port:    psdp.RangedPort{Value: 0},
			Protos:  protos,
			Formats: formats,
		},
		Attributes: []psdp.Attribute{{Key: "inactive"}},
	}
}

func selectH264(video *psdp.MediaDescription) (uint8, string, string, error) {
	formatOK := make(map[string]bool, len(video.MediaName.Formats))
	for _, f := range video.MediaName.Formats {
		formatOK[f] = true
	}
	var selected string
	var rtpmap string
	for _, attr := range video.Attributes {
		if attr.Key != "rtpmap" {
			continue
		}
		parts := strings.SplitN(attr.Value, " ", 2)
		if len(parts) != 2 || !formatOK[parts[0]] {
			continue
		}
		name := strings.ToLower(parts[1])
		if name == "h264/90000" || strings.HasPrefix(name, "h264/90000/") {
			selected = parts[0]
			rtpmap = attr.Value
			break
		}
	}
	if selected == "" {
		return 0, "", "", errNoSupportedVideo
	}
	pt64, err := strconv.ParseUint(selected, 10, 8)
	if err != nil {
		return 0, "", "", err
	}
	var fmtp string
	prefix := selected + " "
	for _, attr := range video.Attributes {
		if attr.Key == "fmtp" && strings.HasPrefix(attr.Value, prefix) {
			fmtp = attr.Value
			break
		}
	}
	fmtp = normalizeH264Fmtp(selected, fmtp)
	return uint8(pt64), rtpmap, fmtp, nil
}

func normalizeH264Fmtp(payloadType string, fmtp string) string {
	params := ""
	if fmtp != "" {
		parts := strings.SplitN(fmtp, " ", 2)
		if len(parts) == 2 {
			params = parts[1]
		}
	}
	var out []string
	hasPacketizationMode := false
	for _, param := range strings.Split(params, ";") {
		param = strings.TrimSpace(param)
		if param == "" {
			continue
		}
		key, _, _ := strings.Cut(param, "=")
		if strings.EqualFold(strings.TrimSpace(key), "packetization-mode") {
			out = append(out, "packetization-mode=1")
			hasPacketizationMode = true
			continue
		}
		out = append(out, param)
	}
	if !hasPacketizationMode {
		out = append(out, "packetization-mode=1")
	}
	return payloadType + " " + strings.Join(out, ";")
}
