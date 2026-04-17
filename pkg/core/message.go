package core

import (
	"encoding/json"
	"time"
)

type MessageStatus string

const (
	StatusWaiting  MessageStatus = "waiting"
	StatusReserved MessageStatus = "reserved"
	StatusDelayed  MessageStatus = "delayed"
	StatusTimeout  MessageStatus = "timeout"
	StatusFailed   MessageStatus = "failed"
	StatusDone     MessageStatus = "done"
)

type Message struct {
	ID          string          `json:"id"`
	Payload     json.RawMessage `json:"payload"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	Status      MessageStatus   `json:"status,omitempty"`
	CreatedAt   int64           `json:"created_at,omitempty"`
	UpdatedAt   int64           `json:"updated_at,omitempty"`
}

func NewMessage(payload []byte, maxAttempts int) *Message {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	now := time.Now().Unix()
	return &Message{
		ID:          "",
		Payload:     payload,
		MaxAttempts: maxAttempts,
		Status:      StatusWaiting,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func (m *Message) Encode() (string, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func DecodeMessage(data string) (*Message, error) {
	var m Message
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return nil, err
	}
	if m.MaxAttempts <= 0 {
		m.MaxAttempts = 1
	}
	if m.Status == "" {
		m.Status = StatusWaiting
	}
	if m.CreatedAt <= 0 {
		m.CreatedAt = time.Now().Unix()
	}
	m.UpdatedAt = time.Now().Unix()
	return &m, nil
}

func (m *Message) AttemptsAllowed() bool {
	if m.MaxAttempts > m.Attempts {
		m.Attempts++
		return true
	}
	m.Attempts++
	return false
}

func (m *Message) SetStatus(status MessageStatus) {
	if m == nil {
		return
	}
	now := time.Now().Unix()
	if m.CreatedAt <= 0 {
		m.CreatedAt = now
	}
	m.Status = status
	m.UpdatedAt = now
}
