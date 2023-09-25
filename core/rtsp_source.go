package core

import (
	"context"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/headers"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"github.com/bluenviron/gortsplib/v4/pkg/url"
	"github.com/liuhengloveyou/livego/common"
	"github.com/liuhengloveyou/livego/conf"
)

func createRangeHeader(cnf *conf.PathConf) (*headers.Range, error) {
	switch cnf.RtspRangeType {
	case conf.RtspRangeTypeClock:
		start, err := time.Parse("20060102T150405Z", cnf.RtspRangeStart)
		if err != nil {
			return nil, err
		}

		return &headers.Range{
			Value: &headers.RangeUTC{
				Start: start,
			},
		}, nil

	case conf.RtspRangeTypeNPT:
		start, err := time.ParseDuration(cnf.RtspRangeStart)
		if err != nil {
			return nil, err
		}

		return &headers.Range{
			Value: &headers.RangeNPT{
				Start: start,
			},
		}, nil

	case conf.RtspRangeTypeSMPTE:
		start, err := time.ParseDuration(cnf.RtspRangeStart)
		if err != nil {
			return nil, err
		}

		return &headers.Range{
			Value: &headers.RangeSMPTE{
				Start: headers.RangeSMPTETime{
					Time: start,
				},
			},
		}, nil

	default:
		return nil, nil
	}
}

type rtspSourceParent interface {
	SetReady(req PathSourceStaticSetReadyReq) PathSourceStaticSetReadyRes
	SetNotReady(req PathSourceStaticSetNotReadyReq)
}

type rtspSource struct {
	readTimeout    conf.StringDuration
	writeTimeout   conf.StringDuration
	writeQueueSize int
	parent         rtspSourceParent
}

func newRTSPSource(
	readTimeout conf.StringDuration,
	writeTimeout conf.StringDuration,
	writeQueueSize int,
	parent rtspSourceParent,
) *rtspSource {
	return &rtspSource{
		readTimeout:    readTimeout,
		writeTimeout:   writeTimeout,
		writeQueueSize: writeQueueSize,
		parent:         parent,
	}
}

// run implements sourceStaticImpl.
func (s *rtspSource) Run(ctx context.Context, cnf *conf.PathConf, reloadConf chan *conf.PathConf) error {
	common.Logger.Info("rtsp connecting")

	c := &gortsplib.Client{
		Transport:      cnf.SourceProtocol.Transport,
		TLSConfig:      tlsConfigForFingerprint(cnf.SourceFingerprint),
		ReadTimeout:    time.Duration(s.readTimeout),
		WriteTimeout:   time.Duration(s.writeTimeout),
		WriteQueueSize: s.writeQueueSize,
		AnyPortEnable:  cnf.SourceAnyPortEnable,
		OnRequest: func(req *base.Request) {
			common.Logger.Debug("c->s %v", req)
		},
		OnResponse: func(res *base.Response) {
			common.Logger.Debug("s->c %v", res)
		},
		OnTransportSwitch: func(err error) {
			common.Logger.Debug(err.Error())
		},
		OnPacketLost: func(err error) {
			common.Logger.Warn(err.Error())
		},
		OnDecodeError: func(err error) {
			common.Logger.Warn(err.Error())
		},
	}

	u, err := url.Parse(cnf.Source)
	if err != nil {
		common.Logger.Error("url.Parse ERR", "err", err)
		return err
	}

	err = c.Start(u.Scheme, u.Host)
	if err != nil {
		common.Logger.Error("start ERR", "err", err)
		return err
	}
	defer c.Close()

	readErr := make(chan error)
	go func() {
		readErr <- func() error {
			desc, _, err := c.Describe(u)
			if err != nil {
				common.Logger.Error("rtsp Describe ERR: ", "err", err)
				return err
			}

			err = c.SetupAll(desc.BaseURL, desc.Medias)
			if err != nil {
				common.Logger.Error("rtsp SetupAll ERR: ", "err", err)
				return err
			}

			res := s.parent.SetReady(PathSourceStaticSetReadyReq{
				Desc:               desc,
				GenerateRTPPackets: false,
			})
			if res.err != nil {
				common.Logger.Error("rtsp SetReady ERR: ", "err", res.err)
				return res.err
			}

			defer s.parent.SetNotReady(PathSourceStaticSetNotReadyReq{})

			for _, medi := range desc.Medias {
				for _, forma := range medi.Formats {
					cmedi := medi
					cforma := forma

					c.OnPacketRTCPAny(func(medi *description.Media, pkt rtcp.Packet) {
						res.stream.UpdateStats(medi, pkt)
					})

					c.OnPacketRTP(cmedi, cforma, func(pkt *rtp.Packet) {
						pts, ok := c.PacketPTS(cmedi, pkt)
						if !ok {
							return
						}
						res.stream.WriteRTPPacket(cmedi, cforma, pkt, time.Now(), pts)
					})
				}
			}

			rangeHeader, err := createRangeHeader(cnf)
			if err != nil {
				return err
			}

			_, err = c.Play(rangeHeader)
			if err != nil {
				return err
			}

			return c.Wait()
		}()
	}()

	for {
		select {
		case err := <-readErr:
			return err

		case <-reloadConf:

		case <-ctx.Done():
			c.Close()
			<-readErr
			return nil
		}
	}
}

// ApiSourceDescribe implements sourceStaticImpl.
func (*rtspSource) ApiSourceDescribe() PathAPISourceOrReader {
	return PathAPISourceOrReader{
		Type: "rtspSource",
		ID:   "",
	}
}
