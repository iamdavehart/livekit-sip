import argparse
import collections
import os
import struct
import sys


ANNEXB = b"\x00\x00\x00\x01"


def read_pcap(path):
    with open(path, "rb") as f:
        header = f.read(24)
        if len(header) != 24:
            raise ValueError("not a pcap file")
        magic = header[:4]
        if magic == b"\xd4\xc3\xb2\xa1":
            endian = "<"
        elif magic == b"\xa1\xb2\xc3\xd4":
            endian = ">"
        elif magic == b"\x4d\x3c\xb2\xa1":
            endian = "<"
        elif magic == b"\xa1\xb2\x3c\x4d":
            endian = ">"
        else:
            raise ValueError(f"unsupported pcap magic {magic.hex()}")
        _magic, _ver_major, _ver_minor, _thiszone, _sigfigs, _snaplen, linktype = struct.unpack(
            endian + "IHHIIII", header
        )
        while True:
            pkt_header = f.read(16)
            if not pkt_header:
                break
            if len(pkt_header) != 16:
                raise ValueError("truncated packet header")
            ts_sec, ts_usec, incl_len, _orig_len = struct.unpack(endian + "IIII", pkt_header)
            data = f.read(incl_len)
            if len(data) != incl_len:
                raise ValueError("truncated packet data")
            yield linktype, ts_sec + (ts_usec / 1_000_000), data


def ipv4_from_frame(linktype, frame):
    if linktype == 1:  # Ethernet
        if len(frame) < 14:
            return None
        eth_type = struct.unpack("!H", frame[12:14])[0]
        offset = 14
        if eth_type == 0x8100 and len(frame) >= 18:
            eth_type = struct.unpack("!H", frame[16:18])[0]
            offset = 18
        if eth_type != 0x0800:
            return None
        return frame[offset:]
    if linktype == 113:  # Linux cooked capture v1
        if len(frame) < 16:
            return None
        proto = struct.unpack("!H", frame[14:16])[0]
        if proto != 0x0800:
            return None
        return frame[16:]
    if linktype == 228:  # IPv4
        return frame
    return None


def parse_udp(linktype, frame):
    pkt = ipv4_from_frame(linktype, frame)
    if not pkt or len(pkt) < 20:
        return None
    version = pkt[0] >> 4
    ihl = (pkt[0] & 0x0F) * 4
    if version != 4 or len(pkt) < ihl + 8:
        return None
    proto = pkt[9]
    if proto != 17:
        return None
    src_ip = ".".join(str(b) for b in pkt[12:16])
    dst_ip = ".".join(str(b) for b in pkt[16:20])
    udp = pkt[ihl:]
    src_port, dst_port, length, _checksum = struct.unpack("!HHHH", udp[:8])
    payload = udp[8 : max(8, length)]
    return src_ip, src_port, dst_ip, dst_port, payload


def parse_rtp(payload):
    if len(payload) < 12:
        return None
    b0, b1 = payload[0], payload[1]
    version = b0 >> 6
    if version != 2:
        return None
    cc = b0 & 0x0F
    x = (b0 >> 4) & 1
    header_len = 12 + (cc * 4)
    if len(payload) < header_len:
        return None
    marker = bool(b1 & 0x80)
    pt = b1 & 0x7F
    seq, ts, ssrc = struct.unpack("!HII", payload[2:12])
    if x:
        if len(payload) < header_len + 4:
            return None
        ext_len_words = struct.unpack("!H", payload[header_len + 2 : header_len + 4])[0]
        header_len += 4 + (ext_len_words * 4)
        if len(payload) < header_len:
            return None
    return pt, marker, seq, ts, ssrc, payload[header_len:]


def h264_nal_type(payload):
    if not payload:
        return None
    return payload[0] & 0x1F


def write_h264(stream_packets, out_path):
    last_fu = None
    wrote = 0
    with open(out_path, "wb") as out:
        for _ts_wall, _key, _pt, marker, seq, ts, ssrc, payload in stream_packets:
            if not payload:
                continue
            nal_type = payload[0] & 0x1F
            if 1 <= nal_type <= 23:
                out.write(ANNEXB + payload)
                wrote += 1
                last_fu = None
            elif nal_type == 24:  # STAP-A
                pos = 1
                while pos + 2 <= len(payload):
                    size = struct.unpack("!H", payload[pos : pos + 2])[0]
                    pos += 2
                    if size == 0 or pos + size > len(payload):
                        break
                    out.write(ANNEXB + payload[pos : pos + size])
                    wrote += 1
                    pos += size
                last_fu = None
            elif nal_type == 28 and len(payload) >= 2:  # FU-A
                fu_indicator, fu_header = payload[0], payload[1]
                start = bool(fu_header & 0x80)
                end = bool(fu_header & 0x40)
                reconstructed_type = fu_header & 0x1F
                nal_header = bytes([(fu_indicator & 0xE0) | reconstructed_type])
                fu_key = (ssrc, ts)
                if start:
                    out.write(ANNEXB + nal_header + payload[2:])
                    wrote += 1
                    last_fu = fu_key
                elif last_fu == fu_key:
                    out.write(payload[2:])
                if end:
                    last_fu = None
            else:
                last_fu = None
    return wrote


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("pcap")
    parser.add_argument("--out-dir", default="extracted_h264")
    args = parser.parse_args()

    streams = collections.defaultdict(list)
    for linktype, ts_wall, frame in read_pcap(args.pcap):
        udp = parse_udp(linktype, frame)
        if not udp:
            continue
        src_ip, src_port, dst_ip, dst_port, payload = udp
        rtp = parse_rtp(payload)
        if not rtp:
            continue
        pt, marker, seq, ts, ssrc, rtp_payload = rtp
        nal = h264_nal_type(rtp_payload)
        if pt < 96 or nal not in {1, 5, 6, 7, 8, 9, 24, 28}:
            continue
        key = (src_ip, src_port, dst_ip, dst_port, ssrc)
        streams[key].append((ts_wall, key, pt, marker, seq, ts, ssrc, rtp_payload))

    if not streams:
        print("no likely H.264 RTP streams found")
        return 1

    os.makedirs(args.out_dir, exist_ok=True)
    for idx, (key, packets) in enumerate(sorted(streams.items(), key=lambda kv: len(kv[1]), reverse=True), 1):
        packets.sort(key=lambda p: p[4])
        src_ip, src_port, dst_ip, dst_port, ssrc = key
        seqs = [p[4] for p in packets]
        gaps = 0
        for a, b in zip(seqs, seqs[1:]):
            expected = (a + 1) & 0xFFFF
            if b != expected:
                gaps += (b - expected) & 0xFFFF
        payload_sizes = [len(p[7]) for p in packets]
        nal_counts = collections.Counter(h264_nal_type(p[7]) for p in packets)
        out_name = f"stream{idx}_{src_ip}_{src_port}_to_{dst_ip}_{dst_port}_ssrc{ssrc}.h264".replace(":", "_")
        out_path = os.path.join(args.out_dir, out_name)
        nals = write_h264(packets, out_path)
        print(
            f"{idx}: {src_ip}:{src_port} -> {dst_ip}:{dst_port} ssrc={ssrc} "
            f"packets={len(packets)} gaps={gaps} max_payload={max(payload_sizes)} "
            f"nal_types={dict(nal_counts)} wrote_nals={nals} file={out_path}"
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
