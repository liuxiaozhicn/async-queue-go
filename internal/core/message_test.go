package core

import "testing"

func TestMessageEncodeDecode(t *testing.T) {
	m := NewMessage([]byte(`{"id":1}`), 3)
	raw, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeMessage(raw)
	if err != nil {
		t.Fatal(err)
	}

	if string(decoded.Payload) != `{"id":1}` {
		t.Fatalf("unexpected payload: %s", string(decoded.Payload))
	}
	if decoded.MaxAttempts != 3 {
		t.Fatalf("unexpected max attempts: %d", decoded.MaxAttempts)
	}
}

func TestAttemptsIncrementAndBoundary(t *testing.T) {
	m := NewMessage([]byte(`{}`), 2)
	if !m.AttemptsAllowed() {
		t.Fatal("first attempt should be allowed")
	}
	if !m.AttemptsAllowed() {
		t.Fatal("second attempt should be allowed")
	}
	if m.AttemptsAllowed() {
		t.Fatal("third attempt should not be allowed")
	}
	if m.Attempts != 3 {
		t.Fatalf("unexpected attempts: %d", m.Attempts)
	}
}
