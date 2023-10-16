package record

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bluenviron/mediacommon/pkg/formats/fmp4"

	"github.com/liuhengloveyou/livego/asyncwriter"
	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/stream"
)

// OnSegmentFunc is the prototype of the function passed as runOnSegmentStart / runOnSegmentComplete
type OnSegmentFunc = func(string)

type sample struct {
	*fmp4.PartSample
	dts time.Duration
}

// Agent saves streams on disk.
type Agent struct {
	path              string
	partDuration      time.Duration
	segmentDuration   time.Duration
	stream            *stream.Stream
	onSegmentCreate   OnSegmentFunc
	onSegmentComplete OnSegmentFunc

	ctx       context.Context
	ctxCancel func()
	writer    *asyncwriter.Writer
	format    recFormat

	done chan struct{}
}

// NewAgent allocates an Agent.
func NewAgent(
	writeQueueSize int,
	path string,
	format conf.RecordFormat,
	partDuration time.Duration,
	segmentDuration time.Duration,
	pathName string,
	stream *stream.Stream,
	onSegmentCreate OnSegmentFunc,
	onSegmentComplete OnSegmentFunc,
) *Agent {
	path = strings.ReplaceAll(path, "%path", pathName)

	switch format {
	case conf.RecordFormatMPEGTS:
		path += ".ts"

	default:
		path += ".mp4"
	}

	ctx, ctxCancel := context.WithCancel(context.Background())

	a := &Agent{
		path:              path,
		partDuration:      partDuration,
		segmentDuration:   segmentDuration,
		stream:            stream,
		onSegmentCreate:   onSegmentCreate,
		onSegmentComplete: onSegmentComplete,
		ctx:               ctx,
		ctxCancel:         ctxCancel,
		done:              make(chan struct{}),
	}

	a.writer = asyncwriter.New(writeQueueSize)

	switch format {
	case conf.RecordFormatMPEGTS:
		a.format = newRecFormatMPEGTS(a)

	default:
		a.format = newRecFormatFMP4(a)
	}

	go a.run()

	return a
}

// Close closes the Agent.
func (a *Agent) Close() {
	a.ctxCancel()
	<-a.done
}

func (a *Agent) run() {
	defer close(a.done)

	a.writer.Start()

	select {
	case err := <-a.writer.Error():
		fmt.Printf(err.Error())
		a.stream.RemoveReader(a.writer)

	case <-a.ctx.Done():
		a.stream.RemoveReader(a.writer)
		a.writer.Stop()
	}

	a.format.close()
}
