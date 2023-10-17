package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/proto"
)

const (
	sourceStaticRetryPause = 5 * time.Second
)

type sourceStaticImpl interface {
	run(context.Context, *conf.Path, chan *conf.Path) error
	apiSourceDescribe() proto.ApiPathSourceOrReader
}

type sourceStaticParent interface {
	sourceStaticSetReady(context.Context, pathSourceStaticSetReadyReq)
	sourceStaticSetNotReady(context.Context, pathSourceStaticSetNotReadyReq)
}

// sourceStatic is a static source.
type sourceStatic struct {
	conf   *conf.Path
	parent sourceStaticParent

	ctx       context.Context
	ctxCancel func()
	impl      sourceStaticImpl
	running   bool

	// in
	chReloadConf                  chan *conf.Path
	chSourceStaticImplSetReady    chan pathSourceStaticSetReadyReq
	chSourceStaticImplSetNotReady chan pathSourceStaticSetNotReadyReq

	// out
	done chan struct{}
}

func newSourceStatic(
	cnf *conf.Path,
	readTimeout conf.StringDuration,
	writeTimeout conf.StringDuration,
	writeQueueSize int,
	parent sourceStaticParent,
) *sourceStatic {
	s := &sourceStatic{
		conf:                          cnf,
		parent:                        parent,
		chReloadConf:                  make(chan *conf.Path),
		chSourceStaticImplSetReady:    make(chan pathSourceStaticSetReadyReq),
		chSourceStaticImplSetNotReady: make(chan pathSourceStaticSetNotReadyReq),
	}

	switch {
	case strings.HasPrefix(cnf.Source, "rtsp://") ||
		strings.HasPrefix(cnf.Source, "rtsps://"):
		s.impl = newRTSPSource(
			readTimeout,
			writeTimeout,
			writeQueueSize,
			s)

	case strings.HasPrefix(cnf.Source, "rtmp://") ||
		strings.HasPrefix(cnf.Source, "rtmps://"):
		s.impl = newRTMPSource(
			readTimeout,
			writeTimeout,
			s)

	case strings.HasPrefix(cnf.Source, "udp://"):
		s.impl = newUDPSource(
			readTimeout,
			s)

	case strings.HasPrefix(cnf.Source, "whep://") ||
		strings.HasPrefix(cnf.Source, "wheps://"):
		s.impl = newWebRTCSource(
			readTimeout,
			s)

	case cnf.Source == "rpiCamera":
		s.impl = newRPICameraSource(
			s)
	}

	return s
}

func (s *sourceStatic) close(reason string) {
	s.stop(reason)
}

func (s *sourceStatic) start(onDemand bool) {
	if s.running {
		panic("should not happen")
	}

	s.running = true
	// s.impl.Log(logger.Info, "started%s",
	// 	func() string {
	// 		if onDemand {
	// 			return " on demand"
	// 		}
	// 		return ""
	// 	}())

	s.ctx, s.ctxCancel = context.WithCancel(context.Background())
	s.done = make(chan struct{})

	go s.run()
}

func (s *sourceStatic) stop(reason string) {
	if !s.running {
		panic("should not happen")
	}

	s.running = false
	s.ctxCancel()

	// we must wait since s.ctx is not thread safe
	<-s.done
}

func (s *sourceStatic) run() {
	defer close(s.done)

	var innerCtx context.Context
	var innerCtxCancel func()
	implErr := make(chan error)
	innerReloadConf := make(chan *conf.Path)

	recreate := func() {
		innerCtx, innerCtxCancel = context.WithCancel(context.Background())
		go func() {
			implErr <- s.impl.run(innerCtx, s.conf, innerReloadConf)
		}()
	}

	recreate()

	recreating := false
	recreateTimer := newEmptyTimer()

	for {
		select {
		case err := <-implErr:
			innerCtxCancel()
			fmt.Println(err.Error())
			recreating = true
			recreateTimer = time.NewTimer(sourceStaticRetryPause)

		case newConf := <-s.chReloadConf:
			s.conf = newConf
			if !recreating {
				cReloadConf := innerReloadConf
				cInnerCtx := innerCtx
				go func() {
					select {
					case cReloadConf <- newConf:
					case <-cInnerCtx.Done():
					}
				}()
			}

		case req := <-s.chSourceStaticImplSetReady:
			s.parent.sourceStaticSetReady(s.ctx, req)

		case req := <-s.chSourceStaticImplSetNotReady:
			s.parent.sourceStaticSetNotReady(s.ctx, req)

		case <-recreateTimer.C:
			recreate()
			recreating = false

		case <-s.ctx.Done():
			if !recreating {
				innerCtxCancel()
				<-implErr
			}
			return
		}
	}
}

func (s *sourceStatic) reloadConf(newConf *conf.Path) {
	select {
	case s.chReloadConf <- newConf:
	case <-s.ctx.Done():
	}
}

// apiSourceDescribe implements source.
func (s *sourceStatic) apiSourceDescribe() proto.ApiPathSourceOrReader {
	return s.impl.apiSourceDescribe()
}

// setReady is called by a sourceStaticImpl.
func (s *sourceStatic) setReady(req pathSourceStaticSetReadyReq) pathSourceStaticSetReadyRes {
	req.res = make(chan pathSourceStaticSetReadyRes)
	select {
	case s.chSourceStaticImplSetReady <- req:
		res := <-req.res

		if res.err == nil {
			fmt.Println("ready: %s", mediaInfo(req.desc.Medias))
		}

		return res

	case <-s.ctx.Done():
		return pathSourceStaticSetReadyRes{err: fmt.Errorf("terminated")}
	}
}

// setNotReady is called by a sourceStaticImpl.
func (s *sourceStatic) setNotReady(req pathSourceStaticSetNotReadyReq) {
	req.res = make(chan struct{})
	select {
	case s.chSourceStaticImplSetNotReady <- req:
		<-req.res
	case <-s.ctx.Done():
	}
}
