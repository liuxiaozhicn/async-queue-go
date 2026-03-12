package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/liuxiaozhicn/async-queue-go/internal/core"
	"github.com/liuxiaozhicn/async-queue-go/internal/queue"
	"github.com/liuxiaozhicn/async-queue-go/internal/worker"
	"github.com/redis/go-redis/v9"
)

type workerOptions struct {
	redisAddr       string
	channel         string
	timeout         int
	handleTimeout   int
	retrySeconds    []int
	concurrent      int
	maxMessages     int
	shutdownTimeout int
}

type workerRuntime interface {
	Start(context.Context) error
	Wait() error
	RunWithSignals(context.Context, time.Duration) error
}

var newWorkerDriver = func(opts workerOptions) (queue.Driver, error) {
	client := redis.NewClient(&redis.Options{Addr: opts.redisAddr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return queue.NewRedisDriver(client, opts.channel, opts.timeout, opts.handleTimeout, opts.retrySeconds), nil
}

var newWorkerConsumer = func(d queue.Driver, concurrent int, maxMessages int) worker.Consumer {
	handler := func(context.Context, *core.Message) (core.Result, error) {
		return core.ACK, nil
	}
	return queue.NewConsumer(d, handler, concurrent, maxMessages)
}

var newSignalContext = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

var newWorkerRuntime = func(d queue.Driver, concurrent int, maxMessages int) workerRuntime {
	consumer := newWorkerConsumer(d, concurrent, maxMessages)
	return worker.NewWorker(consumer, newSignalContext)
}

func parseWorkerArgs(args []string) (workerOptions, error) {
	fs := flag.NewFlagSet("aq-worker", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := workerOptions{}
	var retry string
	fs.StringVar(&opts.redisAddr, "redis-addr", "127.0.0.1:6379", "redis address")
	fs.StringVar(&opts.channel, "channel", "{queue}", "queue channel")
	fs.IntVar(&opts.timeout, "timeout", 2, "pop timeout seconds")
	fs.IntVar(&opts.handleTimeout, "handle-timeout", 10, "handle timeout seconds")
	fs.StringVar(&retry, "retry-seconds", "5", "retry seconds list, comma separated")
	fs.IntVar(&opts.concurrent, "concurrent", 10, "concurrent worker limit")
	fs.IntVar(&opts.maxMessages, "max-messages", 0, "max messages to consume")
	fs.IntVar(&opts.shutdownTimeout, "shutdown-timeout", 30, "graceful shutdown timeout seconds")

	if err := fs.Parse(args); err != nil {
		return workerOptions{}, err
	}

	parts := strings.Split(strings.TrimSpace(retry), ",")
	opts.retrySeconds = make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &v); err != nil {
			return workerOptions{}, errors.New("invalid retry-seconds")
		}
		opts.retrySeconds = append(opts.retrySeconds, v)
	}
	if len(opts.retrySeconds) == 0 {
		opts.retrySeconds = []int{5}
	}
	if opts.shutdownTimeout <= 0 {
		return workerOptions{}, errors.New("shutdown-timeout must be positive")
	}
	return opts, nil
}

func buildWorkerRuntime(args []string) (workerRuntime, workerOptions, error) {
	opts, err := parseWorkerArgs(args)
	if err != nil {
		return nil, workerOptions{}, err
	}
	driver, err := newWorkerDriver(opts)
	if err != nil {
		return nil, workerOptions{}, err
	}
	return newWorkerRuntime(driver, opts.concurrent, opts.maxMessages), opts, nil
}

func runWorker(ctx context.Context, args []string) error {
	runtime, _, err := buildWorkerRuntime(args)
	if err != nil {
		return err
	}
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	return runtime.Wait()
}

func runWorkerWithSignals(args []string) error {
	runtime, opts, err := buildWorkerRuntime(args)
	if err != nil {
		return err
	}
	return runtime.RunWithSignals(context.Background(), time.Duration(opts.shutdownTimeout)*time.Second)
}

func main() {
	if err := runWorkerWithSignals(os.Args[1:]); err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		_, _ = os.Stderr.Write(append(b, '\n'))
		os.Exit(1)
	}
}
