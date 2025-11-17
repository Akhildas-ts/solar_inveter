package service

import (
	"context"
	"sync"
	"time"

	"solar_project/internal/domain"
	"solar_project/internal/repository"
	"solar_project/pkg/logger"
)

// BatchWriter buffers and writes data in batches
type BatchWriter struct {
	repo          repository.Repository
	batchSize     int
	flushInterval time.Duration

	mu     sync.Mutex
	buffer []domain.InverterData
	stop   chan struct{}
	wg     sync.WaitGroup
}

// NewBatchWriter creates a new batch writer
func NewBatchWriter(repo repository.Repository, batchSize int, flushInterval time.Duration) *BatchWriter {
	bw := &BatchWriter{
		repo:          repo,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		buffer:        make([]domain.InverterData, 0, batchSize),
		stop:          make(chan struct{}),
	}

	// Start auto-flush goroutine
	bw.wg.Add(1)
	go bw.autoFlush()

	return bw
}

// Add adds a record to the buffer
func (bw *BatchWriter) Add(record domain.InverterData) {
	bw.mu.Lock()
	bw.buffer = append(bw.buffer, record)
	shouldFlush := len(bw.buffer) >= bw.batchSize
	bw.mu.Unlock()

	if shouldFlush {
		bw.Flush()
	}
}

// Flush writes all buffered records to database
func (bw *BatchWriter) Flush() {
	bw.mu.Lock()
	if len(bw.buffer) == 0 {
		bw.mu.Unlock()
		return
	}

	// Copy buffer and clear
	toWrite := make([]domain.InverterData, len(bw.buffer))
	copy(toWrite, bw.buffer)
	bw.buffer = bw.buffer[:0]
	bw.mu.Unlock()

	// Write to database
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := bw.repo.Insert(ctx, toWrite); err != nil {
		logger.Error("Batch write failed: " + err.Error())
	} else {
		logger.Debug("Batch written: " + string(rune(len(toWrite))) + " records")
	}
}

// Size returns current buffer size
func (bw *BatchWriter) Size() int {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return len(bw.buffer)
}

// autoFlush periodically flushes the buffer
func (bw *BatchWriter) autoFlush() {
	defer bw.wg.Done()
	ticker := time.NewTicker(bw.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bw.Flush()
		case <-bw.stop:
			bw.Flush() // Final flush
			return
		}
	}
}

// Close stops the batch writer and flushes remaining data
func (bw *BatchWriter) Close() {
	close(bw.stop)
	bw.wg.Wait()
}
