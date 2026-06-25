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
	"testing"

	psdp "github.com/pion/sdp/v3"
	"github.com/stretchr/testify/require"
)

func TestNegotiateVideoOfferH264(t *testing.T) {
	var offer psdp.SessionDescription
	require.NoError(t, offer.Unmarshal([]byte("v=0\r\n"+
		"o=- 1 1 IN IP4 10.0.0.10\r\n"+
		"s=2N\r\n"+
		"c=IN IP4 10.0.0.10\r\n"+
		"t=0 0\r\n"+
		"m=video 40002 RTP/AVP 96\r\n"+
		"a=rtpmap:96 H264/90000\r\n"+
		"a=fmtp:96 packetization-mode=1;profile-level-id=42e01f\r\n"+
		"a=sendrecv\r\n")))

	codec, answer, err := negotiateVideoOffer(&offer, 20000)
	require.NoError(t, err)
	require.Equal(t, uint8(96), codec.PayloadType)
	require.Equal(t, "10.0.0.10:40002", codec.Remote.String())
	require.Equal(t, "packetization-mode=1;profile-level-id=42e01f", codec.FMTPLine)
	require.Equal(t, 20000, answer.MediaName.Port.Value)
	require.Equal(t, []string{"96"}, answer.MediaName.Formats)
	require.Contains(t, answer.Attributes, psdp.Attribute{Key: "fmtp", Value: "96 packetization-mode=1;profile-level-id=42e01f"})
}

func TestNegotiateVideoOfferAddsPacketizationMode(t *testing.T) {
	var offer psdp.SessionDescription
	require.NoError(t, offer.Unmarshal([]byte("v=0\r\n"+
		"o=- 1 1 IN IP4 10.0.0.10\r\n"+
		"s=linphone\r\n"+
		"c=IN IP4 10.0.0.10\r\n"+
		"t=0 0\r\n"+
		"m=video 40002 RTP/AVP 97\r\n"+
		"a=rtpmap:97 H264/90000\r\n"+
		"a=fmtp:97 profile-level-id=42801F\r\n"+
		"a=sendrecv\r\n")))

	codec, answer, err := negotiateVideoOffer(&offer, 20000)
	require.NoError(t, err)
	require.Equal(t, uint8(97), codec.PayloadType)
	require.Equal(t, "profile-level-id=42801F;packetization-mode=1", codec.FMTPLine)
	require.Contains(t, answer.Attributes, psdp.Attribute{Key: "fmtp", Value: "97 profile-level-id=42801F;packetization-mode=1"})
}

func TestNegotiateVideoOfferRejectsUnsupportedVideo(t *testing.T) {
	var offer psdp.SessionDescription
	require.NoError(t, offer.Unmarshal([]byte("v=0\r\n"+
		"o=- 1 1 IN IP4 10.0.0.10\r\n"+
		"s=2N\r\n"+
		"c=IN IP4 10.0.0.10\r\n"+
		"t=0 0\r\n"+
		"m=video 40002 RTP/AVP 97\r\n"+
		"a=rtpmap:97 VP8/90000\r\n"+
		"a=sendrecv\r\n")))

	codec, answer, err := negotiateVideoOffer(&offer, 20000)
	require.ErrorIs(t, err, errNoSupportedVideo)
	require.Nil(t, codec)
	require.NotNil(t, answer)
	require.Equal(t, 0, answer.MediaName.Port.Value)
}
