package core

import "encoding/json"

type Message struct {
	Payload     json.RawMessage `json:"payload"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
}

func NewMessage(payload []byte, maxAttempts int) *Message {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	return &Message{Payload: payload, MaxAttempts: maxAttempts}
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
