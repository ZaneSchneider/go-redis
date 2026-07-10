package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
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
	assertReply(t, conn, cmd("get", "missing"), "$-1\r\n")

}

func TestIncr(t *testing.T) {

	addr := startServer(t)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	assertReply(t, conn, cmd("incr", "foo"), ":1\r\n")
	assertReply(t, conn, cmd("incr", "foo"), ":2\r\n")
	assertReply(t, conn, cmd("set", "s", "r"), "+OK\r\n")
	assertReply(t, conn, cmd("incr", "s"), "-ERR value is not an integer or out of range\r\n")
	assertReply(t, conn, cmd("incr"), "-ERR wrong number of arguments for 'INCR' command\r\n")

}

func TestExpiry(t *testing.T) {

	addr := startServer(t)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	assertReply(t, conn, cmd("set", "k", "v", "px", "50"), "+OK\r\n")
	assertReply(t, conn, cmd("get", "k"), "$1\r\nv\r\n")
	time.Sleep(100 * time.Millisecond)
	assertReply(t, conn, cmd("get", "k"), "$-1\r\n")
}

func TestExecAbort(t *testing.T) {

	addr := startServer(t)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	assertReply(t, conn, cmd("multi"), "+OK\r\n")
	assertReply(t, conn, cmd("foo"), "-ERR unknown command 'foo'\r\n")
	assertReply(t, conn, cmd("exec"), "-EXECABORT Transaction discarded because of previous errors.\r\n")

}

func TestExec(t *testing.T) {

	addr := startServer(t)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	assertReply(t, conn, cmd("multi"), "+OK\r\n")
	assertReply(t, conn, cmd("set", "foo", "bar"), "+QUEUED\r\n")
	assertReply(t, conn, cmd("get", "foo"), "+QUEUED\r\n")
	assertReply(t, conn, cmd("exec"), "*2\r\n+OK\r\n$3\r\nbar\r\n")

	assertReply(t, conn, cmd("multi"), "+OK\r\n")
	assertReply(t, conn, cmd("set", "p", "q"), "+QUEUED\r\n")
	assertReply(t, conn, cmd("get", "q"), "+QUEUED\r\n")
	assertReply(t, conn, cmd("discard"), "+OK\r\n")
	assertReply(t, conn, cmd("get", "q"), "$-1\r\n")

	assertReply(t, conn, cmd("multi"), "+OK\r\n")
	assertReply(t, conn, cmd("exec"), "*0\r\n")

	assertReply(t, conn, cmd("exec"), "-ERR EXEC without MULTI\r\n")
	assertReply(t, conn, cmd("discard"), "-ERR DISCARD without MULTI\r\n")

	assertReply(t, conn, cmd("multi"), "+OK\r\n")
	assertReply(t, conn, cmd("multi"), "-ERR MULTI calls can not be nested\r\n")
	assertReply(t, conn, cmd("discard"), "+OK\r\n")

	assertReply(t, conn, cmd("multi"), "+OK\r\n")
	assertReply(t, conn, cmd("set", "foo3", "bar3"), "+QUEUED\r\n")
	assertReply(t, conn, cmd("multi"), "-ERR MULTI calls can not be nested\r\n")
	assertReply(t, conn, cmd("exec"), "*1\r\n+OK\r\n")
	assertReply(t, conn, cmd("get", "foo3"), "$4\r\nbar3\r\n")

	assertReply(t, conn, cmd("set", "foo2", "bar2"), "+OK\r\n")
	assertReply(t, conn, cmd("multi"), "+OK\r\n")
	assertReply(t, conn, cmd("incr", "foo2"), "+QUEUED\r\n")
	assertReply(t, conn, cmd("ping"), "+QUEUED\r\n")
	assertReply(t, conn, cmd("get", "foo2"), "+QUEUED\r\n")
	assertReply(t, conn, cmd("exec"), "*3\r\n-ERR value is not an integer or out of range\r\n+PONG\r\n$4\r\nbar2\r\n")

}

