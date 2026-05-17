package services

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp     string `json:"timestamp"`
	CorrelationID string `json:"correlation-id"`
	Service       string `json:"service"`
	Message       string `json:"message"`
	Level         string `json:"level"`
}

var (
	subscribers = make(map[chan LogEntry]bool)
	subMu       sync.Mutex
)

// Subscribe adds a new subscriber channel
func Subscribe() chan LogEntry {
	subMu.Lock()
	defer subMu.Unlock()
	ch := make(chan LogEntry, 100)
	subscribers[ch] = true
	return ch
}

// Unsubscribe removes a subscriber channel
func Unsubscribe(ch chan LogEntry) {
	subMu.Lock()
	defer subMu.Unlock()
	delete(subscribers, ch)
	close(ch)
}

func LogJSON(correlationID, service, message, level string) {
	entry := LogEntry{
		Timestamp:     time.Now().Format(time.RFC3339),
		CorrelationID: correlationID,
		Service:       service,
		Message:       message,
		Level:         level,
	}
	b, _ := json.Marshal(entry)
	fmt.Println(string(b))

	// Broadcast to all subscribers
	subMu.Lock()
	defer subMu.Unlock()
	for ch := range subscribers {
		select {
		case ch <- entry:
		default:
			// If buffer is full, skip this subscriber to avoid blocking
		}
	}
}
