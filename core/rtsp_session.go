package core

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/auth"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/url"
	"github.com/google/uuid"
	"github.com/pion/rtp"

	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/proto"
	"github.com/liuhengloveyou/livego/stream"
)

type rtspSessionPathManager interface {
	addPublisher(req pathAddPublisherReq) pathAddPublisherRes
	addReader(req pathAddReaderReq) pathAddReaderRes
}

type rtspSessionParent interface {
	getISTLS() bool
	getServer() *gortsplib.Server
}

type rtspSession struct {
	isTLS       bool
	protocols   map[conf.Protocol]struct{}
	session     *gortsplib.ServerSession
	author      *gortsplib.ServerConn
	pathManager rtspSessionPathManager
	parent      rtspSessionParent

	uuid      uuid.UUID
	created   time.Time
	path      *path
	stream    *stream.Stream
	mutex     sync.Mutex
	state     gortsplib.ServerSessionState
	transport *gortsplib.Transport
	pathName  string
}

func newRTSPSession(
	isTLS bool,
	protocols map[conf.Protocol]struct{},
	session *gortsplib.ServerSession,
	sc *gortsplib.ServerConn,
	pathManager rtspSessionPathManager,
	parent rtspSessionParent,
) *rtspSession {
	s := &rtspSession{
		isTLS:       isTLS,
		protocols:   protocols,
		session:     session,
		author:      sc,
		pathManager: pathManager,
		parent:      parent,
		uuid:        uuid.New(),
		created:     time.Now(),
	}

	return s
}

// Close closes a Session.
func (s *rtspSession) close() {
	s.session.Close()
}

func (s *rtspSession) remoteAddr() net.Addr {
	return s.author.NetConn().RemoteAddr()
}

func (s *rtspSession) onUnread() {
	if s.path.conf.RunOnUnread != "" {
		env := s.path.externalCmdEnv()
		desc := s.apiReaderDescribe()
		env["MTX_READER_TYPE"] = desc.Type
		env["MTX_READER_ID"] = desc.ID
	}
}

// onClose is called by rtspServer.
func (s *rtspSession) onClose(err error) {
	if s.session.State() == gortsplib.ServerSessionStatePlay {
		s.onUnread()
	}

	switch s.session.State() {
	case gortsplib.ServerSessionStatePrePlay, gortsplib.ServerSessionStatePlay:
		s.path.removeReader(pathRemoveReaderReq{author: s})

	case gortsplib.ServerSessionStatePreRecord, gortsplib.ServerSessionStateRecord:
		s.path.removePublisher(pathRemovePublisherReq{author: s})
	}

	s.path = nil
	s.stream = nil
}

