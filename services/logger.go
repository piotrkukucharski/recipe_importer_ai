package services

import (
	"encoding/json"
	"fmt"
	"time"
)

type LogEntry struct {
	Timestamp     string `json:"timestamp"`
	CorrelationID string `json:"correlation-id"`
	Service       string `json:"service"`
	Message       string `json:"message"`
	Level         string `json:"level"`
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
}
