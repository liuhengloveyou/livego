package core

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/url"

	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/externalcmd"
	"github.com/liuhengloveyou/livego/log"
	"github.com/liuhengloveyou/livego/stream"
)

func newEmptyTimer() *time.Timer {
	t := time.NewTimer(0)
	<-t.C
	return t
}

type errPathNoOnePublishing struct {
	PathName string
}

// Error implements the error interface.
func (e errPathNoOnePublishing) Error() string {
	return fmt.Sprintf("no one is publishing to Path '%s'", e.PathName)
}

type PathParent interface {
	PathReady(*Path)
	PathNotReady(*Path)
	closePath(*Path)
}

type PathOnDemandState int

const (
	PathOnDemandStateInitial PathOnDemandState = iota
	PathOnDemandStateWaitingReady
	PathOnDemandStateReady
	PathOnDemandStateClosing
)

type PathSourceStaticSetReadyRes struct {
	stream *stream.Stream
	err    error
}

type PathSourceStaticSetReadyReq struct {
	Desc               *description.Session
	GenerateRTPPackets bool
	Res                chan PathSourceStaticSetReadyRes
}

type PathSourceStaticSetNotReadyReq struct {
	res chan struct{}
}

type PathRemoveReaderReq struct {
	author reader
	res    chan struct{}
}

type PathRemovePublisherReq struct {
	author publisher
	res    chan struct{}
}

type PathGetConfForPathRes struct {
	conf *conf.PathConf
	err  error
}

type PathGetConfForPathReq struct {
	name        string
	publish     bool
	credentials AuthCredentials
	res         chan PathGetConfForPathRes
}

type PathDescribeRes struct {
	Path     *Path
	stream   *stream.Stream
	redirect string
	err      error
}

type PathDescribeReq struct {
	PathName    string
	url         *url.URL
	credentials AuthCredentials
	res         chan PathDescribeRes
}

type PathAddReaderRes struct {
	Path   *Path
	stream *stream.Stream
	err    error
}

type PathAddReaderReq struct {
	Author      reader
	PathName    string
	SkipAuth    bool
	Credentials AuthCredentials
	Res         chan PathAddReaderRes
}

type PathAddPublisherRes struct {
	Path *Path
	err  error
}

type PathAddPublisherReq struct {
	Author      publisher
	PathName    string
	SkipAuth    bool
	Credentials AuthCredentials
	Res         chan PathAddPublisherRes
}

type PathStartPublisherRes struct {
	stream *stream.Stream
	err    error
}

type PathStartPublisherReq struct {
	author             publisher
	desc               *description.Session
	generateRTPPackets bool
	res                chan PathStartPublisherRes
}

type PathStopPublisherReq struct {
	author publisher
	res    chan struct{}
}

type PathAPISourceOrReader struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type PathAPIPathsListRes struct {
	data  *apiPathsList
	Paths map[string]*Path
}

type PathAPIPathsListReq struct {
	res chan PathAPIPathsListRes
}

type PathAPIPathsGetRes struct {
	Path *Path
	data *apiPath
	err  error
}

type PathAPIPathsGetReq struct {
	name string
	res  chan PathAPIPathsGetRes
}

