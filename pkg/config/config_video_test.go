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

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoConfigDefaults(t *testing.T) {
	conf, err := NewConfig("redis:\n  address: localhost:6379\n")
	require.NoError(t, err)

	require.Equal(t, "h264", conf.Video.Codec)
	require.Equal(t, "h264", conf.Video.Bridge)
	require.True(t, conf.Video.FallbackAudioOnly)
}

func TestVideoConfigExplicitFallbackFalse(t *testing.T) {
	conf, err := NewConfig(`
redis:
  address: localhost:6379
video:
  enabled: true
  fallback_audio_only: false
`)
	require.NoError(t, err)

	require.True(t, conf.Video.Enabled)
	require.False(t, conf.Video.FallbackAudioOnly)
}

func TestConfigDefaultProviderRequiresRedis(t *testing.T) {
	_, err := NewConfig("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "redis configuration is required")
}

func TestConfigStaticProviderAllowsNoRedis(t *testing.T) {
	conf, err := NewConfig(`
control:
  provider: static
  static:
    room_name: intercom-test
`)
	require.NoError(t, err)
	require.Equal(t, CallControlProviderStatic, conf.Control.Provider)
	require.Nil(t, conf.Redis)
	require.Equal(t, "intercom-test", conf.Control.Static.RoomName)
}

func TestConfigLiveKitAPIProviderAllowsNoRedis(t *testing.T) {
	conf, err := NewConfig(`
api_key: key
api_secret: secret
ws_url: wss://example.livekit.cloud
control:
  provider: livekit_api
  livekit_api:
    project_id: project-a
`)
	require.NoError(t, err)
	require.Equal(t, CallControlProviderLiveKitAPI, conf.Control.Provider)
	require.Nil(t, conf.Redis)
	require.Equal(t, "project-a", conf.Control.LiveKitAPI.ProjectID)
}

func TestConfigLiveKitAPIProviderRequiresAPIConfig(t *testing.T) {
	_, err := NewConfig(`
control:
  provider: livekit_api
`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key, api_secret, and ws_url are required")
}
