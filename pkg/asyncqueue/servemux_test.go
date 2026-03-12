package asyncqueue

import (
	"context"
)

type testJobA struct {
	Value string `json:"value"`
}

func (j *testJobA) GetType() string { return "test_queue_a" }
func (j *testJobA) Handle(ctx context.Context) (Result, error) {
	return ACK, nil
}

type testJobB struct {
	Count int `json:"count"`
}

func (j *testJobB) GetType() string { return "test_queue_b" }
func (j *testJobB) Handle(ctx context.Context) (Result, error) {
	return ACK, nil
}
