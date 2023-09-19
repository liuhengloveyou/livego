package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/log"
)

const (
	sourceStaticRetryPause = 5 * time.Second
)

type sourceStaticImpl interface {
	Run(context.Context, *conf.PathConf, chan *conf.PathConf) error
	ApiSourceDescribe() PathAPISourceOrReader
}

type sourceStaticParent interface {
	sourceStaticSetReady(context.Context, PathSourceStaticSetReadyReq)
	sourceStaticSetNotReady(context.Context, PathSourceStaticSetNotReadyReq)
}

// sourceStatic is a static source.
type sourceStatic struct {
	conf   *conf.PathConf
	parent sourceStaticParent

	ctx       context.Context
	ctxCancel func()
	impl      sourceStaticImpl
	running   bool

	// in
	chReloadConf                  chan *conf.PathConf
	chSourceStaticImplSetReady    chan PathSourceStaticSetReadyReq
	chSourceStaticImplSetNotReady chan PathSourceStaticSetNotReadyReq

	// out
	done chan struct{}
}

func NewSourceStatic(
	cnf *conf.PathConf,
	readTimeout conf.StringDuration,
	writeTimeout conf.StringDuration,
	writeQueueSize int,
	parent sourceStaticParent,
) *sourceStatic {
	s := &sourceStatic{
		conf:                          cnf,
		parent:                        parent,
		chReloadConf:                  make(chan *conf.PathConf),
		chSourceStaticImplSetReady:    make(chan PathSourceStaticSetReadyReq),
		chSourceStaticImplSetNotReady: make(chan PathSourceStaticSetNotReadyReq),
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

	case strings.HasPrefix(cnf.Source, "srt://"):
		s.impl = newSRTSource(
			readTimeout,
			s)

	case strings.HasPrefix(cnf.Source, "whep://") ||
		strings.HasPrefix(cnf.Source, "wheps://"):
		s.impl = NewWebRTCSource(
			readTimeout,
			s)
	}

	return s
}

func (s *sourceStatic) close() {
	if s.running {
		s.stop()
	}
}

func (s *sourceStatic) start() {
	if s.running {
		panic("should not happen")
	}

	s.running = true
	log.Logger.Info("started")

	s.ctx, s.ctxCancel = context.WithCancel(context.Background())
	s.done = make(chan struct{})

	go s.Run()
}

func (s *sourceStatic) stop() {
	if !s.running {
		panic("should not happen")
	}

	s.running = false
	log.Logger.Info("stopped")

	s.ctxCancel()

	// we must wait since s.ctx is not thread safe
	<-s.done
}

func (s *sourceStatic) Run() {
	log.Logger.Info("@@@@@@sourceStatic.Run", s.conf)
	defer close(s.done)

	var innerCtx context.Context
	var innerCtxCancel func()
	implErr := make(chan error)
	innerReloadConf := make(chan *conf.PathConf)

	recreate := func() {
		innerCtx, innerCtxCancel = context.WithCancel(context.Background())
		go func() {
			implErr <- s.impl.Run(innerCtx, s.conf, innerReloadConf)
		}()
	}

	recreate()

	recreating := false
	recreateTimer := newEmptyTimer()

	for {
		select {
		case err := <-implErr:
			innerCtxCancel()
			log.Logger.Error(err.Error())
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

func (s *sourceStatic) reloadConf(newConf *conf.PathConf) {
	select {
	case s.chReloadConf <- newConf:
	case <-s.ctx.Done():
	}
}

// ApiSourceDescribe implements source.
func (s *sourceStatic) ApiSourceDescribe() PathAPISourceOrReader {
	return s.impl.ApiSourceDescribe()
}

// SetReady is called by a sourceStaticImpl.
func (s *sourceStatic) SetReady(req PathSourceStaticSetReadyReq) PathSourceStaticSetReadyRes {
	req.Res = make(chan PathSourceStaticSetReadyRes)
	select {
	case s.chSourceStaticImplSetReady <- req:
		res := <-req.Res

		if res.err == nil {
			log.Logger.Info("ready: %s", SourceMediaInfo(req.Desc.Medias))
		}

		return res

	case <-s.ctx.Done():
		return PathSourceStaticSetReadyRes{err: fmt.Errorf("terminated")}
	}
}

// SetNotReady is called by a sourceStaticImpl.
func (s *sourceStatic) SetNotReady(req PathSourceStaticSetNotReadyReq) {
	req.res = make(chan struct{})
	select {
	case s.chSourceStaticImplSetNotReady <- req:
		<-req.res
	case <-s.ctx.Done():
	}
}
