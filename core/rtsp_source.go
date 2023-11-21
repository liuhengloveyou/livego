package core

import (
	"bytes"
	"context"
	"encoding/binary"
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
func (s *rtspSource) run(ctx context.Context, cnf *conf.Path) error {
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
			fmt.Println("rtspSourc.OnTransportSwitch: ", err.Error())
		},
		OnPacketLost: func(err error) {
			fmt.Println("rtspSourc.OnPacketLost: ", err.Error())
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

						ntp := time.Now()
						// 华为摄像头取rtp扩展头里的ntp
						if pkt.ExtensionProfile == 0xabac {
							var second, microSecond uint32
							binary.Read(bytes.NewBuffer(pkt.Header.GetExtension(0)), binary.BigEndian, &second)
							binary.Read(bytes.NewBuffer(pkt.Header.GetExtension(0)[4:]), binary.BigEndian, &microSecond)

							t1900, _ := time.ParseInLocation("2006-01-02", "1900-01-01", time.UTC)
							ntp = t1900.Add(time.Second * time.Duration(second)).Add(time.Millisecond * time.Duration(float64(microSecond)/4294.967296/1000))
							// fmt.Printf("rtp>>> %d %d %f\n", ntp.Unix(), second, float64(microSecond)/4294.967296/1000)
						}

						res.stream.WriteRTPPacket(cmedi, cforma, pkt, ntp, pts)
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
