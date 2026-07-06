package logger

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp     string      `json:"timestamp"`
	CorrelationID string      `json:"correlation-id"`
	Service       string      `json:"service"`
	Message       string      `json:"message"`
	Level         string      `json:"level"`
	Type          string      `json:"type"` // "log" or "recipe"
	Data          interface{} `json:"data,omitempty"`
}

var (
	subscribers = make(map[chan LogEntry]bool)
	subMu       sync.Mutex
	logBuffer   []LogEntry
	bufferMu    sync.RWMutex
	maxBuffer   = 2000
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
		Type:          "log",
	}
	broadcast(entry)
}

func BroadcastRecipe(correlationID string, recipe interface{}) {
	entry := LogEntry{
		Timestamp:     time.Now().Format(time.RFC3339),
		CorrelationID: correlationID,
		Type:          "recipe",
		Data:          recipe,
	}
	broadcast(entry)
}

func broadcast(entry LogEntry) {
	if entry.Type == "log" {
		b, _ := json.Marshal(entry)
		fmt.Println(string(b))
	}

	bufferMu.Lock()
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxBuffer {
		logBuffer = logBuffer[len(logBuffer)-maxBuffer:]
	}
	bufferMu.Unlock()

	subMu.Lock()
	defer subMu.Unlock()
	for ch := range subscribers {
		select {
		case ch <- entry:
		default:
		}
	}
}

func GetLogsForCorrelationID(cid string) []LogEntry {
	bufferMu.RLock()
	defer bufferMu.RUnlock()
	var entries []LogEntry
	for _, entry := range logBuffer {
		if entry.CorrelationID == cid {
			entries = append(entries, entry)
		}
	}
	return entries
}
