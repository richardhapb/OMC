package main

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

type Message struct {
	text	  string
	timestamp int64
}

var (
	messages []Message
	mutex	 sync.Mutex
)

const (
	MessageDisappearThreshold = 60000 // One minute
	MagicGUID				  = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

func main() {
	connStr := "127.0.0.1:8000"
	listener, err := net.Listen("tcp", connStr)

	mutex = sync.Mutex{}

	if err != nil {
		log.Printf("error establishing connection: %s\n", err)
	}

	defer listener.Close()

	log.Printf("Listening connections on %s\n", connStr)

	for {
		conn, err := listener.Accept()

		if err != nil {
			log.Printf("error listening connection: %s\n", err)
			continue
		}

		go handleConnection(conn)
	}

}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, 1024)
	go removeOldMessages(&messages)
	for {
		n, err := conn.Read(buffer)

		log.Printf("Messages: %v\n", messages)

		if err != nil {
			log.Printf("error: %s\n", err)
			return
		}

		// mutex.Lock()
		// messages = append(messages, Message{text: string(buffer[:n]), timestamp: time.Now().UnixMilli()})
		// mutex.Unlock()

		handleRequest(buffer, n, &conn)

		log.Printf("Received: %s\n", buffer[:n])
	}
}

func handleRequest(buffer []byte, n int, conn *net.Conn) {
	request := string(buffer[:n])
	lines := strings.Split(request, "\n")

	parts := strings.Split(lines[0], " ")
	method := parts[0]
	uri := parts[1]
	if method == "GET" && uri == "/ws" {
		go handleWebSocket(lines, conn)
	}

	if method == "GET" && uri == "/messages" {
		go responseMessages(conn)
	}
}

func handleWebSocket(lines []string, conn *net.Conn) {
	var key string
	for _, line := range lines {
		if strings.HasPrefix(line, "Sec-WebSocket-Key:") {
			parts := strings.SplitN(line, ":", 2)
			key = strings.TrimSpace(parts[1])
			break
		}

	}

	if key == "" {
		log.Println("No WebSocket key found")
		return
	}

	acceptKey := calculateAcceptKey(key)

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"

	log.Println()
	log.Printf("Response to WS connection:\n%s\n", response)
	log.Println()

	_, err := (*conn).Write([]byte(response))

	if err != nil {
		log.Printf("Error connecting WebSocket: %s\n", err)
	}
}

func responseMessages(conn *net.Conn) {

	header := "HTTP/1.1 200 OK\r\n"
	header += "Content-Type: application/json\r\n"
	header += "access-control-allow-origin: *\r\n"

	body := "{\"messages\":["

	for _, message := range messages {
		body += fmt.Sprintf("{\"message\": \"%s\", \"timestamp\": %d},", message.text, message.timestamp)
	}

	header += fmt.Sprintf("Content-Length: %d", calculateContentLength(body))
	header += "\r\n\r\n"

	if strings.HasSuffix(body, ",") {
		body = body[:len(body)-1]
	}

	body += "]}"

	response := header + body + "\r\n\r\n"

	log.Printf("Response to messages: \n%s\n", response)

	(*conn).Write([]byte(response))
}

func calculateContentLength(body string) int {
	return len(body) + 2
}

func calculateAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + MagicGUID))

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func removeOldMessages(messages *[]Message) {
	for {
		mutex.Lock()
		for i := len(*messages) - 1; i >= 0; i-- {
			now := time.Now().UnixMilli()
			if now-(*messages)[i].timestamp >= MessageDisappearThreshold {
				log.Printf("Removing message... %s\n", (*messages)[i].text)
				*messages = slices.Delete(*messages, i, i+1)
			}

		}
		mutex.Unlock()
		time.Sleep(time.Second)
	}

}