type Path struct {
	rtspAddress       string
	readTimeout       conf.StringDuration
	writeTimeout      conf.StringDuration
	writeQueueSize    int
	udpMaxPayloadSize int
	confName          string
	conf              *conf.PathConf
	name              string
	matches           []string
	wg                *sync.WaitGroup
	parent            PathParent

	ctx                            context.Context
	ctxCancel                      func()
	confMutex                      sync.RWMutex
	source                         Source
	stream                         *stream.Stream
	readyTime                      time.Time
	bytesReceived                  *uint64
	readers                        map[reader]struct{}
	describeRequestsOnHold         []PathDescribeReq
	readerAddRequestsOnHold        []PathAddReaderReq
	onDemandCmd                    *externalcmd.Cmd
	onReadyCmd                     *externalcmd.Cmd
	onDemandStaticSourceState      PathOnDemandState
	onDemandStaticSourceReadyTimer *time.Timer
	onDemandStaticSourceCloseTimer *time.Timer
	onDemandPublisherState         PathOnDemandState
	onDemandPublisherReadyTimer    *time.Timer
	onDemandPublisherCloseTimer    *time.Timer
	externalCmdPool                *externalcmd.Pool

	// in
	chReloadConf              chan *conf.PathConf
	chSourceStaticSetReady    chan PathSourceStaticSetReadyReq
	chSourceStaticSetNotReady chan PathSourceStaticSetNotReadyReq
	chDescribe                chan PathDescribeReq
	chRemovePublisher         chan PathRemovePublisherReq
	chAddPublisher            chan PathAddPublisherReq
	chStartPublisher          chan PathStartPublisherReq
	chStopPublisher           chan PathStopPublisherReq
	chAddReader               chan PathAddReaderReq
	chRemoveReader            chan PathRemoveReaderReq
	chAPIPathsGet             chan PathAPIPathsGetReq

	// out
	done chan struct{}
}

func newPath(
	parentCtx context.Context,
	rtspAddress string,
	readTimeout conf.StringDuration,
	writeTimeout conf.StringDuration,
	writeQueueSize int,
	udpMaxPayloadSize int,
	confName string,
	cnf *conf.PathConf,
	name string,
	matches []string,
	wg *sync.WaitGroup,
	externalCmdPool *externalcmd.Pool,
	parent PathParent,
) *Path {
	ctx, ctxCancel := context.WithCancel(parentCtx)

	pa := &Path{
		rtspAddress:                    rtspAddress,
		readTimeout:                    readTimeout,
		writeTimeout:                   writeTimeout,
		writeQueueSize:                 writeQueueSize,
		udpMaxPayloadSize:              udpMaxPayloadSize,
		confName:                       confName,
		conf:                           cnf,
		name:                           name,
		matches:                        matches,
		wg:                             wg,
		externalCmdPool:                externalCmdPool,
		parent:                         parent,
		ctx:                            ctx,
		ctxCancel:                      ctxCancel,
		bytesReceived:                  new(uint64),
		readers:                        make(map[reader]struct{}),
		onDemandStaticSourceReadyTimer: newEmptyTimer(),
		onDemandStaticSourceCloseTimer: newEmptyTimer(),
		onDemandPublisherReadyTimer:    newEmptyTimer(),
		onDemandPublisherCloseTimer:    newEmptyTimer(),
		chReloadConf:                   make(chan *conf.PathConf),
		chSourceStaticSetReady:         make(chan PathSourceStaticSetReadyReq),
		chSourceStaticSetNotReady:      make(chan PathSourceStaticSetNotReadyReq),
		chDescribe:                     make(chan PathDescribeReq),
		chRemovePublisher:              make(chan PathRemovePublisherReq),
		chAddPublisher:                 make(chan PathAddPublisherReq),
		chStartPublisher:               make(chan PathStartPublisherReq),
		chStopPublisher:                make(chan PathStopPublisherReq),
		chAddReader:                    make(chan PathAddReaderReq),
		chRemoveReader:                 make(chan PathRemoveReaderReq),
		chAPIPathsGet:                  make(chan PathAPIPathsGetReq),
		done:                           make(chan struct{}),
	}

	pa.wg.Add(1)
	go pa.run()

	return pa
}

func (pa *Path) close() {
	pa.ctxCancel()
}

func (pa *Path) wait() {
	<-pa.done
}

func (pa *Path) safeConf() *conf.PathConf {
	pa.confMutex.RLock()
	defer pa.confMutex.RUnlock()
	return pa.conf
}

