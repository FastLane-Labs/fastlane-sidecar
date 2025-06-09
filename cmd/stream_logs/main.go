package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/gorilla/websocket"
)

type JsonRpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type JsonRpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

func main() {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	wsURL := "ws://localhost:8080/"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Fatalf("dial error: %v", err)
	}
	defer conn.Close()

	fmt.Println("Connected to sidecar WebSocket")

	// Send a JSON-RPC request to stream logs
	req := JsonRpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "streamLogs",
		Params:  []any{"test-filter"},
	}

	if err := conn.WriteJSON(req); err != nil {
		log.Fatalf("write error: %v", err)
	}

	// Start reading incoming messages
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("read error: %v", err)
				return
			}

			var notif JsonRpcNotification
			if err := json.Unmarshal(message, &notif); err == nil && notif.Method == "logUpdate" {
				fmt.Printf("Received log update: %v\n", notif.Params)
			} else {
				fmt.Printf("Received message: %s\n", message)
			}
		}
	}()

	// Keep running until Ctrl+C
	<-interrupt
	fmt.Println("\nInterrupted, closing connection")
}
