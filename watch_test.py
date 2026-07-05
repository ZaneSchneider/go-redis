#!/usr/bin/env python3
"""
WATCH / optimistic-locking test for the Redis clone.

Start the server first (in another terminal):
    go run ./app/          # listens on 6379
Then run:
    python3 watch_test.py

Why a socket script (and not redis-cli)?  WATCH only means anything when a
SECOND client modifies a watched key WHILE a transaction is open on the first
client. A single redis-cli here-doc can't express that interleaving, so we
drive two live connections (c1, c2) ourselves.

Each scenario uses fresh keys and fresh connections so they don't leak state
into one another (WATCH stays armed on a connection until EXEC/DISCARD).
"""

import os
import socket
import sys

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


def check_pred(desc, got, pred, expected_desc):
    ok = pred(got)
    results.append(ok)
    print("  [%s] %s" % ("PASS" if ok else "FAIL", desc))
    if not ok:
        print("        expected: %s" % expected_desc)
        print("        got:      %r" % (got,))


# --- A. WATCH replies +OK -----------------------------------------------------
print("A. WATCH returns OK")
a1 = Conn("a1")
check("WATCH k_a -> +OK", a1.cmd("WATCH", "k_a"), ("simple", "OK"))

# --- B. WATCH inside MULTI is rejected, txn stays open ------------------------
print("B. WATCH inside MULTI is rejected (error, and the txn stays open)")
b1 = Conn("b1")
check("MULTI -> +OK", b1.cmd("MULTI"), ("simple", "OK"))
check_pred("WATCH inside MULTI -> error", b1.cmd("WATCH", "k_b"),
           lambda x: x[0] == "error", "an error reply (-...)")
check("SET k_b 1 -> +QUEUED (txn still open)", b1.cmd("SET", "k_b", "1"), ("simple", "QUEUED"))
check("DISCARD -> +OK", b1.cmd("DISCARD"), ("simple", "OK"))

# --- C. Watched key untouched -> EXEC runs normally --------------------------
print("C. Watched key untouched -> EXEC runs")
c1 = Conn("c1")
check("SET k_c 10 -> +OK", c1.cmd("SET", "k_c", "10"), ("simple", "OK"))
check("WATCH k_c -> +OK", c1.cmd("WATCH", "k_c"), ("simple", "OK"))
check("MULTI -> +OK", c1.cmd("MULTI"), ("simple", "OK"))
check("INCR k_c -> +QUEUED", c1.cmd("INCR", "k_c"), ("simple", "QUEUED"))
check("EXEC -> [ (int) 11 ]", c1.cmd("EXEC"), ("array", [("int", 11)]))

# --- D. Watched key modified by c2 -> EXEC aborts (null array) ----------------
print("D. Watched key modified by another connection -> EXEC aborts")
d1, d2 = Conn("d1"), Conn("d2")
check("c1 SET k_d 10 -> +OK", d1.cmd("SET", "k_d", "10"), ("simple", "OK"))
check("c1 WATCH k_d -> +OK", d1.cmd("WATCH", "k_d"), ("simple", "OK"))
check("c1 MULTI -> +OK", d1.cmd("MULTI"), ("simple", "OK"))
check("c1 INCR k_d -> +QUEUED", d1.cmd("INCR", "k_d"), ("simple", "QUEUED"))
check("c2 SET k_d 999 -> +OK", d2.cmd("SET", "k_d", "999"), ("simple", "OK"))
check("c1 EXEC -> null array (*-1)", d1.cmd("EXEC"), ("array", None))
check("c1 GET k_d -> 999 (the queued INCR never ran)", d1.cmd("GET", "k_d"), ("bulk", "999"))

# --- E. A DIFFERENT key changes -> watched key intact -> EXEC still runs ------
print("E. A non-watched key changes -> EXEC still runs")
e1, e2 = Conn("e1"), Conn("e2")
check("c1 SET k_e1 10 -> +OK", e1.cmd("SET", "k_e1", "10"), ("simple", "OK"))
check("c1 WATCH k_e1 -> +OK", e1.cmd("WATCH", "k_e1"), ("simple", "OK"))
check("c1 MULTI -> +OK", e1.cmd("MULTI"), ("simple", "OK"))
check("c1 INCR k_e1 -> +QUEUED", e1.cmd("INCR", "k_e1"), ("simple", "QUEUED"))
check("c2 SET k_e2 123 -> +OK (a different key)", e2.cmd("SET", "k_e2", "123"), ("simple", "OK"))
check("c1 EXEC -> [ (int) 11 ]", e1.cmd("EXEC"), ("array", [("int", 11)]))

# --- F. (bonus) Same-value rewrite of a watched key --------------------------
# Real Redis treats WATCH as "was this key touched", so writing the SAME value
# still aborts. A value-snapshot design won't catch this; a version-counter
# design will. CodeCrafters may not test it, so this is informational only.
print("F. (bonus) Same-value rewrite of a watched key")
f1, f2 = Conn("f1"), Conn("f2")
f1.cmd("SET", "k_f", "10")
f1.cmd("WATCH", "k_f")
f1.cmd("MULTI")
f1.cmd("INCR", "k_f")
f2.cmd("SET", "k_f", "10")  # same value as before
rf = f1.cmd("EXEC")
if rf == ("array", None):
    print("  [ok]   EXEC aborted -> your WATCH tracks 'touched', like real Redis")
else:
    print("  [note] EXEC ran (%r) -> value-snapshot design; still fine for CodeCrafters" % (rf,))

# --- summary -----------------------------------------------------------------
print()
passed, total = sum(results), len(results)
print("%d/%d checks passed" % (passed, total))
sys.exit(0 if passed == total else 1)