func (pa *Path) run() {
	defer close(pa.done)
	defer pa.wg.Done()
	fmt.Println("@@@@@@Path.run", pa.conf.HasStaticSource(), pa.conf.SourceOnDemand)

	if pa.conf.Source == "redirect" {
		pa.source = &SourceRedirect{}
	} else if pa.conf.HasStaticSource() {
		pa.source = NewSourceStatic(
			pa.conf,
			pa.readTimeout,
			pa.writeTimeout,
			pa.writeQueueSize,
			pa)

		if !pa.conf.SourceOnDemand {
			pa.source.(*sourceStatic).start()
		}
	}

	var onInitCmd *externalcmd.Cmd
	if pa.conf.RunOnInit != "" {
		onInitCmd = externalcmd.NewCmd(
			pa.externalCmdPool,
			pa.conf.RunOnInit,
			pa.conf.RunOnInitRestart,
			pa.externalCmdEnv(),
			func(err error) {
				log.Logger.Info("runOnInit command exited: %v", err)
			})
	}

	err := pa.runInner()

	// call before destroying context
	pa.parent.closePath(pa)

	pa.ctxCancel()

	pa.onDemandStaticSourceReadyTimer.Stop()
	pa.onDemandStaticSourceCloseTimer.Stop()
	pa.onDemandPublisherReadyTimer.Stop()
	pa.onDemandPublisherCloseTimer.Stop()

	if onInitCmd != nil {
		onInitCmd.Close()
		log.Logger.Info("runOnInit command stopped")
	}

	for _, req := range pa.describeRequestsOnHold {
		req.res <- PathDescribeRes{err: fmt.Errorf("terminated")}
	}

	for _, req := range pa.readerAddRequestsOnHold {
		req.Res <- PathAddReaderRes{err: fmt.Errorf("terminated")}
	}

	if pa.stream != nil {
		pa.setNotReady()
	}

	if pa.source != nil {
		if source, ok := pa.source.(*sourceStatic); ok {
			if !pa.conf.SourceOnDemand || pa.onDemandStaticSourceState != PathOnDemandStateInitial {
				source.close()
			}
		} else if source, ok := pa.source.(publisher); ok {
			source.close()
		}
	}

	if pa.onDemandCmd != nil {
		pa.onDemandCmd.Close()
		log.Logger.Info("runOnDemand command stopped")
	}

	log.Logger.Info("destroyed: %v", err)
}

func (pa *Path) runInner() error {
	for {
		select {
		case <-pa.onDemandStaticSourceReadyTimer.C:
			pa.doOnDemandStaticSourceReadyTimer()

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case <-pa.onDemandStaticSourceCloseTimer.C:
			pa.doOnDemandStaticSourceCloseTimer()

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case <-pa.onDemandPublisherReadyTimer.C:
			pa.doOnDemandPublisherReadyTimer()

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case <-pa.onDemandPublisherCloseTimer.C:
			pa.doOnDemandPublisherCloseTimer()

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case newConf := <-pa.chReloadConf:
			pa.doReloadConf(newConf)

		case req := <-pa.chSourceStaticSetReady:
			pa.doSourceStaticSetReady(req)

		case req := <-pa.chSourceStaticSetNotReady:
			pa.doSourceStaticSetNotReady(req)

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case req := <-pa.chDescribe:
			pa.doDescribe(req)

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case req := <-pa.chRemovePublisher:
			pa.doRemovePublisher(req)

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case req := <-pa.chAddPublisher:
			pa.doAddPublisher(req)

		case req := <-pa.chStartPublisher:
			pa.doStartPublisher(req)

		case req := <-pa.chStopPublisher:
			pa.doStopPublisher(req)

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case req := <-pa.chAddReader:
			pa.doAddReader(req)

			if pa.shouldClose() {
				return fmt.Errorf("not in use")
			}

		case req := <-pa.chRemoveReader:
			pa.doRemoveReader(req)

		case req := <-pa.chAPIPathsGet:
			pa.doAPIPathsGet(req)

		case <-pa.ctx.Done():
			return fmt.Errorf("terminated")
		}
	}
}

