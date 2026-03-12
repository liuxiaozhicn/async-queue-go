# Multi-Queue Complete Example

This example demonstrates a complete async queue system with both worker and job producer functionality in a single application.

## Features

- ✅ Complete example with worker and producer in one program
- ✅ Automatic job pushing with periodic demonstration jobs
- ✅ Graceful shutdown handling
- ✅ Real-time job processing demonstration
- ✅ Configurable via JSON file
- ✅ Push-only mode for testing

## Quick Start

### 1. Start Redis

```bash
redis-server
```

### 2. Build and Run

```bash
cd cmd/examples/multi-queue

# Build the complete example
go build -o multi-queue-example .

# Run with worker and automatic job pushing
./multi-queue-example -config=./config.json

# Or run in push-only mode (no worker)
./multi-queue-example -config=./config.json -push-only
```

### 3. What You'll See

The program will:
1. 🚀 Start the worker listening on the "order" queue
2. ✓ Push 3 sample jobs (2 immediate, 1 delayed)
3. 📝 Process jobs as they arrive
4. ⏰ Push additional jobs every 10 seconds
5. 🛑 Gracefully shutdown on Ctrl+C

## Usage Options

### Complete Mode (Default)
```bash
./multi-queue-example -config=./config.json
```
Starts worker and pushes jobs automatically.

### Push-Only Mode
```bash
./multi-queue-example -config=./config.json -push-only
```
Only pushes sample jobs without starting worker.

### Separate Worker and Producer (Legacy)
```bash
# Terminal 1: Start worker only
./worker -config=./config.json

# Terminal 2: Push jobs only
./producer -config=./config.json
```

## Code Structure

### Job Definition
```go
type CreateOrderJob struct {
    asyncqueue.BaseJob
    OrderID     int         `json:"order_id"`
    UserID      int         `json:"user_id"`
    TotalAmount float64     `json:"total_amount"`
    Items       []OrderItem `json:"items"`
}

func (j *CreateOrderJob) GetType() string { return "CreateOrderJob" }

func (j *CreateOrderJob) Handle(ctx context.Context) (asyncqueue.Result, error) {
    // Process the order
    fmt.Printf("Processing order #%d for user %d\n", j.OrderID, j.UserID)
    time.Sleep(200 * time.Millisecond) // Simulate work
    return asyncqueue.ACK, nil
}
```

### Worker Registration
```go
type InitJobs struct {
    *CreateOrderJob
}

func queueHandle(q *InitJobs) *asyncqueue.ServeMux {
    reg := asyncqueue.NewServeMux()
    reg.Register(q.CreateOrderJob.GetType(), asyncqueue.WrapJob(q.CreateOrderJob))
    return reg
}
```

### Job Pushing
```go
job := &CreateOrderJob{
    BaseJob:     asyncqueue.BaseJob{MaxAttempts: 3},
    OrderID:     1001,
    UserID:      42,
    TotalAmount: 299.99,
    Items:       []OrderItem{...},
}

// Push immediately
err := asyncqueue.Push(ctx, "order", job, 0)

// Push with 5 second delay
err := asyncqueue.Push(ctx, "order", job, 5)
```

## Configuration File

The example uses `config.json`:

```json
{
  "queues": {
    "order": {
      "redis_addr": "127.0.0.1:6379",
      "channel": "{queue:order}",
      "timeout_seconds": 2,
      "handle_timeout": 30,
      "retry_seconds": [5, 10, 30],
      "processes": 2,
      "concurrent": 10,
      "max_messages": 0,
      "shutdown_timeout": 30,
      "enabled": true,
      "auto_restart": false
    }
  }
}
```

## Testing

Run the test suite:

```bash
go test -v
go test -bench=.
```

The test suite includes:
- Job type and structure validation
- Job processing functionality
- Queue registration testing
- Benchmark tests for performance

## Features Demonstrated

1. **Complete Integration**: Worker and producer in one application
2. **Job Types**: Structured job definitions with JSON serialization
3. **Delayed Jobs**: Support for immediate and delayed job execution
4. **Graceful Shutdown**: Proper signal handling and cleanup
5. **Periodic Jobs**: Automatic job generation for demonstration
6. **Error Handling**: Comprehensive error handling and logging
7. **Testing**: Full test coverage including benchmarks
