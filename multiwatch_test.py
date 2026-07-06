#!/usr/bin/env python3
"""
Multi-key WATCH + concurrency test for the Redis clone.

Covers the two fixes:
  #2  WATCH is variadic -- one `WATCH k1 k2 k3` must arm ALL the keys, not just
      the first. (Part 1, deterministic PASS/FAIL.)
  #1  the shared versions map must be read under the same lock it's written
      under, or it's a data race. (Part 2, a concurrency stressor.)

Run Part 1 against any running server:
    go run ./app/
    python3 multiwatch_test.py

To make Part 2 meaningful, start the server under Go's race detector instead,
so an unsynchronized versions access is REPORTED rather than left to chance:
    go run -race ./app/
    python3 multiwatch_test.py
A clean run then means: all keys are armed, and the versions map is race-free
under heavy contention. A "DATA RACE" report shows up on the SERVER's terminal.
"""

import os
import socket
import sys
import threading

HOST = "127.0.0.1"
PORT = int(os.environ.get("PORT", "6379"))  # override with PORT=... if needed


def enc(*args):
    """Encode a command as a RESP array of bulk strings."""
    out = b"*%d\r\n" % len(args)
    for a in args:
        a = str(a).encode()
        out += b"$%d\r\n%s\r\n" % (len(a), a)
    return out


class Conn:
    """A single connection that reads exactly one complete RESP reply at a time."""

    def __init__(self, name):
        self.name = name
        self.sock = socket.create_connection((HOST, PORT))
        self.buf = b""

    def _fill(self):
        chunk = self.sock.recv(4096)
        if not chunk:
            raise ConnectionError("server closed the connection")
        self.buf += chunk

    def _line(self):
        while b"\r\n" not in self.buf:
            self._fill()
        line, self.buf = self.buf.split(b"\r\n", 1)
        return line

    def _read_n(self, n):
        while len(self.buf) < n + 2:  # n payload bytes + trailing \r\n
            self._fill()
        data, self.buf = self.buf[:n], self.buf[n + 2:]
        return data

    def read_reply(self):
        line = self._line()
        t, body = line[:1], line[1:]
        if t == b"+":
            return ("simple", body.decode())
        if t == b"-":
            return ("error", body.decode())
        if t == b":":
            return ("int", int(body))
        if t == b"$":
            n = int(body)
            return ("bulk", None) if n == -1 else ("bulk", self._read_n(n).decode())
        if t == b"*":
            m = int(body)
            return ("array", None) if m == -1 else ("array", [self.read_reply() for _ in range(m)])
        raise ValueError("unrecognized reply: %r" % line)

    def cmd(self, *args):
        self.sock.sendall(enc(*args))
        return self.read_reply()


results = []


def check(desc, got, expected):
    ok = got == expected
    results.append(ok)
    print("  [%s] %s" % ("PASS" if ok else "FAIL", desc))
    if not ok:
        print("        expected: %r" % (expected,))
        print("        got:      %r" % (got,))


# ===================== Part 1: variadic WATCH (deterministic) =================
print("Part 1 -- one WATCH command must arm every key it lists\n")

# A. WATCH k1 k2 in ONE command, then modify the SECOND key -> must abort.
#    This is the core proof: if only the first key were armed, this would run.
print("A. WATCH ka1 ka2 (single cmd), c2 changes ka2 -> abort")
c1, c2 = Conn("c1"), Conn("c2")
check("c1 SET ka1 1 -> +OK", c1.cmd("SET", "ka1", "1"), ("simple", "OK"))
check("c1 SET ka2 1 -> +OK", c1.cmd("SET", "ka2", "1"), ("simple", "OK"))
check("c1 WATCH ka1 ka2 -> +OK", c1.cmd("WATCH", "ka1", "ka2"), ("simple", "OK"))
check("c1 MULTI -> +OK", c1.cmd("MULTI"), ("simple", "OK"))
check("c1 INCR ka1 -> +QUEUED", c1.cmd("INCR", "ka1"), ("simple", "QUEUED"))
check("c2 SET ka2 999 -> +OK", c2.cmd("SET", "ka2", "999"), ("simple", "OK"))
check("c1 EXEC -> null array (ka2 was armed)", c1.cmd("EXEC"), ("array", None))
check("c1 GET ka1 -> 1 (INCR never ran)", c1.cmd("GET", "ka1"), ("bulk", "1"))