func (pa *Path) doOnDemandStaticSourceReadyTimer() {
	for _, req := range pa.describeRequestsOnHold {
		req.res <- PathDescribeRes{err: fmt.Errorf("source of Path '%s' has timed out", pa.name)}
	}
	pa.describeRequestsOnHold = nil

	for _, req := range pa.readerAddRequestsOnHold {
		req.Res <- PathAddReaderRes{err: fmt.Errorf("source of Path '%s' has timed out", pa.name)}
	}
	pa.readerAddRequestsOnHold = nil

	pa.onDemandStaticSourceStop("timed out")
}

func (pa *Path) doOnDemandStaticSourceCloseTimer() {
	pa.setNotReady()
	pa.onDemandStaticSourceStop("not needed by anyone")
}

func (pa *Path) doOnDemandPublisherReadyTimer() {
	for _, req := range pa.describeRequestsOnHold {
		req.res <- PathDescribeRes{err: fmt.Errorf("source of Path '%s' has timed out", pa.name)}
	}
	pa.describeRequestsOnHold = nil

	for _, req := range pa.readerAddRequestsOnHold {
		req.Res <- PathAddReaderRes{err: fmt.Errorf("source of Path '%s' has timed out", pa.name)}
	}
	pa.readerAddRequestsOnHold = nil

	pa.onDemandPublisherStop("timed out")
}

func (pa *Path) doOnDemandPublisherCloseTimer() {
	pa.onDemandPublisherStop("not needed by anyone")
}

func (pa *Path) doReloadConf(newConf *conf.PathConf) {
	if pa.conf.HasStaticSource() {
		go pa.source.(*sourceStatic).reloadConf(newConf)
	}

	pa.confMutex.Lock()
	pa.conf = newConf
	pa.confMutex.Unlock()
}

func (pa *Path) doSourceStaticSetReady(req PathSourceStaticSetReadyReq) {
	fmt.Println("@@@@@@Path) doSourceStaticSetReady")
	err := pa.setReady(req.Desc, req.GenerateRTPPackets)
	if err != nil {
		req.Res <- PathSourceStaticSetReadyRes{err: err}
		return
	}

	if pa.conf.HasOnDemandStaticSource() {
		pa.onDemandStaticSourceReadyTimer.Stop()
		pa.onDemandStaticSourceReadyTimer = newEmptyTimer()

		pa.onDemandStaticSourceScheduleClose()

		for _, req := range pa.describeRequestsOnHold {
			req.res <- PathDescribeRes{
				stream: pa.stream,
			}
		}
		pa.describeRequestsOnHold = nil

		for _, req := range pa.readerAddRequestsOnHold {
			pa.addReaderPost(req)
		}
		pa.readerAddRequestsOnHold = nil
	}

	req.Res <- PathSourceStaticSetReadyRes{stream: pa.stream}
}

func (pa *Path) doSourceStaticSetNotReady(req PathSourceStaticSetNotReadyReq) {
	pa.setNotReady()

	// send response before calling onDemandStaticSourceStop()
	// in order to avoid a deadlock due to sourceStatic.stop()
	close(req.res)

	if pa.conf.HasOnDemandStaticSource() && pa.onDemandStaticSourceState != PathOnDemandStateInitial {
		pa.onDemandStaticSourceStop("an error occurred")
	}
}

func (pa *Path) doDescribe(req PathDescribeReq) {
	if _, ok := pa.source.(*SourceRedirect); ok {
		req.res <- PathDescribeRes{
			redirect: pa.conf.SourceRedirect,
		}
		return
	}

	if pa.stream != nil {
		req.res <- PathDescribeRes{
			stream: pa.stream,
		}
		return
	}

	if pa.conf.HasOnDemandStaticSource() {
		if pa.onDemandStaticSourceState == PathOnDemandStateInitial {
			pa.onDemandStaticSourceStart()
		}
		pa.describeRequestsOnHold = append(pa.describeRequestsOnHold, req)
		return
	}

	if pa.conf.HasOnDemandPublisher() {
		if pa.onDemandPublisherState == PathOnDemandStateInitial {
			pa.onDemandPublisherStart()
		}
		pa.describeRequestsOnHold = append(pa.describeRequestsOnHold, req)
		return
	}

	if pa.conf.Fallback != "" {
		fallbackURL := func() string {
			if strings.HasPrefix(pa.conf.Fallback, "/") {
				ur := url.URL{
					Scheme: req.url.Scheme,
					User:   req.url.User,
					Host:   req.url.Host,
					Path:   pa.conf.Fallback,
				}
				return ur.String()
			}
			return pa.conf.Fallback
		}()
		req.res <- PathDescribeRes{redirect: fallbackURL}
		return
	}

	req.res <- PathDescribeRes{err: errPathNoOnePublishing{PathName: pa.name}}
}

