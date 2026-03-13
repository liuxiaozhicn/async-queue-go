package asyncqueue

type testJobA struct {
	Value string `json:"value"`
}

func (j *testJobA) GetType() string { return "test_queue_a" }

type testJobB struct {
	Count int `json:"count"`
}

func (j *testJobB) GetType() string { return "test_queue_b" }
