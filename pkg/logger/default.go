package logger

import (
	"context"
	"log"
	"time"
)

// Default is the out-of-the-box logger at Info level writing to standard log.
var Default Interface = &defaultLogger{level: Info}

type defaultLogger struct {
	level LogLevel
}

func (l *defaultLogger) LogMode(level LogLevel) Interface {
	return &defaultLogger{level: level}
}

func (l *defaultLogger) Info(ctx context.Context, msg string, args ...interface{}) {
	if l.level >= Info {
		log.Printf("[async-queue] INFO  "+msg, args...)
	}
}

func (l *defaultLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
	if l.level >= Warn {
		log.Printf("[async-queue] WARN  "+msg, args...)
	}
}

func (l *defaultLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	if l.level >= Error {
		log.Printf("[async-queue] ERROR "+msg, args...)
	}
}

func (l *defaultLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, string), err error) {
	if l.level < Info {
		return
	}
	queue, disposition := fc()
	elapsed := time.Since(begin)
	if err != nil {
		log.Printf("[async-queue] TRACE queue=%s disposition=%s elapsed=%v err=%v", queue, disposition, elapsed, err)
	} else {
		log.Printf("[async-queue] TRACE queue=%s disposition=%s elapsed=%v", queue, disposition, elapsed)
	}
}