func (pa *Path) doRemovePublisher(req PathRemovePublisherReq) {
	if pa.source == req.author {
		pa.executeRemovePublisher()
	}
	close(req.res)
}

func (pa *Path) doAddPublisher(req PathAddPublisherReq) {
	if pa.conf.Source != "publisher" {
		req.Res <- PathAddPublisherRes{
			err: fmt.Errorf("can't publish to Path '%s' since 'source' is not 'publisher'", pa.name),
		}
		return
	}

	if pa.source != nil {
		if !pa.conf.OverridePublisher {
			req.Res <- PathAddPublisherRes{err: fmt.Errorf("someone is already publishing to Path '%s'", pa.name)}
			return
		}

		log.Logger.Info("closing existing publisher")
		pa.source.(publisher).close()
		pa.executeRemovePublisher()
	}

	pa.source = req.Author

	req.Res <- PathAddPublisherRes{Path: pa}
}

func (pa *Path) doStartPublisher(req PathStartPublisherReq) {
	if pa.source != req.author {
		req.res <- PathStartPublisherRes{err: fmt.Errorf("publisher is not assigned to this Path anymore")}
		return
	}

	err := pa.setReady(req.desc, req.generateRTPPackets)
	if err != nil {
		req.res <- PathStartPublisherRes{err: err}
		return
	}

	log.Logger.Info("is publishing to Path '%s', %s", pa.name, SourceMediaInfo(req.desc.Medias))

	if pa.conf.HasOnDemandPublisher() {
		pa.onDemandPublisherReadyTimer.Stop()
		pa.onDemandPublisherReadyTimer = newEmptyTimer()

		pa.onDemandPublisherScheduleClose()

		for _, req := range pa.describeRequestsOnHold {
			req.res <- PathDescribeRes{
				stream: pa.stream,
			}
		}
		pa.describeRequestsOnHold = nil

		for _, req := range pa.readerAddRequestsOnHold {
			pa.addReaderPost(req)
		}
		pa.readerAddRequestsOnHold = nil
	}

	req.res <- PathStartPublisherRes{stream: pa.stream}
}

func (pa *Path) doStopPublisher(req PathStopPublisherReq) {
	if req.author == pa.source && pa.stream != nil {
		pa.setNotReady()
	}
	close(req.res)
}

func (pa *Path) doAddReader(req PathAddReaderReq) {
	if pa.stream != nil {
		pa.addReaderPost(req)
		return
	}

	if pa.conf.HasOnDemandStaticSource() {
		if pa.onDemandStaticSourceState == PathOnDemandStateInitial {
			pa.onDemandStaticSourceStart()
		}
		pa.readerAddRequestsOnHold = append(pa.readerAddRequestsOnHold, req)
		return
	}

	if pa.conf.HasOnDemandPublisher() {
		if pa.onDemandPublisherState == PathOnDemandStateInitial {
			pa.onDemandPublisherStart()
		}
		pa.readerAddRequestsOnHold = append(pa.readerAddRequestsOnHold, req)
		return
	}

	req.Res <- PathAddReaderRes{err: errPathNoOnePublishing{PathName: pa.name}}
}

