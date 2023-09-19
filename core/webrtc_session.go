package core

import (
	"context"
	"fmt"

	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/rtptime"
	"github.com/google/uuid"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v3"

	"github.com/liuhengloveyou/livego/asyncwriter"
	"github.com/liuhengloveyou/livego/log"
	"github.com/liuhengloveyou/livego/webrtcpc"
)

type trackRecvPair struct {
	track    *webrtc.TrackRemote
	receiver *webrtc.RTPReceiver
}

func webrtcMediasOfOutgoingTracks(tracks []*webRTCOutgoingTrack) []*description.Media {
	ret := make([]*description.Media, len(tracks))
	for i, track := range tracks {
		ret[i] = track.media
	}
	return ret
}

func webrtcMediasOfIncomingTracks(tracks []*webRTCIncomingTrack) []*description.Media {
	ret := make([]*description.Media, len(tracks))
	for i, track := range tracks {
		ret[i] = track.media
	}
	return ret
}

func whipOffer(body []byte) *webrtc.SessionDescription {
	return &webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(body),
	}
}

func webrtcWaitUntilConnected(
	ctx context.Context,
	pc *webrtcpc.PeerConnection,
) error {
	t := time.NewTimer(webrtcHandshakeTimeout)
	defer t.Stop()

outer:
	for {
		select {
		case <-t.C:
			return fmt.Errorf("deadline exceeded while waiting connection")

		case <-pc.Connected():
			break outer

		case <-ctx.Done():
			return fmt.Errorf("terminated")
		}
	}

	return nil
}

func webrtcGatherOutgoingTracks(desc *description.Session) ([]*webRTCOutgoingTrack, error) {
	var tracks []*webRTCOutgoingTrack

	videoTrack, err := newWebRTCOutgoingTrackVideo(desc)
	if err != nil {
		return nil, err
	}

	if videoTrack != nil {
		tracks = append(tracks, videoTrack)
	}

	audioTrack, err := newWebRTCOutgoingTrackAudio(desc)
	if err != nil {
		return nil, err
	}

	if audioTrack != nil {
		tracks = append(tracks, audioTrack)
	}

	if tracks == nil {
		return nil, fmt.Errorf(
			"the stream doesn't contain any supported codec, which are currently AV1, VP9, VP8, H264, Opus, G722, G711")
	}

	return tracks, nil
}

func webrtcTrackCount(medias []*sdp.MediaDescription) (int, error) {
	videoTrack := false
	audioTrack := false
	trackCount := 0

	for _, media := range medias {
		switch media.MediaName.Media {
		case "video":
			if videoTrack {
				return 0, fmt.Errorf("only a single video and a single audio track are supported")
			}
			videoTrack = true

		case "audio":
			if audioTrack {
				return 0, fmt.Errorf("only a single video and a single audio track are supported")
			}
			audioTrack = true

		default:
			return 0, fmt.Errorf("unsupported media '%s'", media.MediaName.Media)
		}

		trackCount++
	}

	return trackCount, nil
}

func webrtcGatherIncomingTracks(
	ctx context.Context,
	pc *webrtcpc.PeerConnection,
	trackRecv chan trackRecvPair,
	trackCount int,
) ([]*webRTCIncomingTrack, error) {
	var tracks []*webRTCIncomingTrack

	t := time.NewTimer(webrtcTrackGatherTimeout)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			if trackCount == 0 {
				return tracks, nil
			}
			return nil, fmt.Errorf("deadline exceeded while waiting tracks")

		case pair := <-trackRecv:
			track, err := newWebRTCIncomingTrack(pair.track, pair.receiver, pc.WriteRTCP)
			if err != nil {
				return nil, err
			}
			tracks = append(tracks, track)

			if len(tracks) == trackCount {
				return tracks, nil
			}

		case <-pc.Disconnected():
			return nil, fmt.Errorf("peer connection closed")

		case <-ctx.Done():
			return nil, fmt.Errorf("terminated")
		}
	}
}

