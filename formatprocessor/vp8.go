package formatprocessor //nolint:dupl

import (
	"fmt"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpvp8"
	"github.com/pion/rtp"

	"github.com/liuhengloveyou/livego/unit"
)

type formatProcessorVP8 struct {
	udpMaxPayloadSize int
	format            *format.VP8
	encoder           *rtpvp8.Encoder
	decoder           *rtpvp8.Decoder
}

func newVP8(
	udpMaxPayloadSize int,
	forma *format.VP8,
	generateRTPPackets bool,
) (*formatProcessorVP8, error) {
	t := &formatProcessorVP8{
		udpMaxPayloadSize: udpMaxPayloadSize,
		format:            forma,
	}

	if generateRTPPackets {
		err := t.createEncoder()
		if err != nil {
			return nil, err
		}
	}

	return t, nil
}

func (t *formatProcessorVP8) createEncoder() error {
	t.encoder = &rtpvp8.Encoder{
		PayloadMaxSize: t.udpMaxPayloadSize - 12,
		PayloadType:    t.format.PayloadTyp,
	}
	return t.encoder.Init()
}

func (t *formatProcessorVP8) ProcessUnit(uu unit.Unit) error { //nolint:dupl
	u := uu.(*unit.VP8)

	pkts, err := t.encoder.Encode(u.Frame)
	if err != nil {
		return err
	}

	ts := uint32(multiplyAndDivide(u.PTS, time.Duration(t.format.ClockRate()), time.Second))
	for _, pkt := range pkts {
		pkt.Timestamp += ts
	}

	u.RTPPackets = pkts

	return nil
}

func (t *formatProcessorVP8) ProcessRTPPacket( //nolint:dupl
	pkt *rtp.Packet,
	ntp time.Time,
	pts time.Duration,
	hasNonRTSPReaders bool,
) (Unit, error) {
	u := &unit.VP8{
		Base: unit.Base{
			RTPPackets: []*rtp.Packet{pkt},
			NTP:        ntp,
			PTS:        pts,
		},
	}

	// remove padding
	pkt.Header.Padding = false
	pkt.PaddingSize = 0

	if pkt.MarshalSize() > t.udpMaxPayloadSize {
		return nil, fmt.Errorf("payload size (%d) is greater than maximum allowed (%d)",
			pkt.MarshalSize(), t.udpMaxPayloadSize)
	}

	// decode from RTP
	if hasNonRTSPReaders || t.decoder != nil {
		if t.decoder == nil {
			var err error
			t.decoder, err = t.format.CreateDecoder()
			if err != nil {
				return nil, err
			}
		}

		frame, err := t.decoder.Decode(pkt)
		if err != nil {
			if err == rtpvp8.ErrNonStartingPacketAndNoPrevious || err == rtpvp8.ErrMorePacketsNeeded {
				return u, nil
			}
			return nil, err
		}

		u.Frame = frame
	}

	// route packet as is
	return u, nil
}