func (pa *Path) doRemoveReader(req PathRemoveReaderReq) {
	if _, ok := pa.readers[req.author]; ok {
		pa.executeRemoveReader(req.author)
	}
	close(req.res)

	if len(pa.readers) == 0 {
		if pa.conf.HasOnDemandStaticSource() {
			if pa.onDemandStaticSourceState == PathOnDemandStateReady {
				pa.onDemandStaticSourceScheduleClose()
			}
		} else if pa.conf.HasOnDemandPublisher() {
			if pa.onDemandPublisherState == PathOnDemandStateReady {
				pa.onDemandPublisherScheduleClose()
			}
		}
	}
}

func (pa *Path) doAPIPathsGet(req PathAPIPathsGetReq) {
	req.res <- PathAPIPathsGetRes{
		data: &apiPath{
			Name:     pa.name,
			ConfName: pa.confName,
			Conf:     pa.conf,
			Source: func() interface{} {
				if pa.source == nil {
					return nil
				}
				return pa.source.ApiSourceDescribe()
			}(),
			SourceReady: pa.stream != nil,
			Ready:       pa.stream != nil,
			ReadyTime: func() *time.Time {
				if pa.stream == nil {
					return nil
				}
				v := pa.readyTime
				return &v
			}(),
			Tracks: func() []string {
				if pa.stream == nil {
					return []string{}
				}
				return mediasDescription(pa.stream.Desc().Medias)
			}(),
			BytesReceived: atomic.LoadUint64(pa.bytesReceived),
			Readers: func() []interface{} {
				ret := []interface{}{}
				for r := range pa.readers {
					ret = append(ret, r.apiReaderDescribe())
				}
				return ret
			}(),
		},
	}
}

func (pa *Path) shouldClose() bool {
	return pa.conf.Regexp != nil &&
		pa.source == nil &&
		len(pa.readers) == 0 &&
		len(pa.describeRequestsOnHold) == 0 &&
		len(pa.readerAddRequestsOnHold) == 0
}

func (pa *Path) externalCmdEnv() externalcmd.Environment {
	_, port, _ := net.SplitHostPort(pa.rtspAddress)
	env := externalcmd.Environment{
		"MTX_PATH":  pa.name,
		"RTSP_PATH": pa.name, // deprecated
		"RTSP_PORT": port,
	}

	if len(pa.matches) > 1 {
		for i, ma := range pa.matches[1:] {
			env["G"+strconv.FormatInt(int64(i+1), 10)] = ma
		}
	}

	return env
}

func (pa *Path) onDemandStaticSourceStart() {
	pa.source.(*sourceStatic).start()

	pa.onDemandStaticSourceReadyTimer.Stop()
	pa.onDemandStaticSourceReadyTimer = time.NewTimer(time.Duration(pa.conf.SourceOnDemandStartTimeout))

	pa.onDemandStaticSourceState = PathOnDemandStateWaitingReady
}

func (pa *Path) onDemandStaticSourceScheduleClose() {
	pa.onDemandStaticSourceCloseTimer.Stop()
	pa.onDemandStaticSourceCloseTimer = time.NewTimer(time.Duration(pa.conf.SourceOnDemandCloseAfter))

	pa.onDemandStaticSourceState = PathOnDemandStateClosing
}

func (pa *Path) onDemandStaticSourceStop(reason string) {
	if pa.onDemandStaticSourceState == PathOnDemandStateClosing {
		pa.onDemandStaticSourceCloseTimer.Stop()
		pa.onDemandStaticSourceCloseTimer = newEmptyTimer()
	}

	pa.onDemandStaticSourceState = PathOnDemandStateInitial

	pa.source.(*sourceStatic).stop()
}

func (pa *Path) onDemandPublisherStart() {
	pa.onDemandCmd = externalcmd.NewCmd(
		pa.externalCmdPool,
		pa.conf.RunOnDemand,
		pa.conf.RunOnDemandRestart,
		pa.externalCmdEnv(),
		func(err error) {
			log.Logger.Info("runOnDemand command exited: %v", err)
		})

	pa.onDemandPublisherReadyTimer.Stop()
	pa.onDemandPublisherReadyTimer = time.NewTimer(time.Duration(pa.conf.RunOnDemandStartTimeout))

	pa.onDemandPublisherState = PathOnDemandStateWaitingReady
}