// onAnnounce is called by rtspServer.
func (s *rtspSession) onAnnounce(c *rtspConn, ctx *gortsplib.ServerHandlerOnAnnounceCtx) (*base.Response, error) {
	if len(ctx.Path) == 0 || ctx.Path[0] != '/' {
		return &base.Response{
			StatusCode: base.StatusBadRequest,
		}, fmt.Errorf("invalid path")
	}
	ctx.Path = ctx.Path[1:]

	if c.authNonce == "" {
		var err error
		c.authNonce, err = auth.GenerateNonce()
		if err != nil {
			return &base.Response{
				StatusCode: base.StatusInternalServerError,
			}, err
		}
	}

	res := s.pathManager.addPublisher(pathAddPublisherReq{
		author:   s,
		pathName: ctx.Path,
		credentials: authCredentials{
			query:       ctx.Query,
			ip:          c.ip(),
			proto:       authProtocolRTSP,
			id:          &c.uuid,
			rtspRequest: ctx.Request,
			rtspBaseURL: nil,
			rtspNonce:   c.authNonce,
		},
	})

	if res.err != nil {
		switch terr := res.err.(type) {
		case *errAuthentication:
			return c.handleAuthError(terr)

		default:
			return &base.Response{
				StatusCode: base.StatusBadRequest,
			}, res.err
		}
	}

	s.path = res.path

	s.mutex.Lock()
	s.state = gortsplib.ServerSessionStatePreRecord
	s.pathName = ctx.Path
	s.mutex.Unlock()

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// onSetup is called by rtspServer.
func (s *rtspSession) onSetup(c *rtspConn, ctx *gortsplib.ServerHandlerOnSetupCtx,
) (*base.Response, *gortsplib.ServerStream, error) {
	if len(ctx.Path) == 0 || ctx.Path[0] != '/' {
		return &base.Response{
			StatusCode: base.StatusBadRequest,
		}, nil, fmt.Errorf("invalid path")
	}
	ctx.Path = ctx.Path[1:]

	// in case the client is setupping a stream with UDP or UDP-multicast, and these
	// transport protocols are disabled, gortsplib already blocks the request.
	// we have only to handle the case in which the transport protocol is TCP
	// and it is disabled.
	if ctx.Transport == gortsplib.TransportTCP {
		if _, ok := s.protocols[conf.Protocol(gortsplib.TransportTCP)]; !ok {
			return &base.Response{
				StatusCode: base.StatusUnsupportedTransport,
			}, nil, nil
		}
	}

	switch s.session.State() {
	case gortsplib.ServerSessionStateInitial, gortsplib.ServerSessionStatePrePlay: // play
		baseURL := &url.URL{
			Scheme:   ctx.Request.URL.Scheme,
			Host:     ctx.Request.URL.Host,
			Path:     ctx.Path,
			RawQuery: ctx.Query,
		}

		if ctx.Query != "" {
			baseURL.RawQuery += "/"
		} else {
			baseURL.Path += "/"
		}

		if c.authNonce == "" {
			var err error
			c.authNonce, err = auth.GenerateNonce()
			if err != nil {
				return &base.Response{
					StatusCode: base.StatusInternalServerError,
				}, nil, err
			}
		}

		res := s.pathManager.addReader(pathAddReaderReq{
			author:   s,
			pathName: ctx.Path,
			credentials: authCredentials{
				query:       ctx.Query,
				ip:          c.ip(),
				proto:       authProtocolRTSP,
				id:          &c.uuid,
				rtspRequest: ctx.Request,
				rtspBaseURL: baseURL,
				rtspNonce:   c.authNonce,
			},
		})

		if res.err != nil {
			switch terr := res.err.(type) {
			case *errAuthentication:
				res, err := c.handleAuthError(terr)
				return res, nil, err

			case errPathNoOnePublishing:
				return &base.Response{
					StatusCode: base.StatusNotFound,
				}, nil, res.err

			default:
				return &base.Response{
					StatusCode: base.StatusBadRequest,
				}, nil, res.err
			}
		}

		s.path = res.path
		s.stream = res.stream

		s.mutex.Lock()
		s.state = gortsplib.ServerSessionStatePrePlay
		s.pathName = ctx.Path
		s.mutex.Unlock()

		var stream *gortsplib.ServerStream
		if !s.parent.getISTLS() {
			stream = res.stream.RTSPStream(s.parent.getServer())
		} else {
			stream = res.stream.RTSPSStream(s.parent.getServer())
		}

		return &base.Response{
			StatusCode: base.StatusOK,
		}, stream, nil

	default: // record
		return &base.Response{
			StatusCode: base.StatusOK,
		}, nil, nil
	}
}

// onPlay is called by rtspServer.
func (s *rtspSession) onPlay(_ *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	h := make(base.Header)

	if s.session.State() == gortsplib.ServerSessionStatePrePlay {
		fmt.Println("is reading from path '%s', with %s, %s",
			s.path.name,
			s.session.SetuppedTransport(),
			mediaInfo(s.session.SetuppedMedias()))

		pathConf := s.path.safeConf()

		if pathConf.RunOnRead != "" {
			env := s.path.externalCmdEnv()
			desc := s.apiReaderDescribe()
			env["MTX_READER_TYPE"] = desc.Type
			env["MTX_READER_ID"] = desc.ID

			fmt.Println("runOnRead command started")
		}

		s.mutex.Lock()
		s.state = gortsplib.ServerSessionStatePlay
		s.transport = s.session.SetuppedTransport()
		s.mutex.Unlock()
	}

	return &base.Response{
		StatusCode: base.StatusOK,
		Header:     h,
	}, nil
}

// onRecord is called by rtspServer.
func (s *rtspSession) onRecord(_ *gortsplib.ServerHandlerOnRecordCtx) (*base.Response, error) {
	res := s.path.startPublisher(pathStartPublisherReq{
		author:             s,
		desc:               s.session.AnnouncedDescription(),
		generateRTPPackets: false,
	})
	if res.err != nil {
		return &base.Response{
			StatusCode: base.StatusBadRequest,
		}, res.err
	}

	s.stream = res.stream

	for _, medi := range s.session.AnnouncedDescription().Medias {
		for _, forma := range medi.Formats {
			cmedi := medi
			cforma := forma

			s.session.OnPacketRTP(cmedi, cforma, func(pkt *rtp.Packet) {
				pts, ok := s.session.PacketPTS(cmedi, pkt)
				if !ok {
					return
				}

				res.stream.WriteRTPPacket(cmedi, cforma, pkt, time.Now(), pts)
			})
		}
	}

	s.mutex.Lock()
	s.state = gortsplib.ServerSessionStateRecord
	s.transport = s.session.SetuppedTransport()
	s.mutex.Unlock()

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// onPause is called by rtspServer.
func (s *rtspSession) onPause(_ *gortsplib.ServerHandlerOnPauseCtx) (*base.Response, error) {
	switch s.session.State() {
	case gortsplib.ServerSessionStatePlay:
		s.onUnread()

		s.mutex.Lock()
		s.state = gortsplib.ServerSessionStatePrePlay
		s.mutex.Unlock()

	case gortsplib.ServerSessionStateRecord:
		s.path.stopPublisher(pathStopPublisherReq{author: s})

		s.mutex.Lock()
		s.state = gortsplib.ServerSessionStatePreRecord
		s.mutex.Unlock()
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// apiReaderDescribe implements reader.
func (s *rtspSession) apiReaderDescribe() proto.ApiPathSourceOrReader {
	return proto.ApiPathSourceOrReader{
		Type: func() string {
			if s.isTLS {
				return "rtspsSession"
			}
			return "rtspSession"
		}(),
		ID: s.uuid.String(),
	}
}

// apiSourceDescribe implements source.
func (s *rtspSession) apiSourceDescribe() proto.ApiPathSourceOrReader {
	return s.apiReaderDescribe()
}

// onPacketLost is called by rtspServer.
func (s *rtspSession) onPacketLost(ctx *gortsplib.ServerHandlerOnPacketLostCtx) {
	fmt.Println("rtspSession.onPacketLost: ", ctx.Error.Error())
}

// onDecodeError is called by rtspServer.
func (s *rtspSession) onDecodeError(ctx *gortsplib.ServerHandlerOnDecodeErrorCtx) {
	fmt.Println(ctx.Error.Error())
}

// onStreamWriteError is called by rtspServer.
func (s *rtspSession) onStreamWriteError(ctx *gortsplib.ServerHandlerOnStreamWriteErrorCtx) {
	fmt.Println(ctx.Error.Error())
}

func (s *rtspSession) apiItem() *proto.ApiRTSPSession {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return &proto.ApiRTSPSession{
		ID:         s.uuid,
		Created:    s.created,
		RemoteAddr: s.remoteAddr().String(),
		State: func() proto.ApiRTSPSessionState {
			switch s.state {
			case gortsplib.ServerSessionStatePrePlay,
				gortsplib.ServerSessionStatePlay:
				return proto.ApiRTSPSessionStateRead

			case gortsplib.ServerSessionStatePreRecord,
				gortsplib.ServerSessionStateRecord:
				return proto.ApiRTSPSessionStatePublish
			}
			return proto.ApiRTSPSessionStateIdle
		}(),
		Path: s.pathName,
		Transport: func() *string {
			if s.transport == nil {
				return nil
			}
			v := s.transport.String()
			return &v
		}(),
		BytesReceived: s.session.BytesReceived(),
		BytesSent:     s.session.BytesSent(),
	}
}
