package sip

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/livekit/media-sdk/rtp"
	"github.com/livekit/protocol/logger"
	prtp "github.com/pion/rtp"
)

const VideoBridgeH264FFmpegDownscale = "h264_ffmpeg_downscale"

func init() {
	RegisterVideoBridge(VideoBridgeH264FFmpegDownscale, func(params VideoBridgeParams) (VideoBridge, error) {
		if strings.ToLower(params.Codec.SDPName) != VideoCodecH264 {
			return nil, fmt.Errorf("unsupported video codec %q", params.Codec.SDPName)
		}
		return &h264FFmpegDownscaleBridge{params: params}, nil
	})
}

type h264FFmpegDownscaleBridge struct {
	params VideoBridgeParams

	roomInput rtp.HandlerCloser
	ffmpegIn  *ffmpegInputRTPWriter
	cancel    context.CancelFunc
	out       *net.UDPConn
	sdp       string
	outCount  atomic.Uint64
	wg        sync.WaitGroup
}

func (b *h264FFmpegDownscaleBridge) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if b.params.Room == nil || b.params.Media == nil {
		return errors.New("missing room or media for video bridge")
	}
	roomInput, err := b.params.Room.NewParticipantVideoTrack(b.params.Codec)
	if err != nil {
		return err
	}
	b.roomInput = roomInput
	b.params.Media.WriteVideoTo(roomInput)

	inPort, err := reserveUDPPort()
	if err != nil {
		b.Close()
		return err
	}

	outConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Close()
		return err
	}
	b.out = outConn

	sdp, err := b.writeInputSDP(inPort)
	if err != nil {
		b.Close()
		return err
	}
	b.sdp = sdp

	dst, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", inPort))
	if err != nil {
		b.Close()
		return err
	}
	input, err := net.DialUDP("udp4", nil, dst)
	if err != nil {
		b.Close()
		return err
	}
	b.ffmpegIn = &ffmpegInputRTPWriter{
		conn:        input,
		payloadType: b.params.Codec.PayloadType,
		log:         b.params.Log,
	}
	b.params.Room.SetVideoOutput(b.ffmpegIn)

	runCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	writer := b.params.Media.GetVideoWriter()
	b.wg.Add(2)
	go b.runFFmpeg(runCtx, outConn.LocalAddr().(*net.UDPAddr).Port)
	go b.forwardRTP(runCtx, outConn, writer)
	return nil
}

func (b *h264FFmpegDownscaleBridge) Close() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	if b.params.Room != nil {
		b.params.Room.SetVideoOutput(nil)
	}
	if b.params.Media != nil {
		b.params.Media.WriteVideoTo(nil)
	}
	if b.roomInput != nil {
		b.roomInput.Close()
		b.roomInput = nil
	}
	if b.ffmpegIn != nil {
		b.ffmpegIn.Close()
		b.ffmpegIn = nil
	}
	if b.out != nil {
		_ = b.out.Close()
		b.out = nil
	}
	b.wg.Wait()
	if b.sdp != "" {
		_ = os.Remove(b.sdp)
		b.sdp = ""
	}
}

