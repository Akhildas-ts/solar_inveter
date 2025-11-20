// internal/service/batch_writer.go
// FIXED: Actually writes to inverter_data collection

package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

	// Stats
	batchesWritten uint64
	recordsWritten uint64
	lastFlushTime  time.Time
	lastFlushCount int
}

// NewBatchWriter creates a new batch writer
func NewBatchWriter(repo repository.Repository, batchSize int, flushInterval time.Duration) *BatchWriter {
	bw := &BatchWriter{
		repo:          repo,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		buffer:        make([]domain.InverterData, 0, batchSize),
		stop:          make(chan struct{}),
		lastFlushTime: time.Now(),
	}

	// Start auto-flush goroutine
	bw.wg.Add(1)
	go bw.autoFlush()

	logger.Info(fmt.Sprintf("✓ BatchWriter started: %d size, %v interval", batchSize, flushInterval))
	return bw
}

// Add adds a record to the buffer and flushes if needed
func (bw *BatchWriter) Add(record domain.InverterData) {
	bw.mu.Lock()
	bw.buffer = append(bw.buffer, record)
	shouldFlush := len(bw.buffer) >= bw.batchSize
	bw.mu.Unlock()

	if shouldFlush {
		bw.Flush()
	}
}

// Flush writes all buffered records to database (inverter_data collection)
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

	// Write to database with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTime := time.Now()
	recordCount := len(toWrite)

	// CRITICAL: Use InsertMany with unordered writes for 600 RPS
	err := bw.repo.Insert(ctx, toWrite)

	elapsed := time.Since(startTime)

	if err != nil {
		logger.Error(fmt.Sprintf("❌ Batch write FAILED: %v records in %v: %v",
			recordCount, elapsed, err))
		return
	}

	// Stats
	atomic.AddUint64(&bw.batchesWritten, 1)
	atomic.AddUint64(&bw.recordsWritten, uint64(recordCount))
	bw.lastFlushCount = recordCount
	bw.lastFlushTime = time.Now()

	// Log successful flush
	rps := float64(recordCount) / elapsed.Seconds()
	logger.Debug(fmt.Sprintf("✓ Flushed %d records in %v (%.0f/s)",
		recordCount, elapsed.Round(time.Millisecond), rps))
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
			bw.Flush() // Final flush before shutdown
			return
		}
	}
}

// Stats returns writer statistics
func (bw *BatchWriter) Stats() map[string]interface{} {
	return map[string]interface{}{
		"batches_written":  atomic.LoadUint64(&bw.batchesWritten),
		"records_written":  atomic.LoadUint64(&bw.recordsWritten),
		"buffer_size":      bw.Size(),
		"last_flush_count": bw.lastFlushCount,
		"last_flush_time":  bw.lastFlushTime.Format("15:04:05"),
	}
}

// Close stops the batch writer and flushes remaining data
func (bw *BatchWriter) Close() {
	close(bw.stop)
	bw.wg.Wait()
	logger.Info(fmt.Sprintf("✓ BatchWriter closed. Total: %d batches, %d records",
		atomic.LoadUint64(&bw.batchesWritten),
		atomic.LoadUint64(&bw.recordsWritten)))
}