type webRTCSession struct {
	writeQueueSize int
	api            *webrtc.API
	req            webRTCNewSessionReq
	wg             *sync.WaitGroup
	parent         *WebRTCManager

	ctx       context.Context
	ctxCancel func()
	created   time.Time
	uuid      uuid.UUID
	secret    uuid.UUID
	mutex     sync.RWMutex
	pc        *webrtcpc.PeerConnection
	Datachan  *webrtc.DataChannel

	chNew           chan webRTCNewSessionReq
	chAddCandidates chan webRTCAddSessionCandidatesReq
}

func newWebRTCSession(
	parentCtx context.Context,
	writeQueueSize int,
	api *webrtc.API,
	req webRTCNewSessionReq,
	wg *sync.WaitGroup,
	parent *WebRTCManager,
) *webRTCSession {
	ctx, ctxCancel := context.WithCancel(parentCtx)

	s := &webRTCSession{
		writeQueueSize:  writeQueueSize,
		api:             api,
		req:             req,
		wg:              wg,
		parent:          parent,
		ctx:             ctx,
		ctxCancel:       ctxCancel,
		created:         time.Now(),
		uuid:            uuid.New(),
		secret:          uuid.New(),
		chNew:           make(chan webRTCNewSessionReq),
		chAddCandidates: make(chan webRTCAddSessionCandidatesReq),
	}

	wg.Add(1)
	go s.run()

	return s
}

func (s *webRTCSession) close() {
	s.ctxCancel()
}

func (s *webRTCSession) run() {
	defer s.wg.Done()

	err := s.runInner()

	s.ctxCancel()

	s.parent.closeSession(s)

	log.Logger.Info("closed: %v", err)
}

func (s *webRTCSession) runInner() error {
	select {
	case <-s.chNew:
	case <-s.ctx.Done():
		return fmt.Errorf("terminated")
	}

	errStatusCode, err := s.runInner2()

	if errStatusCode != 0 {
		s.req.res <- webRTCNewSessionRes{
			err:           err,
			errStatusCode: errStatusCode,
		}
	}

	return err
}

func (s *webRTCSession) runInner2() (int, error) {
	log.Logger.Info("webRTCSession.runInner2", "req", s.req.publish)
	if s.req.publish {
		return s.runPublish()
	} else {
		return s.runRead()
	}
}

func (s *webRTCSession) runPublish() (int, error) {
	ip, _, _ := net.SplitHostPort(s.req.remoteAddr)

	res := DefaultPathManager.addPublisher(PathAddPublisherReq{
		Author:   s,
		PathName: s.req.pathName,
		Credentials: AuthCredentials{
			Query: s.req.query,
			Ip:    net.ParseIP(ip),
			User:  s.req.user,
			Pass:  s.req.pass,
			Proto: AuthProtocolWebRTC,
			ID:    &s.uuid,
		},
	})
	if res.err != nil {
		if _, ok := res.err.(*errAuthentication); ok {
			// wait some seconds to stop brute force attacks
			<-time.After(webrtcPauseAfterAuthError)

			return http.StatusUnauthorized, res.err
		}

		return http.StatusBadRequest, res.err
	}

	defer res.Path.removePublisher(PathRemovePublisherReq{author: s})

	servers, err := s.parent.generateICEServers()
	if err != nil {
		return http.StatusInternalServerError, err
	}

	pc, err := webrtcpc.New(
		servers,
		s.api)
	if err != nil {
		return http.StatusBadRequest, err
	}
	defer pc.Close()

	offer := whipOffer(s.req.offer)

	var sdp sdp.SessionDescription
	err = sdp.Unmarshal([]byte(offer.SDP))
	if err != nil {
		return http.StatusBadRequest, err
	}

	trackCount, err := webrtcTrackCount(sdp.MediaDescriptions)
	if err != nil {
		return http.StatusBadRequest, err
	}

	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RtpTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	if err != nil {
		return http.StatusBadRequest, err
	}

	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RtpTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	if err != nil {
		return http.StatusBadRequest, err
	}

	trackRecv := make(chan trackRecvPair)

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		select {
		case trackRecv <- trackRecvPair{track, receiver}:
		case <-s.ctx.Done():
		}
	})

	err = pc.SetRemoteDescription(*offer)
	if err != nil {
		return http.StatusBadRequest, err
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return http.StatusBadRequest, err
	}

	err = pc.SetLocalDescription(answer)
	if err != nil {
		return http.StatusBadRequest, err
	}

	err = pc.WaitGatheringDone(s.ctx)
	if err != nil {
		return http.StatusBadRequest, err
	}

	s.writeAnswer(pc.LocalDescription())

	go s.readRemoteCandidates(pc)

	err = webrtcWaitUntilConnected(s.ctx, pc)
	if err != nil {
		return 0, err
	}

	s.mutex.Lock()
	s.pc = pc
	s.mutex.Unlock()

	tracks, err := webrtcGatherIncomingTracks(s.ctx, pc, trackRecv, trackCount)
	if err != nil {
		return 0, err
	}
	medias := webrtcMediasOfIncomingTracks(tracks)

	rres := res.Path.startPublisher(PathStartPublisherReq{
		author:             s,
		desc:               &description.Session{Medias: medias},
		generateRTPPackets: false,
	})
	if rres.err != nil {
		return 0, rres.err
	}

	timeDecoder := rtptime.NewGlobalDecoder()

	for _, track := range tracks {
		track.start(rres.stream, timeDecoder)
	}

	select {
	case <-pc.Disconnected():
		return 0, fmt.Errorf("peer connection closed")

	case <-s.ctx.Done():
		return 0, fmt.Errorf("terminated")
	}
}

