package stream

import (
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

type streamMedia struct {
	formats map[format.Format]*streamFormat `json:"-"`

	SSRC        uint32
	NTPTime     uint64
	RTPTime     uint32
	PacketCount uint32
	OctetCount  uint32
}

func newStreamMedia(udpMaxPayloadSize int,
	medi *description.Media,
	generateRTPPackets bool,
) (*streamMedia, error) {
	sm := &streamMedia{
		formats: make(map[format.Format]*streamFormat),
	}

	for _, forma := range medi.Formats {
		var err error
		sm.formats[forma], err = newStreamFormat(udpMaxPayloadSize, forma, generateRTPPackets)
		if err != nil {
			return nil, err
		}
	}

	return sm, nil
}
