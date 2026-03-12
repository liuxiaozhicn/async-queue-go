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