func (pa *Path) onDemandPublisherScheduleClose() {
	pa.onDemandPublisherCloseTimer.Stop()
	pa.onDemandPublisherCloseTimer = time.NewTimer(time.Duration(pa.conf.RunOnDemandCloseAfter))

	pa.onDemandPublisherState = PathOnDemandStateClosing
}

func (pa *Path) onDemandPublisherStop(reason string) {
	if pa.source != nil {
		pa.source.(publisher).close()
		pa.executeRemovePublisher()
	}

	if pa.onDemandPublisherState == PathOnDemandStateClosing {
		pa.onDemandPublisherCloseTimer.Stop()
		pa.onDemandPublisherCloseTimer = newEmptyTimer()
	}

	pa.onDemandPublisherState = PathOnDemandStateInitial

	if pa.onDemandCmd != nil {
		pa.onDemandCmd.Close()
		pa.onDemandCmd = nil
	}
}

func (pa *Path) setReady(desc *description.Session, allocateEncoder bool) error {
	var err error
	pa.stream, err = stream.New(
		pa.udpMaxPayloadSize,
		desc,
		allocateEncoder,
		pa.bytesReceived,
	)
	if err != nil {
		return err
	}

	pa.readyTime = time.Now()

	if pa.conf.RunOnReady != "" {
		pa.onReadyCmd = externalcmd.NewCmd(
			pa.externalCmdPool,
			pa.conf.RunOnReady,
			pa.conf.RunOnReadyRestart,
			pa.externalCmdEnv(),
			func(err error) {
				log.Logger.Info("runOnReady command exited: %v", err)
			})
	}

	pa.parent.PathReady(pa)

	return nil
}

func (pa *Path) setNotReady() {
	pa.parent.PathNotReady(pa)

	for r := range pa.readers {
		pa.executeRemoveReader(r)
		r.close()
	}

	if pa.onReadyCmd != nil {
		pa.onReadyCmd.Close()
		pa.onReadyCmd = nil
		log.Logger.Info("runOnReady command stopped")
	}

	if pa.stream != nil {
		pa.stream.Close()
		pa.stream = nil
	}
}

func (pa *Path) executeRemoveReader(r reader) {
	delete(pa.readers, r)
}

func (pa *Path) executeRemovePublisher() {
	if pa.stream != nil {
		pa.setNotReady()
	}

	pa.source = nil
}

func (pa *Path) addReaderPost(req PathAddReaderReq) {
	if _, ok := pa.readers[req.Author]; ok {
		req.Res <- PathAddReaderRes{
			Path:   pa,
			stream: pa.stream,
		}
		return
	}

	if pa.conf.MaxReaders != 0 && len(pa.readers) >= pa.conf.MaxReaders {
		req.Res <- PathAddReaderRes{
			err: fmt.Errorf("maximum reader count reached"),
		}
		return
	}

	pa.readers[req.Author] = struct{}{}

	if pa.conf.HasOnDemandStaticSource() {
		if pa.onDemandStaticSourceState == PathOnDemandStateClosing {
			pa.onDemandStaticSourceState = PathOnDemandStateReady
			pa.onDemandStaticSourceCloseTimer.Stop()
			pa.onDemandStaticSourceCloseTimer = newEmptyTimer()
		}
	} else if pa.conf.HasOnDemandPublisher() {
		if pa.onDemandPublisherState == PathOnDemandStateClosing {
			pa.onDemandPublisherState = PathOnDemandStateReady
			pa.onDemandPublisherCloseTimer.Stop()
			pa.onDemandPublisherCloseTimer = newEmptyTimer()
		}
	}

	req.Res <- PathAddReaderRes{
		Path:   pa,
		stream: pa.stream,
	}
}

