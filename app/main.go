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
	mu       sync.Mutex
	data     map[string]entry
	versions map[string]uint64
}

func (db *SafeDB) SET(key string, value string, expiry time.Time) {

	db.mu.Lock()
	db.setLocked(key, value, expiry)
	db.mu.Unlock()

}

func (db *SafeDB) setLocked(key string, value string, expiry time.Time) {

	db.data[key] = entry{value: value, expiresAt: expiry}
	db.versions[key]++

}

func (db *SafeDB) GET(key string) (string, bool) {

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.getLocked(key)

}

func (db *SafeDB) getLocked(key string) (string, bool) {

	e, ok := db.data[key]
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		delete(db.data, key)
		db.versions[key]++
		ok = false
	}

	return e.value, ok

}

func (db *SafeDB) WATCH(keys []string) map[string]uint64 {

	db.mu.Lock()
	defer db.mu.Unlock()

	versions := make(map[string]uint64)
	for _, key := range keys {
		versions[key] = db.versions[key]
	}

	return versions

}

// currently diverges slightly from real redis behavior
// i.e. "+5" incriments to 6, "007" incriments to 8, int64 max overflows,
// but real redis would return an error in these cases
func (db *SafeDB) INCR(key string) (int, bool) {

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.incrLocked(key)

}

func (db *SafeDB) incrLocked(key string) (int, bool) {

	e, ok := db.data[key]
	if !ok {
		db.data[key] = entry{value: "1", expiresAt: time.Time{}}
		db.versions[key]++
		return 1, true
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		db.data[key] = entry{value: "1", expiresAt: time.Time{}}
		db.versions[key]++
		return 1, true
	}

	num, err := strconv.Atoi(e.value)
	if err != nil {
		return 0, false
	}

	num++
	db.data[key] = entry{value: strconv.Itoa(num), expiresAt: e.expiresAt}
	db.versions[key]++
	return num, ok
}

func (db *SafeDB) EXEC_TRANSACTION(queue [][]string, versions map[string]uint64) ([]byte, bool) {

	db.mu.Lock()
	defer db.mu.Unlock()

	aborted := false
	for key, version := range versions {
		if db.versions[key] != version {
			aborted = true
			break
		}
	}

	if aborted {
		return nullArray(), false
	}

	responses := []byte(arrayReply(queue))
	for _, cmd := range queue {

		resp := db.executeLocked(cmd)
		responses = append(responses, resp...)
	}

	return responses, true
}

func (db *SafeDB) executeLocked(args []string) []byte {

	if len(args) == 0 {
		return errorReply("ERR no command provided")
	}

	switch strings.ToUpper(args[0]) {

	case "GET":
		if len(args) < 2 {
			return errorReply("ERR wrong number of arguments for 'GET' command")
		}
		val, ok := db.getLocked(args[1])
		if !ok {
			return nullBulk()
		} else {
			return bulkString(val)
		}

	case "SET":
		if len(args) < 3 {
			return errorReply("ERR wrong number of arguments for 'SET' command")
		}
		if len(args) >= 5 && strings.ToUpper(args[3]) == "PX" {
			expiryMillis, err := strconv.Atoi(args[4])
			if err != nil {
				return errorReply("ERR invalid PX value")
			}
			expiryTime := time.Now().Add(time.Duration(expiryMillis) * time.Millisecond)
			db.setLocked(args[1], args[2], expiryTime)
			return simpleString("OK")
		} else {
			db.setLocked(args[1], args[2], time.Time{})
			return simpleString("OK")
		}

	case "INCR":
		if len(args) < 2 {
			return errorReply("ERR wrong number of arguments for 'INCR' command")
		}
		num, ok := db.incrLocked(args[1])
		if !ok {
			return errorReply("ERR value is not an integer or out of range")
		} else {
			return integerReply(num)
		}

	case "ECHO":
		if len(args) < 2 {
			return errorReply("ERR wrong number of arguments for 'ECHO' command")
		}
		return bulkString(args[1])

	case "PING":
		return simpleString("PONG")

	case "COMMAND":
		return []byte("*0\r\n")

	default:
		return errorReply("ERR unknown command '" + args[0] + "'")
	}

}

func (db *SafeDB) EXECUTE_COMMAND(args []string) []byte {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.executeLocked(args)
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

func bulkString(s string) []byte {
	return []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(s), s))
}

func simpleString(s string) []byte {
	return []byte(fmt.Sprintf("+%s\r\n", s))
}

func integerReply(i int) []byte {
	return []byte(fmt.Sprintf(":%d\r\n", i))
}

func nullBulk() []byte {
	return []byte("$-1\r\n")
}

func nullArray() []byte {
	return []byte("*-1\r\n")
}

func arrayReply(queue [][]string) []byte {
	return []byte(fmt.Sprintf("*%d\r\n", len(queue)))
}

func errorReply(s string) []byte {
	return []byte(fmt.Sprintf("-%s\r\n", s))
}

func writeResponse(conn net.Conn, data []byte) bool {
	_, err := conn.Write(data)
	if err != nil {
		fmt.Println("Error writing response: ", err.Error())
		return false
	}
	return true
}

func handleConnection(conn net.Conn, database *SafeDB) {

	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic: ", r)
		}
	}()

	var queue [][]string
	var versions map[string]uint64 = make(map[string]uint64)
	var multi bool = false
	reader := bufio.NewReader(conn)

	for {

		args, err := readCommand(reader)
		if err != nil {
			if err != io.EOF {
				fmt.Println("Error reading command: ", err.Error())
			}
			break
		}

		//fmt.Println("received:", args)

		if len(args) == 0 {
			continue
		}

		switch strings.ToUpper(args[0]) {

		case "MULTI":
			if multi {
				writeResponse(conn, errorReply("ERR MULTI calls can not be nested"))
				continue
			}
			multi = true
			queue = [][]string{}
			writeResponse(conn, simpleString("OK"))
			continue

		case "EXEC":
			if !multi {
				writeResponse(conn, errorReply("ERR EXEC without MULTI"))
				continue
			}

			resp, _ := database.EXEC_TRANSACTION(queue, versions)
			writeResponse(conn, resp)
			multi = false
			queue = [][]string{}
			versions = make(map[string]uint64)
			continue

		case "DISCARD":
			if !multi {
				writeResponse(conn, errorReply("ERR DISCARD without MULTI"))
				continue
			}
			multi = false
			queue = [][]string{}
			versions = make(map[string]uint64)
			writeResponse(conn, simpleString("OK"))
			continue

		case "WATCH":
			if len(args) < 2 {
				writeResponse(conn, errorReply("ERR wrong number of arguments for 'WATCH' command"))
				continue
			}
			if multi {
				writeResponse(conn, errorReply("ERR WATCH inside MULTI is not allowed"))
				continue
			}

			for k, v := range database.WATCH(args[1:]) {
				if _, ok := versions[k]; !ok {
					versions[k] = v
				}
			}

			writeResponse(conn, simpleString("OK"))
			continue

		case "UNWATCH":
			if multi {
				writeResponse(conn, errorReply("ERR UNWATCH inside MULTI is not allowed"))
				continue
			}
			versions = make(map[string]uint64)
			writeResponse(conn, simpleString("OK"))
			continue

		default:
			if multi {
				queue = append(queue, args)
				writeResponse(conn, simpleString("QUEUED"))
				continue
			}
			writeResponse(conn, database.EXECUTE_COMMAND(args))
			continue

		}

	}
}

func main() {

	database := &SafeDB{
		data:     make(map[string]entry),
		versions: make(map[string]uint64),
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
