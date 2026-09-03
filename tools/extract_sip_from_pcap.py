import sys

from extract_h264_from_pcap import parse_udp, read_pcap


def main():
    if len(sys.argv) != 2:
        print("usage: extract_sip_from_pcap.py <pcap>")
        return 2
    for _linktype, ts, frame in read_pcap(sys.argv[1]):
        udp = parse_udp(_linktype, frame)
        if not udp:
            continue
        src_ip, src_port, dst_ip, dst_port, payload = udp
        if src_port != 5060 and dst_port != 5060:
            continue
        text = payload.decode("utf-8", "replace")
        if "SIP/2.0" not in text and not text.startswith(("INVITE ", "ACK ", "BYE ", "REGISTER ", "OPTIONS ")):
            continue
        print(f"--- {ts:.6f} {src_ip}:{src_port} -> {dst_ip}:{dst_port} len={len(payload)}")
        print(text.replace("\r\n", "\n"))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
