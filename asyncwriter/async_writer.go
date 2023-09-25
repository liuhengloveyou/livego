// Package asyncwriter contains an asynchronous writer.
package asyncwriter

import (
	"fmt"

	"github.com/bluenviron/gortsplib/v4/pkg/ringbuffer"
	"github.com/liuhengloveyou/livego/common"
)

// Writer is an asynchronous writer.
type Writer struct {
	buffer *ringbuffer.RingBuffer

	// out
	err chan error
}

// New allocates a Writer.
func New(queueSize int) *Writer {
	buffer, _ := ringbuffer.New(uint64(queueSize))

	return &Writer{
		buffer: buffer,
		err:    make(chan error),
	}
}

// Start starts the writer routine.
func (w *Writer) Start() {
	go w.run()
}

// Stop stops the writer routine.
func (w *Writer) Stop() {
	w.buffer.Close()
	<-w.err
}

// Error returns whenever there's an error.
func (w *Writer) Error() chan error {
	return w.err
}

func (w *Writer) run() {
	w.err <- w.runInner()
}

func (w *Writer) runInner() error {
	for {
		cb, ok := w.buffer.Pull()
		if !ok {
			return fmt.Errorf("terminated")
		}

		err := cb.(func() error)()
		if err != nil {
			// cErr, ok := err.(*common.Error)
			// if ok {
			// 	if cErr.Equal(common.ErrTooFast) {
			// 		time.Sleep(time.Millisecond * 10) // 太快了，sleep10ms
			// 	} else if cErr.Equal(common.ErrTooSlowly) { //nolint:errcheck
			// 		// 太慢了
			// 	}

			// 	continue
			// }

			return err
		}
	}
}

// Push appends an element to the queue.
func (w *Writer) Push(cb func() error) {
	ok := w.buffer.Push(cb)
	if !ok {
		common.Logger.Warn("write queue is full")
	}
}
