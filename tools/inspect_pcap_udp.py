import collections
import sys

from extract_h264_from_pcap import parse_rtp, parse_udp, read_pcap


def main():
    if len(sys.argv) != 2:
        print("usage: inspect_pcap_udp.py <pcap>")
        return 2
    udp_counts = collections.Counter()
    rtp_counts = collections.Counter()
    total = 0
    linktypes = collections.Counter()
    first_ts = None
    last_ts = None
    for linktype, ts, frame in read_pcap(sys.argv[1]):
        total += 1
        linktypes[linktype] += 1
        first_ts = ts if first_ts is None else min(first_ts, ts)
        last_ts = ts if last_ts is None else max(last_ts, ts)
        udp = parse_udp(linktype, frame)
        if not udp:
            continue
        src_ip, src_port, dst_ip, dst_port, payload = udp
        udp_counts[(src_ip, src_port, dst_ip, dst_port)] += 1
        rtp = parse_rtp(payload)
        if rtp:
            pt, _marker, _seq, _rtpts, ssrc, _rtp_payload = rtp
            rtp_counts[(src_ip, src_port, dst_ip, dst_port, pt, ssrc)] += 1
    print(f"packets={total} linktypes={dict(linktypes)} first={first_ts} last={last_ts}")
    print("top udp:")
    for key, count in udp_counts.most_common(20):
        print(count, key)
    print("top rtp-like:")
    for key, count in rtp_counts.most_common(20):
        print(count, key)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
