package core

import (
	"fmt"
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
	"github.com/liuhengloveyou/livego/common"
	"github.com/liuhengloveyou/livego/stream"
	"github.com/liuhengloveyou/livego/unit"
)

var (
	ntpEpoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
)

type webRTCOutgoingTrack struct {
	SSRC    uint32
	LastPTS time.Duration

	sess   *webRTCSession
	sender *webrtc.RTPSender
	media  *description.Media
	format format.Format
	track  *webrtc.TrackLocalStaticRTP
	cb     func(unit.Unit) error
}

func newWebRTCOutgoingTrackVideo(sess *webRTCSession, desc *description.Session) (*webRTCOutgoingTrack, error) {
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
			sess:   sess,
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
			sess:   sess,
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
			sess:   sess,
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
			sess:   sess,
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

			sub := time.Since(tunit.GetNTP()).Milliseconds() + outTrack.sess.AmendMs
			// fmt.Println(">>>>>>", sub, time.Since(tunit.GetNTP()).Milliseconds(), outTrack.sess.AmendMs)
			if sub < 0 {
				time.Sleep(time.Duration(0-sub) * time.Millisecond)
			}

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
			}

			return nil
		}

		return outTrack, nil
	}

	return nil, nil
}

// func (d *webRTCOutgoingTrack) UpdateStats(packetLen uint32) {
// 	atomic.AddUint32(&d.octetCount, packetLen)
// 	atomic.AddUint32(&d.packetCount, 1)
// }

// func (d *webRTCOutgoingTrack) getSRStats() (octets, packets uint32) {
// 	octets = atomic.LoadUint32(&d.octetCount)
// 	packets = atomic.LoadUint32(&d.packetCount)
// 	return
// }

func newWebRTCOutgoingTrackAudio(sess *webRTCSession, desc *description.Session) (*webRTCOutgoingTrack, error) {
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
			sess:   sess,
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

func (t *webRTCOutgoingTrack) start(stream *stream.Stream, writer *asyncwriter.Writer) {
	go func() {
		for {
			time.Sleep(time.Second)
			// s := stream.GetStream("video")

			if t.sess != nil && t.sess.Datachan != nil {
				if sendErr := t.sess.Datachan.SendText(fmt.Sprintf("%v = %v", common.NtpTime(stream.NTPTime).Time().UnixMilli(), stream.LastPts.Milliseconds())); sendErr != nil {
					fmt.Println(sendErr)
					return
				}
			}
		}
	}()

	// read incoming RTCP packets to make interceptors work
	go func() {
		for {
			pkts, _, err := t.sender.ReadRTCP()
			if err != nil {
				return
			}

			for _, pkt := range pkts {
				if _, ok := pkt.(*rtcp.ReceiverReport); ok {
				}

			}
		}
	}()

	stream.AddReader(writer, t.media, t.format, t.cb)
}
