package core

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpav1"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpvp8"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpvp9"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"

	"github.com/liuhengloveyou/livego/asyncwriter"
	"github.com/liuhengloveyou/livego/stream"
	"github.com/liuhengloveyou/livego/unit"
)

var (
	ntpEpoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
)

type webRTCOutgoingTrack struct {
	SSRC    uint32
	LastPTS time.Duration

	sender *webrtc.RTPSender
	media  *description.Media
	format format.Format
	track  *webrtc.TrackLocalStaticRTP
	cb     func(unit.Unit) error

	// Report helpers
	octetCount  uint32
	packetCount uint32
	maxPacketTs uint32
}

func newWebRTCOutgoingTrackVideo(desc *description.Session) (*webRTCOutgoingTrack, error) {
	var av1Format *format.AV1
	videoMedia := desc.FindFormat(&av1Format)

	if videoMedia != nil {
		webRTCTrak, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeAV1,
				ClockRate: 90000,
			},
			"av1",
			webrtcStreamID,
		)
		if err != nil {
			return nil, err
		}

		encoder := &rtpav1.Encoder{
			PayloadType:    105,
			PayloadMaxSize: webrtcPayloadMaxSize,
		}
		err = encoder.Init()
		if err != nil {
			return nil, err
		}

		return &webRTCOutgoingTrack{
			SSRC:   *encoder.SSRC,
			media:  videoMedia,
			format: av1Format,
			track:  webRTCTrak,
			cb: func(u unit.Unit) error {
				tunit := u.(*unit.AV1)

				if tunit.TU == nil {
					return nil
				}

				packets, err := encoder.Encode(tunit.TU)
				if err != nil {
					return nil //nolint:nilerr
				}

				for _, pkt := range packets {
					pkt.Timestamp = tunit.RTPPackets[0].Timestamp
					webRTCTrak.WriteRTP(pkt) //nolint:errcheck
				}

				return nil
			},
		}, nil
	}

	var vp9Format *format.VP9
	videoMedia = desc.FindFormat(&vp9Format)

	if videoMedia != nil { //nolint:dupl
		webRTCTrak, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeVP9,
				ClockRate: uint32(vp9Format.ClockRate()),
			},
			"vp9",
			webrtcStreamID,
		)
		if err != nil {
			return nil, err
		}

		encoder := &rtpvp9.Encoder{
			PayloadType:    96,
			PayloadMaxSize: webrtcPayloadMaxSize,
		}
		err = encoder.Init()
		if err != nil {
			return nil, err
		}

		return &webRTCOutgoingTrack{
			SSRC:   *encoder.SSRC,
			media:  videoMedia,
			format: vp9Format,
			track:  webRTCTrak,
			cb: func(u unit.Unit) error {
				tunit := u.(*unit.VP9)

				if tunit.Frame == nil {
					return nil
				}

				packets, err := encoder.Encode(tunit.Frame)
				if err != nil {
					return nil //nolint:nilerr
				}

				for _, pkt := range packets {
					pkt.Timestamp = tunit.RTPPackets[0].Timestamp
					webRTCTrak.WriteRTP(pkt) //nolint:errcheck
				}

				return nil
			},
		}, nil
	}

	var vp8Format *format.VP8
	videoMedia = desc.FindFormat(&vp8Format)

	if videoMedia != nil { //nolint:dupl
		webRTCTrak, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeVP8,
				ClockRate: uint32(vp8Format.ClockRate()),
			},
			"vp8",
			webrtcStreamID,
		)
		if err != nil {
			return nil, err
		}

		encoder := &rtpvp8.Encoder{
			PayloadType:    96,
			PayloadMaxSize: webrtcPayloadMaxSize,
		}
		err = encoder.Init()
		if err != nil {
			return nil, err
		}

		return &webRTCOutgoingTrack{
			SSRC:   *encoder.SSRC,
			media:  videoMedia,
			format: vp8Format,
			track:  webRTCTrak,
			cb: func(u unit.Unit) error {
				tunit := u.(*unit.VP8)

				if tunit.Frame == nil {
					return nil
				}

				packets, err := encoder.Encode(tunit.Frame)
				if err != nil {
					return nil //nolint:nilerr
				}

				for _, pkt := range packets {
					pkt.Timestamp = tunit.RTPPackets[0].Timestamp
					webRTCTrak.WriteRTP(pkt) //nolint:errcheck
				}

				return nil
			},
		}, nil
	}

	var h264Format *format.H264
	videoMedia = desc.FindFormat(&h264Format)

	if videoMedia != nil {
		webRTCTrak, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeH264,
				ClockRate: uint32(h264Format.ClockRate()),
			},
			"h264",
			webrtcStreamID,
		)
		if err != nil {
			return nil, err
		}

		encoder := &rtph264.Encoder{
			PayloadType:    96,
			PayloadMaxSize: webrtcPayloadMaxSize,
		}
		err = encoder.Init()
		if err != nil {
			return nil, err
		}

		firstNALUReceived := false

		outTrack := &webRTCOutgoingTrack{
			SSRC:   *encoder.SSRC,
			media:  videoMedia,
			format: h264Format,
			track:  webRTCTrak,
		}

		outTrack.cb = func(u unit.Unit) error {
			tunit := u.(*unit.H264)

			if tunit.AU == nil {
				return nil
			}
			outTrack.LastPTS = tunit.PTS

			if !firstNALUReceived {
				firstNALUReceived = true
			} else {
				if tunit.PTS < outTrack.LastPTS {
					return fmt.Errorf("WebRTC doesn't support H264 streams with B-frames")
				}
			}

			packets, err := encoder.Encode(tunit.AU)
			if err != nil {
				return nil //nolint:nilerr
			}

			for _, pkt := range packets {
				pkt.Timestamp = tunit.RTPPackets[0].Timestamp
				webRTCTrak.WriteRTP(pkt) //nolint:errcheck

				outTrack.UpdateStats(uint32(len(pkt.Payload)))

				fmt.Println(">>>", pkt)
			}

			return nil
		}

		return outTrack, nil
	}

	return nil, nil
}