func TestWatchRace(t *testing.T) {

	addr := startServer(t)

	connA, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer connA.Close()

	connB, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer connB.Close()

	assertReply(t, connA, cmd("watch", "k1", "k3"), "+OK\r\n")
	assertReply(t, connA, cmd("watch", "k2"), "+OK\r\n")
	assertReply(t, connB, cmd("set", "k3", "v2"), "+OK\r\n")
	assertReply(t, connA, cmd("multi"), "+OK\r\n")
	assertReply(t, connA, cmd("set", "k3", "v3"), "+QUEUED\r\n")
	assertReply(t, connA, cmd("exec"), "*-1\r\n")

	assertReply(t, connA, cmd("watch", "k4"), "+OK\r\n")
	assertReply(t, connA, cmd("multi"), "+OK\r\n")
	assertReply(t, connA, cmd("set", "k4", "v"), "+QUEUED\r\n")
	assertReply(t, connB, cmd("set", "k6", "v"), "+OK\r\n")
	assertReply(t, connA, cmd("exec"), "*1\r\n+OK\r\n")

}

func readReply(r *bufio.Reader) (string, error) {

	input, err := r.ReadString('\n')
	if err != nil {
		return input, err
	}

	input = input[:len(input)-2]

	switch input[0] {
	case '+':
		return input, nil
	case '-':
		return input, nil
	case ':':
		return input, nil

	case '$':
		s, _ := strconv.Atoi(input[1:])
		if s == -1 {
			return input, nil
		}
		buf := make([]byte, s+2)
		_, err := io.ReadFull(r, buf)
		if err != nil {
			return input, err
		}
		return input, nil

	case '*':
		s, _ := strconv.Atoi(input[1:])
		if s == -1 {
			return input, nil
		}

		for range s {

			_, err := readReply(r)
			if err != nil {
				return input, err
			}
		}

		return input, nil

	default:

		return input, fmt.Errorf("bad reply type %q", input)

	}

}

func stressWriter(addr string, id, iters int, keys []string) error {

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	for i := 0; i < iters; i++ {

		k := keys[(id+i)%len(keys)]

		_, err := conn.Write([]byte(cmd("set", k, "1")))
		if err != nil {
			return err
		}
		_, err = readReply(r)
		if err != nil {
			return err
		}

		_, err = conn.Write([]byte(cmd("incr", k)))
		if err != nil {
			return err
		}
		_, err = readReply(r)
		if err != nil {
			return err
		}
	}

	return nil
}

func stressReader(addr string, id, iters int, keys []string) error {

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	for i := 0; i < iters; i++ {

		k := keys[(id+i)%len(keys)]

		_, err := conn.Write([]byte(cmd("watch", k)))
		if err != nil {
			return err
		}
		_, err = readReply(r)
		if err != nil {
			return err
		}

		_, err = conn.Write([]byte(cmd("multi")))
		if err != nil {
			return err
		}
		_, err = readReply(r)
		if err != nil {
			return err
		}

		_, err = conn.Write([]byte(cmd("incr", k)))
		if err != nil {
			return err
		}
		_, err = readReply(r)
		if err != nil {
			return err
		}

		_, err = conn.Write([]byte(cmd("exec")))
		if err != nil {
			return err
		}
		_, err = readReply(r)
		if err != nil {
			return err
		}
	}

	return nil
}

func TestConcStressor(t *testing.T) {

	addr := startServer(t)

	n := 8
	keys := []string{"k1", "k2", "k3", "k4"}
	iters := 200
	errs := make([]error, n)

	var wg sync.WaitGroup
	for id := 0; id < n; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				err := stressWriter(addr, id, iters, keys)
				errs[id] = err

			} else {
				err := stressReader(addr, id, iters, keys)
				errs[id] = err

			}
		}(id)
	}
	wg.Wait()

	for id := range errs {

		if errs[id] != nil {
			t.Errorf("worker %d: %v", id, errs[id])
		}
	}

}
