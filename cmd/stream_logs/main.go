package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/log"
	"github.com/gorilla/websocket"
)

// go run main.go --ws-url ws://localhost:8080/ --loki-push-url http://localhost:3100/loki/api/v1/push

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

type LokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

type LokiPushPayload struct {
	Streams []LokiStream `json:"streams"`
}

func sendToLoki(logMsg string, lokiURL string) error {
	payload := LokiPushPayload{
		Streams: []LokiStream{
			{
				Stream: map[string]string{
					"job": "sidecar-log-stream",
				},
				Values: [][2]string{
					{fmt.Sprintf("%d000", time.Now().UnixMicro()), logMsg},
				},
			},
		},
	}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(lokiURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to push log to Loki: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read Loki response body: %w", err)
		}
		return fmt.Errorf("Loki push failed with status code %d and body: %s", resp.StatusCode, string(body))
	}
	return nil
}

func connectAndStream(wsURL, lokiPushURL string, stop <-chan struct{}) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-stop:
			log.Info("Shutting down log stream...")
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			log.Error("WebSocket connection failed", err, "backoff", backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		log.Info("websocket connection established")
		backoff = time.Second // reset backoff on success

		// Send subscription
		req := JsonRpcRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "streamLogs",
			Params:  []interface{}{""},
		}
		if err := conn.WriteJSON(req); err != nil {
			log.Error("write error", err)
			conn.Close()
			continue
		}

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Error("WebSocket read error", err)
				conn.Close()
				break
			}

			var notif JsonRpcNotification
			if err := json.Unmarshal(message, &notif); err == nil && notif.Method == "logUpdate" {
				logLine := fmt.Sprintf("%v", notif.Params)
				log.Info("Log", logLine)
				if err := sendToLoki(logLine, lokiPushURL); err != nil {
					log.Error("failed to push log to Loki", err)
				}
			} else {
				log.Info("Received non-log message", message)
			}
		}

		log.Error("Disconnected. Attempting to reconnect...")
	}
}

func main() {
	var wsURL string
	var lokiPushURL string

	flag.StringVar(&wsURL, "ws-url", "", "WebSocket URL for log stream (e.g., ws://localhost:8080/)")
	flag.StringVar(&lokiPushURL, "loki-push-url", "", "Loki push API URL (e.g., http://localhost:3100/loki/api/v1/push)")
	flag.Parse()

	if wsURL == "" || lokiPushURL == "" {
		flag.Usage()
		os.Exit(1)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	stop := make(chan struct{})

	go connectAndStream(wsURL, lokiPushURL, stop)

	<-interrupt
	close(stop)
	log.Info("Interrupted, shutting down.")
	time.Sleep(1 * time.Second)
}
