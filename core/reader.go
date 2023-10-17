package core

import (
	"github.com/liuhengloveyou/livego/asyncwriter"
	"github.com/liuhengloveyou/livego/proto"
	"github.com/liuhengloveyou/livego/stream"
)

// reader is an entity that can read a stream.
type reader interface {
	close()
	apiReaderDescribe() proto.ApiPathSourceOrReader
}

func readerMediaInfo(r *asyncwriter.Writer, stream *stream.Stream) string {
	return mediaInfo(stream.MediasForReader(r))
}
