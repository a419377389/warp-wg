package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type LogStream struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func NewLogStream() *LogStream {
	return &LogStream{clients: make(map[chan string]struct{})}
}

func (ls *LogStream) Subscribe() chan string {
	ch := make(chan string, 200)
	ls.mu.Lock()
	ls.clients[ch] = struct{}{}
	ls.mu.Unlock()
	return ch
}

func (ls *LogStream) Unsubscribe(ch chan string) {
	ls.mu.Lock()
	if _, ok := ls.clients[ch]; ok {
		delete(ls.clients, ch)
		close(ch)
	}
	ls.mu.Unlock()
}

func (ls *LogStream) Broadcast(line string) {
	ls.mu.Lock()
	for ch := range ls.clients {
		select {
		case ch <- line:
		default:
		}
	}
	ls.mu.Unlock()
}

type Logger struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	stream *LogStream
}

func NewLogger(path string, stream *LogStream) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Logger{
		file:   f,
		writer: bufio.NewWriter(f),
		stream: stream,
	}, nil
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writer != nil {
		_ = l.writer.Flush()
	}
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

func (l *Logger) Info(msg string) {
	l.write("INFO", msg)
}

func (l *Logger) Warn(msg string) {
	l.write("WARN", msg)
}

func (l *Logger) Error(msg string) {
	l.write("ERROR", msg)
}

func (l *Logger) write(level, msg string) {
	line := fmt.Sprintf("[%s] [%s] %s", time.Now().Format("2006-01-02 15:04:05"), level, msg)
	l.mu.Lock()
	if l.writer != nil {
		_, _ = l.writer.WriteString(line + "\n")
		_ = l.writer.Flush()
	}
	l.mu.Unlock()
	if l.stream != nil {
		l.stream.Broadcast(line)
	}
}
