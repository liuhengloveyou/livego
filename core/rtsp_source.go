package core

import (
	"context"
	"fmt"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/headers"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"github.com/bluenviron/gortsplib/v4/pkg/url"
	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/proto"
)

func createRangeHeader(cnf *conf.Path) (*headers.Range, error) {
	switch cnf.RTSPRangeType {
	case conf.RTSPRangeTypeClock:
		start, err := time.Parse("20060102T150405Z", cnf.RTSPRangeStart)
		if err != nil {
			return nil, err
		}

		return &headers.Range{
			Value: &headers.RangeUTC{
				Start: start,
			},
		}, nil

	case conf.RTSPRangeTypeNPT:
		start, err := time.ParseDuration(cnf.RTSPRangeStart)
		if err != nil {
			return nil, err
		}

		return &headers.Range{
			Value: &headers.RangeNPT{
				Start: start,
			},
		}, nil

	case conf.RTSPRangeTypeSMPTE:
		start, err := time.ParseDuration(cnf.RTSPRangeStart)
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
	setReady(req pathSourceStaticSetReadyReq) pathSourceStaticSetReadyRes
	setNotReady(req pathSourceStaticSetNotReadyReq)
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
func (s *rtspSource) run(ctx context.Context, cnf *conf.Path, reloadConf chan *conf.Path) error {
	c := &gortsplib.Client{
		Transport:      cnf.SourceProtocol.Transport,
		TLSConfig:      tlsConfigForFingerprint(cnf.SourceFingerprint),
		ReadTimeout:    time.Duration(s.readTimeout),
		WriteTimeout:   time.Duration(s.writeTimeout),
		WriteQueueSize: s.writeQueueSize,
		AnyPortEnable:  cnf.SourceAnyPortEnable,
		OnRequest: func(req *base.Request) {
			fmt.Printf("[c->s] %v\n", req)
		},
		OnResponse: func(res *base.Response) {
			fmt.Println("[s->c] ", res)
		},
		OnTransportSwitch: func(err error) {
			fmt.Println(err.Error())
		},
		OnPacketLost: func(err error) {
			fmt.Println(err.Error())
		},
		OnDecodeError: func(err error) {
			fmt.Println(err.Error())
		},
	}

	u, err := url.Parse(cnf.Source)
	if err != nil {
		return err
	}

	err = c.Start(u.Scheme, u.Host)
	if err != nil {
		return err
	}
	defer c.Close()

	readErr := make(chan error)
	go func() {
		readErr <- func() error {
			desc, _, err := c.Describe(u)
			if err != nil {
				return err
			}

			err = c.SetupAll(desc.BaseURL, desc.Medias)
			if err != nil {
				return err
			}

			res := s.parent.setReady(pathSourceStaticSetReadyReq{
				desc:               desc,
				generateRTPPackets: false,
			})
			if res.err != nil {
				return res.err
			}

			defer s.parent.setNotReady(pathSourceStaticSetNotReadyReq{})

			for _, medi := range desc.Medias {
				for _, forma := range medi.Formats {
					cmedi := medi
					cforma := forma

					c.OnPacketRTCP(cmedi, func(pkt rtcp.Packet) {
						fmt.Println("rtcp>>>", pkt)
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

// apiSourceDescribe implements sourceStaticImpl.
func (*rtspSource) apiSourceDescribe() proto.ApiPathSourceOrReader {
	return proto.ApiPathSourceOrReader{
		Type: "rtspSource",
		ID:   "",
	}
}