// reloadConf is called by PathManager.
func (pa *Path) reloadConf(newConf *conf.PathConf) {
	select {
	case pa.chReloadConf <- newConf:
	case <-pa.ctx.Done():
	}
}

// sourceStaticSetReady is called by sourceStatic.
func (pa *Path) sourceStaticSetReady(sourceStaticCtx context.Context, req PathSourceStaticSetReadyReq) {
	select {
	case pa.chSourceStaticSetReady <- req:

	case <-pa.ctx.Done():
		req.Res <- PathSourceStaticSetReadyRes{err: fmt.Errorf("terminated")}

	// this avoids:
	// - invalid requests sent after the source has been terminated
	// - deadlocks caused by <-done inside stop()
	case <-sourceStaticCtx.Done():
		req.Res <- PathSourceStaticSetReadyRes{err: fmt.Errorf("terminated")}
	}
}

// sourceStaticSetNotReady is called by sourceStatic.
func (pa *Path) sourceStaticSetNotReady(sourceStaticCtx context.Context, req PathSourceStaticSetNotReadyReq) {
	select {
	case pa.chSourceStaticSetNotReady <- req:

	case <-pa.ctx.Done():
		close(req.res)

	// this avoids:
	// - invalid requests sent after the source has been terminated
	// - deadlocks caused by <-done inside stop()
	case <-sourceStaticCtx.Done():
		close(req.res)
	}
}

// describe is called by a reader or publisher through PathManager.
func (pa *Path) describe(req PathDescribeReq) PathDescribeRes {
	select {
	case pa.chDescribe <- req:
		return <-req.res
	case <-pa.ctx.Done():
		return PathDescribeRes{err: fmt.Errorf("terminated")}
	}
}

// removePublisher is called by a publisher.
func (pa *Path) removePublisher(req PathRemovePublisherReq) {
	req.res = make(chan struct{})
	select {
	case pa.chRemovePublisher <- req:
		<-req.res
	case <-pa.ctx.Done():
	}
}

// addPublisher is called by a publisher through PathManager.
func (pa *Path) addPublisher(req PathAddPublisherReq) PathAddPublisherRes {
	select {
	case pa.chAddPublisher <- req:
		return <-req.Res
	case <-pa.ctx.Done():
		return PathAddPublisherRes{err: fmt.Errorf("terminated")}
	}
}

// startPublisher is called by a publisher.
func (pa *Path) startPublisher(req PathStartPublisherReq) PathStartPublisherRes {
	req.res = make(chan PathStartPublisherRes)
	select {
	case pa.chStartPublisher <- req:
		return <-req.res
	case <-pa.ctx.Done():
		return PathStartPublisherRes{err: fmt.Errorf("terminated")}
	}
}

// stopPublisher is called by a publisher.
func (pa *Path) stopPublisher(req PathStopPublisherReq) {
	req.res = make(chan struct{})
	select {
	case pa.chStopPublisher <- req:
		<-req.res
	case <-pa.ctx.Done():
	}
}

// addReader is called by a reader through PathManager.
func (pa *Path) addReader(req PathAddReaderReq) PathAddReaderRes {
	log.Logger.Info("path.addReader", "req", req)
	select {
	case pa.chAddReader <- req:
		return <-req.Res
	case <-pa.ctx.Done():
		return PathAddReaderRes{err: fmt.Errorf("terminated")}
	}
}

// removeReader is called by a reader.
func (pa *Path) removeReader(req PathRemoveReaderReq) {
	req.res = make(chan struct{})
	select {
	case pa.chRemoveReader <- req:
		<-req.res
	case <-pa.ctx.Done():
	}
}

// apiPathsGet is called by api.
func (pa *Path) apiPathsGet(req PathAPIPathsGetReq) (*apiPath, error) {
	req.res = make(chan PathAPIPathsGetRes)
	select {
	case pa.chAPIPathsGet <- req:
		res := <-req.res
		return res.data, res.err

	case <-pa.ctx.Done():
		return nil, fmt.Errorf("terminated")
	}
}
