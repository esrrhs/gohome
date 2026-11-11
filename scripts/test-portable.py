"""Small native cross-platform gate; each entry must run and pass, never skip."""
import pathlib
import subprocess
import sys
import tempfile

# Keep the expensive full suite / crypto / race-slow in the existing Linux jobs.
# Separate entries make an accidentally empty package or protocol fail closed.
ENTRIES = [
    ("list", ["./list/..."], "^Test"),
    ("lru", ["./lru/..."], "^Test"),
    ("pool", ["./pool/..."], "^Test"),
    ("dns-cache", ["./dns/cache/..."], "^Test"),
    ("dns-matcher", ["./dns/matcher/..."], "^Test"),
    ("dns-fakeip", ["./dns/fakeip/..."], "^Test"),
    ("thread-lifecycle", ["./thread"], "^Test(Harden|TaskPool_Stats|GroupError|ConcurrentStat)"),
    ("tcp", ["./network"], "^TestTcpListenCloseJoinEcho$"),
    ("udp", ["./network"], "^TestUdpCloseJoinEcho$"),
    ("rudp", ["./network"], "^TestRudpAcceptImmediateReadWrite$"),
    ("kcp", ["./network"], "^TestKcp(AcceptImmediateReadWrite|FecLoopback)$"),
    ("quic", ["./network"], "^TestQuicAcceptImmediateReadWrite$"),
    ("rhttp", ["./network"], "^TestRhttpH(1|2C)ReusesConnection$"),
    ("wss", ["./network"], "^TestWss"),
    ("rdns", ["./network"], "^TestRdns"),
]


def main():
    print("RICMP/raw ICMP deliberately omitted: privileged IPv4/IPv6 coverage stays "
          "on ubuntu-latest. No setcap, sysctl, modprobe or netem in this gate.", flush=True)
    checker = pathlib.Path(__file__).with_name("check-protocol-tests.py").resolve()
    # Keep logs within the checkout and print full JSON on failures.
    with tempfile.TemporaryDirectory(prefix="portable-ci-", dir=".") as tmp:
        for name, packages, selector in ENTRIES:
            print(f"::group::{name} (race)", flush=True)
            log = pathlib.Path(tmp) / f"{name}.json"
            with log.open("w", encoding="utf-8") as output:
                result = subprocess.run(
                    ["go", "test", "-race", "-json", "-count=1", "-timeout=3m",
                     "-run", selector, *packages], stdout=output, check=False)
            checked = subprocess.run([sys.executable, str(checker), str(log)], check=False)
            if result.returncode or checked.returncode:
                print(log.read_text(encoding="utf-8"), flush=True)
                sys.exit(result.returncode or checked.returncode)
            print("::endgroup::", flush=True)


if __name__ == "__main__":
    main()