func (d *webRTCOutgoingTrack) UpdateStats(packetLen uint32) {
	atomic.AddUint32(&d.octetCount, packetLen)
	atomic.AddUint32(&d.packetCount, 1)
}

func (d *webRTCOutgoingTrack) getSRStats() (octets, packets uint32) {
	octets = atomic.LoadUint32(&d.octetCount)
	packets = atomic.LoadUint32(&d.packetCount)
	return
}

func newWebRTCOutgoingTrackAudio(desc *description.Session) (*webRTCOutgoingTrack, error) {
	var opusFormat *format.Opus
	audioMedia := desc.FindFormat(&opusFormat)

	if audioMedia != nil {
		webRTCTrak, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeOpus,
				ClockRate: uint32(opusFormat.ClockRate()),
				Channels:  2,
			},
			"opus",
			webrtcStreamID,
		)
		if err != nil {
			return nil, err
		}

		return &webRTCOutgoingTrack{
			media:  audioMedia,
			format: opusFormat,
			track:  webRTCTrak,
			cb: func(u unit.Unit) error {
				for _, pkt := range u.GetRTPPackets() {
					webRTCTrak.WriteRTP(pkt) //nolint:errcheck
				}

				return nil
			},
		}, nil
	}

	var g722Format *format.G722
	audioMedia = desc.FindFormat(&g722Format)

	if audioMedia != nil {
		webRTCTrak, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeG722,
				ClockRate: uint32(g722Format.ClockRate()),
			},
			"g722",
			webrtcStreamID,
		)
		if err != nil {
			return nil, err
		}

		return &webRTCOutgoingTrack{
			media:  audioMedia,
			format: g722Format,
			track:  webRTCTrak,
			cb: func(u unit.Unit) error {
				for _, pkt := range u.GetRTPPackets() {
					webRTCTrak.WriteRTP(pkt) //nolint:errcheck
				}

				return nil
			},
		}, nil
	}

	var g711Format *format.G711
	audioMedia = desc.FindFormat(&g711Format)

	if audioMedia != nil {
		var mtyp string
		if g711Format.MULaw {
			mtyp = webrtc.MimeTypePCMU
		} else {
			mtyp = webrtc.MimeTypePCMA
		}

		webRTCTrak, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  mtyp,
				ClockRate: uint32(g711Format.ClockRate()),
			},
			"g711",
			webrtcStreamID,
		)
		if err != nil {
			return nil, err
		}

		return &webRTCOutgoingTrack{
			media:  audioMedia,
			format: g711Format,
			track:  webRTCTrak,
			cb: func(u unit.Unit) error {
				for _, pkt := range u.GetRTPPackets() {
					webRTCTrak.WriteRTP(pkt) //nolint:errcheck
				}

				return nil
			},
		}, nil
	}

	return nil, nil
}

func (t *webRTCOutgoingTrack) start(
	stream *stream.Stream,
	writer *asyncwriter.Writer,
) {

	// for {
	// 	// Read the RTCP packets as they become available for our new remote track
	// 	rtcpPackets, _, rtcpErr := receiver.ReadRTCP()
	// 	if rtcpErr != nil {
	// 		panic(rtcpErr)
	// 	}

	// 	for _, r := range rtcpPackets {
	// 		// Print a string description of the packets
	// 		if stringer, canString := r.(fmt.Stringer); canString {
	// 			fmt.Printf("Received RTCP Packet: %v", stringer.String())
	// 		}
	// 	}
	// }

	// read incoming RTCP packets to make interceptors work
	go func() {
		for {
			pkts, _, err := t.sender.ReadRTCP()
			for _, pkt := range pkts {
				if _, ok := pkt.(*rtcp.ReceiverReport); ok {
					fmt.Println("@@@@@@@@@@@@", pkt, err)
				}

			}
		}
	}()

	stream.AddReader(writer, t.media, t.format, t.cb)
}

func (d *webRTCOutgoingTrack) CreateSenderReport() *rtcp.SenderReport {
	now := time.Now()
	nowNTP := toNtpTime(now)

	octets, packets := d.getSRStats()

	return &rtcp.SenderReport{
		SSRC:        d.SSRC,
		NTPTime:     nowNTP,
		RTPTime:     uint32(d.LastPTS),
		PacketCount: packets,
		OctetCount:  octets,
	}
}

func toNtpTime(t time.Time) uint64 {
	nsec := uint64(t.Sub(ntpEpoch))
	sec := nsec / 1e9
	nsec = (nsec - sec*1e9) << 32
	frac := nsec / 1e9
	if nsec%1e9 >= 1e9/2 {
		frac++
	}
	return sec<<32 | frac
}
