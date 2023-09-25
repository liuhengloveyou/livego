package core

import (
	"context"
	"fmt"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/mediacommon/pkg/formats/mpegts"
	srt "github.com/datarhei/gosrt"

	"github.com/liuhengloveyou/livego/common"
	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/stream"
	"github.com/liuhengloveyou/livego/unit"
)

type srtSourceParent interface {
	SetReady(req PathSourceStaticSetReadyReq) PathSourceStaticSetReadyRes
	SetNotReady(req PathSourceStaticSetNotReadyReq)
}

type srtSource struct {
	readTimeout conf.StringDuration
	parent      srtSourceParent
}

func newSRTSource(
	readTimeout conf.StringDuration,
	parent srtSourceParent,
) *srtSource {
	s := &srtSource{
		readTimeout: readTimeout,
		parent:      parent,
	}

	return s
}

// run implements sourceStaticImpl.
func (s *srtSource) Run(ctx context.Context, cnf *conf.PathConf, reloadConf chan *conf.PathConf) error {
	common.Logger.Debug("connecting")

	conf := srt.DefaultConfig()
	address, err := conf.UnmarshalURL(cnf.Source)
	if err != nil {
		return err
	}

	err = conf.Validate()
	if err != nil {
		return err
	}

	sconn, err := srt.Dial("srt", address, conf)
	if err != nil {
		return err
	}

	readDone := make(chan error)
	go func() {
		readDone <- s.runReader(sconn)
	}()

	for {
		select {
		case err := <-readDone:
			sconn.Close()
			return err

		case <-reloadConf:

		case <-ctx.Done():
			sconn.Close()
			<-readDone
			return nil
		}
	}
}

func (s *srtSource) runReader(sconn srt.Conn) error {
	sconn.SetReadDeadline(time.Now().Add(time.Duration(s.readTimeout)))
	r, err := mpegts.NewReader(mpegts.NewBufferedReader(sconn))
	if err != nil {
		return err
	}

	r.OnDecodeError(func(err error) {
		common.Logger.Warn(err.Error())
	})

	var medias []*description.Media //nolint:prealloc
	var stream *stream.Stream

	var td *mpegts.TimeDecoder
	decodeTime := func(t int64) time.Duration {
		if td == nil {
			td = mpegts.NewTimeDecoder(t)
		}
		return td.Decode(t)
	}

	for _, track := range r.Tracks() { //nolint:dupl
		var medi *description.Media

		switch tcodec := track.Codec.(type) {
		case *mpegts.CodecH264:
			medi = &description.Media{
				Type: description.MediaTypeVideo,
				Formats: []format.Format{&format.H264{
					PayloadTyp:        96,
					PacketizationMode: 1,
				}},
			}

			r.OnDataH26x(track, func(pts int64, _ int64, au [][]byte) error {
				stream.WriteUnit(medi, medi.Formats[0], &unit.H264{
					Base: unit.Base{
						NTP: time.Now(),
						PTS: decodeTime(pts),
					},
					AU: au,
				})
				return nil
			})

		case *mpegts.CodecH265:
			medi = &description.Media{
				Type: description.MediaTypeVideo,
				Formats: []format.Format{&format.H265{
					PayloadTyp: 96,
				}},
			}

			r.OnDataH26x(track, func(pts int64, _ int64, au [][]byte) error {
				stream.WriteUnit(medi, medi.Formats[0], &unit.H265{
					Base: unit.Base{
						NTP: time.Now(),
						PTS: decodeTime(pts),
					},
					AU: au,
				})
				return nil
			})

		case *mpegts.CodecMPEG4Audio:
			medi = &description.Media{
				Type: description.MediaTypeAudio,
				Formats: []format.Format{&format.MPEG4Audio{
					PayloadTyp:       96,
					SizeLength:       13,
					IndexLength:      3,
					IndexDeltaLength: 3,
					Config:           &tcodec.Config,
				}},
			}

			r.OnDataMPEG4Audio(track, func(pts int64, aus [][]byte) error {
				stream.WriteUnit(medi, medi.Formats[0], &unit.MPEG4AudioGeneric{
					Base: unit.Base{
						NTP: time.Now(),
						PTS: decodeTime(pts),
					},
					AUs: aus,
				})
				return nil
			})

		case *mpegts.CodecOpus:
			medi = &description.Media{
				Type: description.MediaTypeAudio,
				Formats: []format.Format{&format.Opus{
					PayloadTyp: 96,
					IsStereo:   (tcodec.ChannelCount == 2),
				}},
			}

			r.OnDataOpus(track, func(pts int64, packets [][]byte) error {
				stream.WriteUnit(medi, medi.Formats[0], &unit.Opus{
					Base: unit.Base{
						NTP: time.Now(),
						PTS: decodeTime(pts),
					},
					Packets: packets,
				})
				return nil
			})

		case *mpegts.CodecMPEG1Audio:
			medi = &description.Media{
				Type:    description.MediaTypeAudio,
				Formats: []format.Format{&format.MPEG1Audio{}},
			}

			r.OnDataMPEG1Audio(track, func(pts int64, frames [][]byte) error {
				stream.WriteUnit(medi, medi.Formats[0], &unit.MPEG1Audio{
					Base: unit.Base{
						NTP: time.Now(),
						PTS: decodeTime(pts),
					},
					Frames: frames,
				})
				return nil
			})

		default:
			continue
		}

		medias = append(medias, medi)
	}

	if len(medias) == 0 {
		return fmt.Errorf("no supported tracks found")
	}

	res := s.parent.SetReady(PathSourceStaticSetReadyReq{
		Desc:               &description.Session{Medias: medias},
		GenerateRTPPackets: true,
	})
	if res.err != nil {
		return res.err
	}

	stream = res.stream

	for {
		sconn.SetReadDeadline(time.Now().Add(time.Duration(s.readTimeout)))
		err := r.Read()
		if err != nil {
			return err
		}
	}
}

// ApiSourceDescribe implements sourceStaticImpl.
func (*srtSource) ApiSourceDescribe() PathAPISourceOrReader {
	return PathAPISourceOrReader{
		Type: "srtSource",
		ID:   "",
	}
}
