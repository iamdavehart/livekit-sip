package sip

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/livekit/media-sdk/rtp"
	prtp "github.com/pion/rtp"
)

const VideoBridgeH264TestSrc = "h264_testsrc"

func init() {
	RegisterVideoBridge(VideoBridgeH264TestSrc, func(params VideoBridgeParams) (VideoBridge, error) {
		if strings.ToLower(params.Codec.SDPName) != VideoCodecH264 {
			return nil, fmt.Errorf("unsupported video codec %q", params.Codec.SDPName)
		}
		return &h264TestSrcBridge{params: params}, nil
	})
}

type h264TestSrcBridge struct {
	params VideoBridgeParams
	in     rtp.HandlerCloser

	cancel context.CancelFunc
	conn   *net.UDPConn
	wg     sync.WaitGroup
}

func (b *h264TestSrcBridge) Start(ctx context.Context) error {
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

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Close()
		return err
	}
	b.conn = conn

	runCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	writer := b.params.Media.GetVideoWriter()
	b.wg.Add(2)
	go b.runFFmpeg(runCtx, conn.LocalAddr().(*net.UDPAddr).Port)
	go b.forwardRTP(runCtx, conn, writer)
	return nil
}

func (b *h264TestSrcBridge) Close() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	if b.conn != nil {
		_ = b.conn.Close()
		b.conn = nil
	}
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
	b.wg.Wait()
}

func (b *h264TestSrcBridge) runFFmpeg(ctx context.Context, port int) {
	defer b.wg.Done()

	profile := "baseline"
	if strings.Contains(strings.ToLower(b.params.Codec.FMTPLine), "profile-level-id=64") {
		profile = "high"
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-re",
		"-f", "lavfi",
		"-i", "testsrc=size=1278x720:rate=15",
		"-an",
		"-c:v", "libx264",
		"-profile:v", profile,
		"-level:v", "3.0",
		"-pix_fmt", "yuv420p",
		"-tune", "zerolatency",
		"-preset", "veryfast",
		"-b:v", "500k",
		"-bf", "0",
		"-g", "1",
		"-x264-params", "keyint=1:min-keyint=1:scenecut=0:repeat-headers=1",
		"-payload_type", strconv.Itoa(int(b.params.Codec.PayloadType)),
		"-f", "rtp",
		fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1000", port),
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		b.params.Log.Errorw("ffmpeg testsrc exited with failure", err, "output", strings.TrimSpace(string(out)))
		return
	}
	if len(out) != 0 {
		b.params.Log.Debugw("ffmpeg testsrc exited", "output", strings.TrimSpace(string(out)))
	}
}

func (b *h264TestSrcBridge) forwardRTP(ctx context.Context, conn *net.UDPConn, writer rtp.WriteStream) {
	defer b.wg.Done()

	buf := make([]byte, 1500)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			b.params.Log.Warnw("cannot read ffmpeg testsrc RTP", err)
			return
		}

		var pkt prtp.Packet
		if err = pkt.Unmarshal(buf[:n]); err != nil {
			b.params.Log.Debugw("cannot parse ffmpeg testsrc RTP", "error", err)
			continue
		}
		pkt.PayloadType = b.params.Codec.PayloadType
		if _, err = writer.WriteRTP(&pkt.Header, pkt.Payload); err != nil {
			if ctx.Err() != nil {
				return
			}
			b.params.Log.Warnw("cannot write ffmpeg testsrc RTP", err)
			return
		}
	}
}