func (s *webRTCSession) runRead() (int, error) {
	ip, _, _ := net.SplitHostPort(s.req.remoteAddr)
	log.Logger.Info("webRTCSession.runRead")

	res := DefaultPathManager.addReader(PathAddReaderReq{
		Author:   s,
		PathName: s.req.pathName,
		Credentials: AuthCredentials{
			Query: s.req.query,
			Ip:    net.ParseIP(ip),
			User:  s.req.user,
			Pass:  s.req.pass,
			Proto: AuthProtocolWebRTC,
			ID:    &s.uuid,
		},
	})
	if res.err != nil {
		if _, ok := res.err.(*errAuthentication); ok {
			// wait some seconds to stop brute force attacks
			<-time.After(webrtcPauseAfterAuthError)

			return http.StatusUnauthorized, res.err
		}

		if strings.HasPrefix(res.err.Error(), "no one is publishing") {
			return http.StatusNotFound, res.err
		}

		return http.StatusBadRequest, res.err
	}

	defer res.Path.removeReader(PathRemoveReaderReq{author: s})

	tracks, err := webrtcGatherOutgoingTracks(res.stream.Desc())
	if err != nil {
		return http.StatusBadRequest, err
	}

	servers, err := s.parent.generateICEServers()
	if err != nil {
		return http.StatusInternalServerError, err
	}

	pc, err := webrtcpc.New(
		servers,
		s.api)
	if err != nil {
		return http.StatusBadRequest, err
	}
	defer pc.Close()

	for _, track := range tracks {
		var err error
		track.sender, err = pc.AddTrack(track.track)
		if err != nil {
			return http.StatusBadRequest, err
		}
	}

	offer := whipOffer(s.req.offer)

	err = pc.SetRemoteDescription(*offer)
	if err != nil {
		return http.StatusBadRequest, err
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return http.StatusBadRequest, err
	}

	err = pc.SetLocalDescription(answer)
	if err != nil {
		return http.StatusBadRequest, err
	}

	err = pc.WaitGatheringDone(s.ctx)
	if err != nil {
		return http.StatusBadRequest, err
	}

	s.writeAnswer(pc.LocalDescription())

	go s.readRemoteCandidates(pc)

	err = webrtcWaitUntilConnected(s.ctx, pc)
	if err != nil {
		return 0, err
	}

	// Register data channel creation handling
	pc.OnDataChannel(func(d *webrtc.DataChannel) {
		fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

		s.Datachan = d

		// Register channel opening handling
		// d.OnOpen(func() {
		// 	fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", d.Label(), d.ID())
		// 	time.Sleep(3 * time.Second)
		// 	for range time.NewTicker(5 * time.Second).C {

		// 		// Send the message as text
		// 		sendErr := d.SendText(time.Now().GoString())
		// 		if sendErr != nil {
		// 			fmt.Println(sendErr)
		// 		}
		// 	}
		// })

		// Register text message handling
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
		})
	})

	s.mutex.Lock()
	s.pc = pc
	s.mutex.Unlock()

	writer := asyncwriter.New(s.writeQueueSize)

	defer res.stream.RemoveReader(writer)

	for _, track := range tracks {
		track.start(s, res.stream, writer)
	}

	log.Logger.Info("is reading from path '%s', %s",
		res.Path.name, SourceMediaInfo(webrtcMediasOfOutgoingTracks(tracks)))

	writer.Start()

	select {
	case <-pc.Disconnected():
		writer.Stop()
		return 0, fmt.Errorf("peer connection closed")

	case err := <-writer.Error():
		return 0, err

	case <-s.ctx.Done():
		writer.Stop()
		return 0, fmt.Errorf("terminated")
	}
}

