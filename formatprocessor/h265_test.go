package formatprocessor

import (
	"bytes"
	"testing"

	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/mediacommon/pkg/codecs/h265"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"

	"github.com/liuhengloveyou/livego/unit"
)

func TestH265DynamicParams(t *testing.T) {
	forma := &format.H265{
		PayloadTyp: 96,
	}

	p, err := New(1472, forma, false)
	require.NoError(t, err)

	enc, err := forma.CreateEncoder()
	require.NoError(t, err)

	pkts, err := enc.Encode([][]byte{{byte(h265.NALUType_CRA_NUT) << 1, 0}})
	require.NoError(t, err)

	data := &unit.H265{
		Base: unit.Base{
			RTPPackets: []*rtp.Packet{pkts[0]},
		},
	}
	err = p.Process(data, true)
	require.NoError(t, err)

	require.Equal(t, [][]byte{
		{byte(h265.NALUType_CRA_NUT) << 1, 0},
	}, data.AU)

	pkts, err = enc.Encode([][]byte{{byte(h265.NALUType_VPS_NUT) << 1, 1, 2, 3}})
	require.NoError(t, err)

	err = p.Process(&unit.H265{
		Base: unit.Base{
			RTPPackets: []*rtp.Packet{pkts[0]},
		},
	}, false)
	require.NoError(t, err)

	pkts, err = enc.Encode([][]byte{{byte(h265.NALUType_SPS_NUT) << 1, 4, 5, 6}})
	require.NoError(t, err)

	err = p.Process(&unit.H265{
		Base: unit.Base{
			RTPPackets: []*rtp.Packet{pkts[0]},
		},
	}, false)
	require.NoError(t, err)

	pkts, err = enc.Encode([][]byte{{byte(h265.NALUType_PPS_NUT) << 1, 7, 8, 9}})
	require.NoError(t, err)

	err = p.Process(&unit.H265{
		Base: unit.Base{
			RTPPackets: []*rtp.Packet{pkts[0]},
		},
	}, false)
	require.NoError(t, err)

	require.Equal(t, []byte{byte(h265.NALUType_VPS_NUT) << 1, 1, 2, 3}, forma.VPS)
	require.Equal(t, []byte{byte(h265.NALUType_SPS_NUT) << 1, 4, 5, 6}, forma.SPS)
	require.Equal(t, []byte{byte(h265.NALUType_PPS_NUT) << 1, 7, 8, 9}, forma.PPS)

	pkts, err = enc.Encode([][]byte{{byte(h265.NALUType_CRA_NUT) << 1, 0}})
	require.NoError(t, err)

	data = &unit.H265{
		Base: unit.Base{
			RTPPackets: []*rtp.Packet{pkts[0]},
		},
	}
	err = p.Process(data, true)
	require.NoError(t, err)

	require.Equal(t, [][]byte{
		{byte(h265.NALUType_VPS_NUT) << 1, 1, 2, 3},
		{byte(h265.NALUType_SPS_NUT) << 1, 4, 5, 6},
		{byte(h265.NALUType_PPS_NUT) << 1, 7, 8, 9},
		{byte(h265.NALUType_CRA_NUT) << 1, 0},
	}, data.AU)
}

func TestH265OversizedPackets(t *testing.T) {
	forma := &format.H265{
		PayloadTyp: 96,
		VPS:        []byte{byte(h265.NALUType_VPS_NUT) << 1, 10, 11, 12},
		SPS:        []byte{byte(h265.NALUType_SPS_NUT) << 1, 13, 14, 15},
		PPS:        []byte{byte(h265.NALUType_PPS_NUT) << 1, 16, 17, 18},
	}

	p, err := New(1472, forma, false)
	require.NoError(t, err)

	var out []*rtp.Packet

	for _, pkt := range []*rtp.Packet{
		{
			Header: rtp.Header{
				Version:        2,
				Marker:         true,
				PayloadType:    96,
				SequenceNumber: 123,
				Timestamp:      45343,
				SSRC:           563423,
				Padding:        true,
			},
			Payload: []byte{0x01, 0x02, 0x03, 0x04},
		},
		{
			Header: rtp.Header{
				Version:        2,
				Marker:         true,
				PayloadType:    96,
				SequenceNumber: 124,
				Timestamp:      45343,
				SSRC:           563423,
				Padding:        true,
			},
			Payload: bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 2000/4),
		},
	} {
		data := &unit.H265{
			Base: unit.Base{
				RTPPackets: []*rtp.Packet{pkt},
			},
		}
		err = p.Process(data, false)
		require.NoError(t, err)

		out = append(out, data.RTPPackets...)
	}

	require.Equal(t, []*rtp.Packet{
		{
			Header: rtp.Header{
				Version:        2,
				Marker:         true,
				PayloadType:    96,
				SequenceNumber: 123,
				Timestamp:      45343,
				SSRC:           563423,
			},
			Payload: []byte{0x01, 0x02, 0x03, 0x04},
		},
		{
			Header: rtp.Header{
				Version:        2,
				Marker:         false,
				PayloadType:    96,
				SequenceNumber: 124,
				Timestamp:      45343,
				SSRC:           563423,
			},
			Payload: append(
				append([]byte{0x63, 0x02, 0x80, 0x03, 0x04}, bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 363)...),
				[]byte{0x01, 0x02, 0x03}...,
			),
		},
		{
			Header: rtp.Header{
				Version:        2,
				Marker:         true,
				PayloadType:    96,
				SequenceNumber: 125,
				Timestamp:      45343,
				SSRC:           563423,
			},
			Payload: append(
				[]byte{0x63, 0x02, 0x40, 0x04},
				bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 135)...,
			),
		},
	}, out)
}

func TestH265EmptyPacket(t *testing.T) {
	forma := &format.H265{
		PayloadTyp: 96,
	}

	p, err := New(1472, forma, true)
	require.NoError(t, err)

	unit := &unit.H265{
		AU: [][]byte{
			{byte(h265.NALUType_VPS_NUT) << 1, 10, 11, 12}, // VPS
			{byte(h265.NALUType_SPS_NUT) << 1, 13, 14, 15}, // SPS
			{byte(h265.NALUType_PPS_NUT) << 1, 16, 17, 18}, // PPS
		},
	}

	err = p.Process(unit, false)
	require.NoError(t, err)

	// if all NALUs have been removed, no RTP packets must be generated.
	require.Equal(t, []*rtp.Packet(nil), unit.RTPPackets)
}