func reserveUDPPort() (int, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return 0, err
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if err = conn.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func (b *h264FFmpegDownscaleBridge) writeInputSDP(port int) (string, error) {
	f, err := os.CreateTemp("", "livekit-sip-h264-input-*.sdp")
	if err != nil {
		return "", err
	}
	defer f.Close()

	fmtp := b.params.Codec.FMTPLine
	if fmtp == "" {
		fmtp = fmt.Sprintf("%d packetization-mode=1", b.params.Codec.PayloadType)
	}
	if !strings.HasPrefix(fmtp, strconv.Itoa(int(b.params.Codec.PayloadType))+" ") {
		fmtp = strconv.Itoa(int(b.params.Codec.PayloadType)) + " " + fmtp
	}

	sdpBody := fmt.Sprintf("v=0\r\n"+
		"o=- 0 0 IN IP4 127.0.0.1\r\n"+
		"s=LiveKit H264 input\r\n"+
		"c=IN IP4 127.0.0.1\r\n"+
		"t=0 0\r\n"+
		"m=video %d RTP/AVP %d\r\n"+
		"a=rtpmap:%d H264/90000\r\n"+
		"a=fmtp:%s\r\n",
		port,
		b.params.Codec.PayloadType,
		b.params.Codec.PayloadType,
		fmtp,
	)
	_, err = f.WriteString(sdpBody)
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	b.params.Log.Infow("created ffmpeg downscale input SDP", "path", f.Name(), "sdp", sdpBody)
	return f.Name(), nil
}

func (b *h264FFmpegDownscaleBridge) runFFmpeg(ctx context.Context, outPort int) {
	defer b.wg.Done()

	profile := "baseline"
	if strings.Contains(strings.ToLower(b.params.Codec.FMTPLine), "profile-level-id=64") {
		profile = "high"
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-protocol_whitelist", "file,udp,rtp",
		"-fflags", "+nobuffer+genpts",
		"-flags", "low_delay",
		"-analyzeduration", "0",
		"-probesize", "32",
		"-max_delay", "0",
		"-f", "sdp",
		"-i", b.sdp,
		"-an",
		"-vf", "scale=w=640:h=-2:flags=fast_bilinear,fps=15",
		"-c:v", "libx264",
		"-profile:v", profile,
		"-level:v", "3.0",
		"-pix_fmt", "yuv420p",
		"-tune", "zerolatency",
		"-preset", "ultrafast",
		"-b:v", "500k",
		"-bf", "0",
		"-g", "15",
		"-x264-params", "keyint=15:min-keyint=15:scenecut=0:repeat-headers=1",
		"-payload_type", strconv.Itoa(int(b.params.Codec.PayloadType)),
		"-f", "rtp",
		fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1000", outPort),
	}

	b.params.Log.Infow("starting ffmpeg downscale", "args", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		b.params.Log.Errorw("ffmpeg downscale exited with failure", err, "output", strings.TrimSpace(string(out)))
		return
	}
	if len(out) != 0 {
		b.params.Log.Debugw("ffmpeg downscale exited", "output", strings.TrimSpace(string(out)))
	}
}

func (b *h264FFmpegDownscaleBridge) forwardRTP(ctx context.Context, conn *net.UDPConn, writer rtp.WriteStream) {
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
			b.params.Log.Warnw("cannot read ffmpeg downscale RTP", err)
			return
		}

		var pkt prtp.Packet
		if err = pkt.Unmarshal(buf[:n]); err != nil {
			b.params.Log.Debugw("cannot parse ffmpeg downscale RTP", "error", err)
			continue
		}
		count := b.outCount.Add(1)
		if count == 1 || count%300 == 0 {
			b.params.Log.Infow("ffmpeg downscale output RTP",
				"count", count,
				"payloadType", pkt.PayloadType,
				"sequenceNumber", pkt.SequenceNumber,
				"timestamp", pkt.Timestamp,
				"marker", pkt.Marker,
				"payloadSize", len(pkt.Payload),
			)
		}
		pkt.PayloadType = b.params.Codec.PayloadType
		if _, err = writer.WriteRTP(&pkt.Header, pkt.Payload); err != nil {
			if ctx.Err() != nil {
				return
			}
			b.params.Log.Warnw("cannot write ffmpeg downscale RTP", err)
			return
		}
	}
}

type ffmpegInputRTPWriter struct {
	conn        *net.UDPConn
	payloadType uint8
	count       atomic.Uint64
	log         logger.Logger
}

func (w *ffmpegInputRTPWriter) String() string {
	return "LiveKitVideoRTP -> ffmpeg"
}

func (w *ffmpegInputRTPWriter) WriteRTP(h *prtp.Header, payload []byte) (int, error) {
	count := w.count.Add(1)
	if w.log != nil && (count == 1 || count%300 == 0) {
		w.log.Infow("ffmpeg downscale input RTP",
			"count", count,
			"payloadType", h.PayloadType,
			"sequenceNumber", h.SequenceNumber,
			"timestamp", h.Timestamp,
			"marker", h.Marker,
			"payloadSize", len(payload),
		)
	}
	p := &prtp.Packet{
		Header:  *h,
		Payload: payload,
	}
	p.PayloadType = w.payloadType
	buf, err := p.Marshal()
	if err != nil {
		return 0, err
	}
	return w.conn.Write(buf)
}

func (w *ffmpegInputRTPWriter) Close() {
	if w.conn != nil {
		_ = w.conn.Close()
	}
}