# B. WATCH three keys, modify the LAST one -> abort. Arming must reach the
#    end of the list (kb2/kb3 start missing = version 0; creating kb3 bumps it).
print("B. WATCH kb1 kb2 kb3 (single cmd), c2 creates kb3 -> abort")
b1, b2 = Conn("b1"), Conn("b2")
check("c1 SET kb1 1 -> +OK", b1.cmd("SET", "kb1", "1"), ("simple", "OK"))
check("c1 WATCH kb1 kb2 kb3 -> +OK", b1.cmd("WATCH", "kb1", "kb2", "kb3"), ("simple", "OK"))
check("c1 MULTI -> +OK", b1.cmd("MULTI"), ("simple", "OK"))
check("c1 INCR kb1 -> +QUEUED", b1.cmd("INCR", "kb1"), ("simple", "QUEUED"))
check("c2 SET kb3 5 -> +OK", b2.cmd("SET", "kb3", "5"), ("simple", "OK"))
check("c1 EXEC -> null array (kb3 was armed)", b1.cmd("EXEC"), ("array", None))

# C. WATCH k1 k2, modify an UNwatched key -> EXEC still runs (no over-arming).
print("C. WATCH kc1 kc2 (single cmd), c2 changes kc9 (unwatched) -> runs")
d1, d2 = Conn("d1"), Conn("d2")
check("c1 SET kc1 1 -> +OK", d1.cmd("SET", "kc1", "1"), ("simple", "OK"))
check("c1 WATCH kc1 kc2 -> +OK", d1.cmd("WATCH", "kc1", "kc2"), ("simple", "OK"))
check("c1 MULTI -> +OK", d1.cmd("MULTI"), ("simple", "OK"))
check("c1 INCR kc1 -> +QUEUED", d1.cmd("INCR", "kc1"), ("simple", "QUEUED"))
check("c2 SET kc9 999 -> +OK (unwatched)", d2.cmd("SET", "kc9", "999"), ("simple", "OK"))
check("c1 EXEC -> [ (int) 2 ] (ran)", d1.cmd("EXEC"), ("array", [("int", 2)]))

# D. WATCH k1 k2, nobody touches them -> EXEC runs. Baseline.
print("D. WATCH kd1 kd2 (single cmd), nothing changes -> runs")
e1 = Conn("e1")
check("c1 SET kd1 1 -> +OK", e1.cmd("SET", "kd1", "1"), ("simple", "OK"))
check("c1 SET kd2 1 -> +OK", e1.cmd("SET", "kd2", "1"), ("simple", "OK"))
check("c1 WATCH kd1 kd2 -> +OK", e1.cmd("WATCH", "kd1", "kd2"), ("simple", "OK"))
check("c1 MULTI -> +OK", e1.cmd("MULTI"), ("simple", "OK"))
check("c1 INCR kd1 -> +QUEUED", e1.cmd("INCR", "kd1"), ("simple", "QUEUED"))
check("c1 EXEC -> [ (int) 2 ] (ran)", e1.cmd("EXEC"), ("array", [("int", 2)]))

# ===================== Part 2: versions-map race stressor =====================
# Heavy concurrent contention on a tiny shared key set: half the connections
# hammer SET/INCR (which WRITE the versions map), half run WATCH+EXEC (which
# READ it). If the read side isn't under the same lock, the race detector fires
# on the server, or Go throws a fatal "concurrent map read and map write" that
# drops connections -- which shows up here as worker errors.
print("\nPart 2 -- concurrency stress (run the server with -race to catch #1)")


def stress_worker(wid, iters, keys, errors):
    try:
        conn = Conn("s%d" % wid)
        for i in range(iters):
            k = keys[(wid + i) % len(keys)]
            if wid % 2 == 0:            # writers: touch the versions map
                conn.cmd("SET", k, str(i))
                conn.cmd("INCR", k)
            else:                       # readers: WATCH + EXEC read the versions map
                conn.cmd("WATCH", k)
                conn.cmd("MULTI")
                conn.cmd("INCR", k)
                conn.cmd("EXEC")
    except Exception as ex:
        errors.append("worker %d: %r" % (wid, ex))


NWORKERS, ITERS = 8, 250
keys = ["ks0", "ks1", "ks2", "ks3"]
errors = []
threads = [threading.Thread(target=stress_worker, args=(w, ITERS, keys, errors))
           for w in range(NWORKERS)]
for t in threads:
    t.start()
for t in threads:
    t.join()
print("  ran ~%d ops across %d connections on %d shared keys" %
      (NWORKERS * ITERS * 3, NWORKERS, len(keys)))
check("stress: every connection survived (no crash / dropped conns)", errors, [])
for e in errors[:5]:
    print("        " + e)

# ================================ summary ====================================
print()
passed, total = sum(results), len(results)
print("%d/%d checks passed" % (passed, total))
print("(Part 2 only *proves* #1 is fixed when the server runs under `go run -race`.)")
sys.exit(0 if passed == total else 1)