func (s *webRTCSession) writeAnswer(answer *webrtc.SessionDescription) {
	s.req.res <- webRTCNewSessionRes{
		sx:     s,
		answer: []byte(answer.SDP),
	}
}

func (s *webRTCSession) readRemoteCandidates(pc *webrtcpc.PeerConnection) {
	for {
		select {
		case req := <-s.chAddCandidates:
			for _, candidate := range req.candidates {
				err := pc.AddICECandidate(*candidate)
				if err != nil {
					req.res <- webRTCAddSessionCandidatesRes{err: err}
				}
			}
			req.res <- webRTCAddSessionCandidatesRes{}

		case <-s.ctx.Done():
			return
		}
	}
}

// new is called by webRTCHTTPServer through webRTCManager.
func (s *webRTCSession) new(req webRTCNewSessionReq) webRTCNewSessionRes {
	select {
	case s.chNew <- req:
		return <-req.res

	case <-s.ctx.Done():
		return webRTCNewSessionRes{err: fmt.Errorf("terminated"), errStatusCode: http.StatusInternalServerError}
	}
}

// addCandidates is called by webRTCHTTPServer through webRTCManager.
func (s *webRTCSession) addCandidates(
	req webRTCAddSessionCandidatesReq,
) webRTCAddSessionCandidatesRes {
	select {
	case s.chAddCandidates <- req:
		return <-req.res

	case <-s.ctx.Done():
		return webRTCAddSessionCandidatesRes{err: fmt.Errorf("terminated")}
	}
}

// apiSourceDescribe implements sourceStaticImpl.
func (s *webRTCSession) ApiSourceDescribe() PathAPISourceOrReader {
	return PathAPISourceOrReader{
		Type: "webRTCSession",
		ID:   s.uuid.String(),
	}
}

// apiReaderDescribe implements reader.
func (s *webRTCSession) apiReaderDescribe() PathAPISourceOrReader {
	return s.ApiSourceDescribe()
}

func (s *webRTCSession) apiItem() *ApiWebRTCSession {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	peerConnectionEstablished := false
	localCandidate := ""
	remoteCandidate := ""
	bytesReceived := uint64(0)
	bytesSent := uint64(0)

	if s.pc != nil {
		peerConnectionEstablished = true
		localCandidate = s.pc.LocalCandidate()
		remoteCandidate = s.pc.RemoteCandidate()
		bytesReceived = s.pc.BytesReceived()
		bytesSent = s.pc.BytesSent()
	}

	return &ApiWebRTCSession{
		ID:                        s.uuid,
		Created:                   s.created,
		RemoteAddr:                s.req.remoteAddr,
		PeerConnectionEstablished: peerConnectionEstablished,
		LocalCandidate:            localCandidate,
		RemoteCandidate:           remoteCandidate,
		State: func() apiWebRTCSessionState {
			if s.req.publish {
				return apiWebRTCSessionStatePublish
			}
			return apiWebRTCSessionStateRead
		}(),
		Path:          s.req.pathName,
		BytesReceived: bytesReceived,
		BytesSent:     bytesSent,
	}
}

func (s *webRTCSession) WriteDataChannel() {

}
