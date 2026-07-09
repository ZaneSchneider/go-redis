package main

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func startServer(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	t.Cleanup(func() {
		_ = l.Close()
	})

	database := &SafeDB{
		data:     make(map[string]entry),
		versions: make(map[string]uint64),
	}
	go serve(l, database)

	return l.Addr().String()
}

func cmd(parts ...string) string {

	result := fmt.Sprintf("*%d\r\n", len(parts))

	for _, p := range parts {
		result += fmt.Sprintf("$%d\r\n%s\r\n", len(p), p)
	}

	return result
}

func assertReply(t *testing.T, conn net.Conn, send, want string) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err := conn.Write([]byte(send))
	if err != nil {
		t.Fatalf("Failed to send command: %v", err)
	}

	buf := make([]byte, len(want))
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf) != want {
		t.Fatalf("Unexpected response: %q", string(buf))
	}
}

func TestPing(t *testing.T) {
	addr := startServer(t)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	assertReply(t, conn, "*1\r\n$4\r\nPING\r\n", "+PONG\r\n")
}

func TestSimpleCommands(t *testing.T) {

	addr := startServer(t)

	var tests = []struct {
		name, send, want string
	}{
		{"ping", cmd("PING"), "+PONG\r\n"},
		{"echo", cmd("ECHO", "TESTING"), "$7\r\nTESTING\r\n"},
		{"foobar", cmd("FOOBAR"), "-ERR unknown command 'FOOBAR'\r\n"},
		{"get", cmd("GET"), "-ERR wrong number of arguments for 'GET' command\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatalf("Failed to connect to server: %v", err)
			}
			defer conn.Close()
			assertReply(t, conn, tt.send, tt.want)
		})
	}
}

func TestSetGet(t *testing.T) {

	addr := startServer(t)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	assertReply(t, conn, cmd("set", "foo", "bar"), "+OK\r\n")
	assertReply(t, conn, cmd("get", "foo"), "$3\r\nbar\r\n")

}
