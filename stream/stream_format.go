package stream

import (
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/pion/rtp"

	"github.com/liuhengloveyou/livego/asyncwriter"
	"github.com/liuhengloveyou/livego/formatprocessor"
	"github.com/liuhengloveyou/livego/log"
	"github.com/liuhengloveyou/livego/unit"
)

func unitSize(u unit.Unit) uint64 {
	n := uint64(0)
	for _, pkt := range u.GetRTPPackets() {
		n += uint64(pkt.MarshalSize())
	}
	return n
}

type streamFormat struct {
	proc    formatprocessor.Processor
	readers map[*asyncwriter.Writer]readerFunc
}

func newStreamFormat(
	udpMaxPayloadSize int,
	forma format.Format,
	generateRTPPackets bool,
) (*streamFormat, error) {
	proc, err := formatprocessor.New(udpMaxPayloadSize, forma, generateRTPPackets)
	if err != nil {
		return nil, err
	}

	sf := &streamFormat{
		proc:    proc,
		readers: make(map[*asyncwriter.Writer]readerFunc),
	}

	return sf, nil
}

func (sf *streamFormat) addReader(r *asyncwriter.Writer, cb readerFunc) {
	sf.readers[r] = cb
}

func (sf *streamFormat) removeReader(r *asyncwriter.Writer) {
	delete(sf.readers, r)
}

func (sf *streamFormat) writeUnit(s *Stream, medi *description.Media, u unit.Unit) {
	hasNonRTSPReaders := len(sf.readers) > 0

	err := sf.proc.Process(u, hasNonRTSPReaders)
	if err != nil {
		log.Logger.Error(err.Error())
		return
	}

	atomic.AddUint64(s.bytesReceived, unitSize(u))

	if s.rtspStream != nil {
		for _, pkt := range u.GetRTPPackets() {
			s.rtspStream.WritePacketRTPWithNTP(medi, pkt, u.GetNTP()) //nolint:errcheck
		}
	}

	if s.rtspsStream != nil {
		for _, pkt := range u.GetRTPPackets() {
			s.rtspsStream.WritePacketRTPWithNTP(medi, pkt, u.GetNTP()) //nolint:errcheck
		}
	}

	for writer, cb := range sf.readers {
		ccb := cb
		writer.Push(func() error {
			return ccb(u)
		})
	}
}

func (sf *streamFormat) writeRTPPacket(
	s *Stream,
	medi *description.Media,
	pkt *rtp.Packet,
	ntp time.Time,
	pts time.Duration,
) {
	sf.writeUnit(s, medi, sf.proc.UnitForRTPPacket(pkt, ntp, pts))
}
