package asyncqueue

import (
	"context"
	"encoding/json"
	"testing"
)

// Test Job implementations

type testEmailJob struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (j *testEmailJob) GetType() string { return "testEmailJob" }
func (j *testEmailJob) Handle(_ context.Context) (Result, error) {
	return ACK, nil
}

type testFailJob struct {
	ShouldFail bool `json:"should_fail"`
}

func (j *testFailJob) GetType() string { return "testFailJob" }
func (j *testFailJob) Handle(_ context.Context) (Result, error) {
	if j.ShouldFail {
		return RETRY, nil
	}
	return ACK, nil
}

func TestJobHandle(t *testing.T) {
	t.Run("email job returns ACK", func(t *testing.T) {
		j := &testEmailJob{To: "a@b.com", Subject: "hi", Body: "body"}
		result, err := j.Handle(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != ACK {
			t.Errorf("expected ACK, got %v", result)
		}
	})

	t.Run("fail job returns RETRY when ShouldFail", func(t *testing.T) {
		j := &testFailJob{ShouldFail: true}
		result, err := j.Handle(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != RETRY {
			t.Errorf("expected RETRY, got %v", result)
		}
	})
}

func TestPush(t *testing.T) {
	t.Run("no default server returns error", func(t *testing.T) {
		SetDefaultServer(nil)
		err := Push(context.Background(), "q", nil, 0)
		if err == nil {
			t.Error("expected error when no default server")
		}
	})

	t.Run("no default server propagates error from GetQueue", func(t *testing.T) {
		SetDefaultServer(nil)
		j := &testEmailJob{To: "a@b.com", Subject: "hi", Body: "body"}
		err := Push(context.Background(), "q", j, 0)
		if err == nil {
			t.Error("expected error when no default server")
		}
	})

	t.Run("marshals job without type envelope", func(t *testing.T) {
		j := &testEmailJob{To: "a@b.com", Subject: "hi", Body: "body"}
		data, err := json.Marshal(j)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		// Payload must NOT contain a "type" field (no Message wrapper)
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if _, hasType := raw["type"]; hasType {
			t.Error("payload must not contain a 'type' envelope field")
		}
		if raw["to"] != "a@b.com" {
			t.Errorf("expected to=a@b.com, got %v", raw["to"])
		}
	})
}

func TestGetType(t *testing.T) {
	t.Run("returns declared type name", func(t *testing.T) {
		j := &testEmailJob{}
		if j.GetType() != "testEmailJob" {
			t.Errorf("expected testEmailJob, got %s", j.GetType())
		}
	})
}
