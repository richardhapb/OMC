package main

import (
	"log"
	"net"
	"slices"
	"sync"
	"time"
)

type Message struct {
	text	  string
	timestamp int64
}

var messages []Message
var mutex sync.Mutex

const MessageDisappearThreshold = 60000 // One minute

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

		mutex.Lock()
		messages = append(messages, Message{text: string(buffer[:n]), timestamp: time.Now().UnixMilli()})
		mutex.Unlock()

		log.Printf("Received: %s\n", buffer[:n])
	}
}

func removeOldMessages(messages *[]Message) {
	for {
		mutex.Lock()
		for i := len(*messages) - 1; i >= 0; i-- {
			now := time.Now().UnixMilli()
			if now-(*messages)[i].timestamp >= MessageDisappearThreshold {
				log.Printf("Removing message... %s\n", (*messages)[i].text)
				*messages = slices.Delete(*messages, i, i + 1)
			}

		}
		mutex.Unlock()
		time.Sleep(time.Second)
	}

}
