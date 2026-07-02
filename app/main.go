package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

type SafeDB struct {
	mu   sync.Mutex
	data map[string]string
}

func (db *SafeDB) SET(key string, value string) string {

	db.mu.Lock()
	db.data[key] = value
	db.mu.Unlock()
	return "+OK\r\n"

}

func (db *SafeDB) GET(key string) (string, bool) {

	db.mu.Lock()
	val, ok := db.data[key]
	db.mu.Unlock()
	return val, ok

}

func parseRESP(buf []byte) [][]string {

	s := strings.Split(string(buf), "\r\n")
	var res [][]string

	n := 1
	i := 0
	//temp := []string{}

	for i < len(s) {

		if s[i] == "" {
			break
		}

		var temp []string
		N := strings.TrimPrefix(s[i], "*")
		NumberOfArgs, err := strconv.Atoi(N)
		if err != nil {
			return res
		}

		i += 1

		for n <= NumberOfArgs {

			i += 1
			temp = append(temp, s[i])
			n++
			i += 1

		}

		res = append(res, temp)
		n = 1

	}

	return res
}

func main() {

	database := &SafeDB{
		data: make(map[string]string),
	}

	l, err := net.Listen("tcp", "0.0.0.0:6379")
	if err != nil {
		fmt.Println("Failed to bind to port 6379")
		os.Exit(1)
	}
	conn, err := l.Accept()
	if err != nil {
		fmt.Println("Error accepting connection: ", err.Error())
		os.Exit(1)
	}

	buf := make([]byte, 1024)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}

		args := parseRESP(buf[:n])
		fmt.Println("received:", args)

		if len(args) == 0 {
			continue
		}

		for _, cmd := range args {

			switch cmd[0] {

			case "GET":
				val, ok := database.GET(cmd[1])
				if !ok {
					conn.Write([]byte("$-1\r\n"))
				} else {
					conn.Write([]byte(fmt.Sprintf("$%d\r\n%s\r\n", len(val), val)))
				}

			case "SET":
				conn.Write([]byte(database.SET(cmd[1], cmd[2])))

			case "PING":
				conn.Write([]byte("+PONG\r\n"))
			}

		}

		//conn.Write([]byte("+PONG\r\n"))
	}

}
