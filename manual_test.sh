#!/usr/bin/env bash
#
# Manual smoke test for the Redis clone.
# Start the server first (in another terminal):
#     go run ./app/          # listens on 6379
# Then run:
#     bash manual_test.sh
#
# Each scenario runs on its own redis-cli connection: transaction state
# (MULTI/queue) is per-connection, but the keyspace is shared across them.

RC="redis-cli -p 6379"

$RC PING >/dev/null 2>&1 || { echo "Server not reachable on 6379 — start it first."; exit 1; }

echo "=== basics ==="
$RC <<'EOF'
PING
ECHO hello
SET foo bar
GET foo
GET missing
INCR counter
INCR counter
SET notnum abc
INCR notnum
EOF

echo; echo "=== SET with PX expiry (temp -> nil after the sleep) ==="
$RC SET temp value PX 50
$RC GET temp
sleep 0.1
$RC GET temp

echo; echo "=== happy-path transaction (EXEC -> array: OK, 2, 3, \"3\") ==="
$RC <<'EOF'
MULTI
SET a 1
INCR a
INCR a
GET a
EXEC
EOF

echo; echo "=== empty transaction (EXEC -> empty array) ==="
$RC <<'EOF'
MULTI
EXEC
EOF

echo; echo "=== DISCARD abandons the queue (d stays 'original') ==="
$RC <<'EOF'
SET d original
MULTI
SET d changed
DISCARD
GET d
EOF

echo; echo "=== EXEC / DISCARD without MULTI (both error) ==="
$RC <<'EOF'
EXEC
DISCARD
EOF

echo; echo "=== nested MULTI (2nd MULTI errors, txn stays open) ==="
$RC <<'EOF'
MULTI
MULTI
SET n 42
EXEC
GET n
EOF

echo; echo "=== error inside EXEC (INCR fails, PING + GET still run) ==="
$RC <<'EOF'
SET s hello
MULTI
INCR s
PING
GET s
EXEC
EOF
