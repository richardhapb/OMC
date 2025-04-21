package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

type WebSocket struct {
	conn	  *net.Conn
	connected bool
}

type Message struct {
	text	  string
	timestamp int64
}

var (
	messages	  []Message
	mutex		  sync.Mutex
	wsConnections []WebSocket
)

const (
	MessageDisappearThreshold = 60000 // One minute
	MagicGUID				  = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

func main() {
	connStr := "127.0.0.1:8000"
	listener, err := net.Listen("tcp", connStr)

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
	// go removeOldMessages(&messages)
	for {
		n, err := conn.Read(buffer)

		if err != nil {
			log.Printf("error: %s\n", err)
			return
		}

		// mutex.Lock()
		// messages = append(messages, Message{text: string(buffer[:n]), timestamp: time.Now().UnixMilli()})
		// mutex.Unlock()

		handleRequest(buffer, n, conn)

		log.Printf("Received: %s\n", buffer[:n])
	}
}

func handleRequest(buffer []byte, n int, conn net.Conn) {
	request := string(buffer[:n])
	log.Printf("Request received: \n%s\n\n", request)

	lines := strings.Split(request, "\n")

	parts := strings.Split(lines[0], " ")
	method := parts[0]
	uri := parts[1]
	if method == "GET" && uri == "/ws" {
		handleWebSocket(lines, conn)
	}

	if method == "GET" && uri == "/messages" {
		go responseMessages(conn)
	}
}

func handleWebSocket(lines []string, conn net.Conn) {
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

	mutex.Lock()
	wsConnections = append(wsConnections, WebSocket{conn: &conn, connected: true})
	mutex.Unlock()

	_, err := (conn).Write([]byte(response))

	go removeOldMessages()
	wsLoop(conn)

	if err != nil {
		log.Printf("Error connecting WebSocket: %s\n", err)
	}
}

func wsLoop(conn net.Conn) {
	for {
		opcode, payload, err := readFrame(conn)
		if err != nil {
			log.Printf("Error reading payload: %s", err)
			return
		}

		if opcode == 1 {
			mutex.Lock()
			messages = append(messages, Message{
				text:	   string(payload),
				timestamp: time.Now().UnixMilli(),
			})
			mutex.Unlock()
			log.Printf("Messages: %v\n", messages)

			for _, ws := range wsConnections {
				if ws.connected {
					go writeFrame(*ws.conn, payload)
				}
			}
		}
	}
}

func readFrame(conn net.Conn) (opcode byte, payload []byte, err error) {
	header := make([]byte, 2)
	_, err = io.ReadFull(conn, header)

	if err != nil {
		log.Printf("error reading header: %s", err)
		return
	}

	// end := (header[0] & 0x80) != 0 // First byte == 0 indicates end the connection, 0x80 = 1000 0000
	opcode = header[0] & 0x0F
	masked := (header[1] & 0x80) != 0	// The second byte being 0 indicates that the content is not masked (mandatory for the client). 0x80 = 1000 0000
	payloadLen := int(header[1] & 0x7F) // Payload length can be 126, which indicates that the next two bytes represent the length. If it is 127, it indicates an extended payload (8 bytes). 0x7F = 0111 1111

	if payloadLen == 126 {
		extended := make([]byte, 2)
		_, err = io.ReadFull(conn, extended)

		if err != nil {
			return
		}
		payloadLen = int(binary.BigEndian.Uint16(extended))
	} else if payloadLen == 127 {
		extended := make([]byte, 8)
		_, err = io.ReadFull(conn, extended)

		if err != nil {
			return
		}
		payloadLen = int(binary.BigEndian.Uint64(extended))
	}

	var maskBytes []byte
	if masked {
		maskBytes = make([]byte, 4)
		_, err = io.ReadFull(conn, maskBytes)

		if err != nil {
			return
		}
	}

	payload = make([]byte, payloadLen)
	_, err = io.ReadFull(conn, payload)

	if err != nil {
		return
	}

	if masked {
		for i := range payloadLen {
			payload[i] ^= maskBytes[i%4]
		}
	}

	return
}

func writeFrame(conn net.Conn, payload []byte) error {
	frame := make([]byte, 2+len(payload))
	frame[0] = 0x81
	frame[1] = byte(len(payload))
	copy(frame[2:], payload)

	_, err := conn.Write(frame)
	return err
}

func responseMessages(conn net.Conn) {

	type messagesResponse struct {
		Messages []Message `json:"messages"`
	}

	body, err := json.Marshal(messagesResponse{messages})

	if err != nil {
		log.Printf("error marshaling JSON: %s", err)
		return
	}

	header := "HTTP/1.1 200 OK\r\n"
	header += "Content-Type: application/json\r\n"
	header += "access-control-allow-origin: *\r\n"

	header += fmt.Sprintf("Content-Length: %d", len(body))
	header += "\r\n\r\n"

	log.Printf("Response to messages: \n%s\n", header)
	log.Println(string(body))

	conn.Write([]byte(header))
	conn.Write(body)
}

func calculateAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + MagicGUID))

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func removeOldMessages() {
	for {
		mutex.Lock()
		for i := len(messages) - 1; i >= 0; i-- {
			now := time.Now().UnixMilli()
			if now-(messages)[i].timestamp >= MessageDisappearThreshold {
				log.Printf("Removing message... %s\n", messages[i].text)
				messages = slices.Delete(messages, i, i+1)
			}

		}
		mutex.Unlock()
		time.Sleep(time.Second)
	}

}
