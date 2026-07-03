package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type entry struct {
	value     string
	expiresAt time.Time
}

type SafeDB struct {
	mu   sync.Mutex
	data map[string]entry
}

func (db *SafeDB) SET(key string, value string, expiry time.Time) string {

	db.mu.Lock()
	db.data[key] = entry{value: value, expiresAt: expiry}
	db.mu.Unlock()
	return "+OK\r\n"

}

func (db *SafeDB) GET(key string) (string, bool) {

	db.mu.Lock()
	e, ok := db.data[key]
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		delete(db.data, key)
		ok = false
	}
	db.mu.Unlock()
	return e.value, ok

}

func readCommand(reader *bufio.Reader) ([]string, error) {

	res := []string{}
	x, err := reader.ReadString('\n')
	if err != nil {
		return res, err
	}
	x = strings.TrimSpace(x)
	x = strings.TrimPrefix(x, "*")
	NumberOfArgs, err := strconv.Atoi(x)
	if err != nil {
		return res, err
	}

	i := 0

	for i < NumberOfArgs {
		x, err := reader.ReadString('\n')
		if err != nil {
			return res, err
		}
		x = strings.TrimSpace(x)
		x = strings.TrimPrefix(x, "$")
		y, err := strconv.Atoi(x)
		if err != nil {
			return res, err
		}
		if y <= 0 {
			res = append(res, "")
			i++
			continue
		}
		buf := make([]byte, y)
		z, err := io.ReadFull(reader, buf)
		if err != nil {
			return res, err
		}
		reader.Discard(2) // Discard the trailing \r\n

		res = append(res, string(buf[:z]))
		i++
	}

	return res, nil

}

func handleConnection(conn net.Conn, database *SafeDB) {

	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic: ", r)
		}
	}()

	reader := bufio.NewReader(conn)

	for {
		args, err := readCommand(reader)
		if err != nil {
			fmt.Println("Error reading command: ", err.Error())
			break
		}
		fmt.Println("received:", args)

		if len(args) == 0 {
			continue
		}

		switch strings.ToUpper(args[0]) {

		case "GET":
			if len(args) < 2 {
				conn.Write([]byte("-ERR wrong number of arguments for 'GET' command\r\n"))
				continue
			}
			val, ok := database.GET(args[1])
			if !ok {
				conn.Write([]byte("$-1\r\n"))
			} else {
				conn.Write([]byte(fmt.Sprintf("$%d\r\n%s\r\n", len(val), val)))
			}

		case "SET":
			if len(args) < 3 {
				conn.Write([]byte("-ERR wrong number of arguments for 'SET' command\r\n"))
				continue
			}
			if len(args) >= 5 && strings.ToUpper(args[3]) == "PX" {
				expiryMillis, err := strconv.Atoi(args[4])
				if err != nil {
					conn.Write([]byte("-ERR invalid PX value\r\n"))
					continue
				}
				expiryTime := time.Now().Add(time.Duration(expiryMillis) * time.Millisecond)
				conn.Write([]byte(database.SET(args[1], args[2], expiryTime)))
			} else {
				conn.Write([]byte(database.SET(args[1], args[2], time.Time{})))
			}

		case "ECHO":
			if len(args) < 2 {
				conn.Write([]byte("-ERR wrong number of arguments for 'ECHO' command\r\n"))
				continue
			}
			conn.Write([]byte(fmt.Sprintf("$%d\r\n%s\r\n", len(args[1]), args[1])))

		case "PING":
			conn.Write([]byte("+PONG\r\n"))
		}

		//conn.Write([]byte("+PONG\r\n"))
	}
}

func main() {

	database := &SafeDB{
		data: make(map[string]entry),
	}

	l, err := net.Listen("tcp", "0.0.0.0:6379")
	if err != nil {
		fmt.Println("Failed to bind to port 6379")
		os.Exit(1)
	}

	for {
		// Accept a connection
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			continue
		}
		go handleConnection(conn, database)
	}

	//buf := make([]byte, 1024)

}
