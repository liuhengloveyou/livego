// Package core contains the main struct of the software.
package core

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reflect"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/gin-gonic/gin"

	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/externalcmd"
	"github.com/liuhengloveyou/livego/log"
	"github.com/liuhengloveyou/livego/rlimit"
)

// Core is an instance of mediamtx.
type Core struct {
	ctx             context.Context
	ctxCancel       func()
	conf            *conf.Conf
	confFound       bool
	externalCmdPool *externalcmd.Pool
	metrics         *Metrics
	pprof           *pprof
	pathManager     *PathManager
	rtspServer      *rtspServer
	rtspsServer     *rtspServer
	rtmpServer      *rtmpServer
	rtmpsServer     *rtmpServer
	webRTCManager   *WebRTCManager
	srtServer       *srtServer
	api             *api

	// in
	chAPIConfigSet chan *conf.Conf

	// out
	done chan struct{}
}

// New allocates a core.
func New(args []string) (*Core, bool) {
	ctx, ctxCancel := context.WithCancel(context.Background())

	p := &Core{
		ctx:            ctx,
		ctxCancel:      ctxCancel,
		chAPIConfigSet: make(chan *conf.Conf),
		done:           make(chan struct{}),
	}

	var err error
	p.conf, p.confFound, err = conf.Load("livego.yml")
	if err != nil {
		fmt.Printf("ERR: %s\n", err)
		return nil, false
	}

	err = p.createResources(true)
	if err != nil {
		log.Logger.Error("%s", err)

		p.closeResources(nil, false)
		return nil, false
	}

	go p.run()

	return p, true
}

// Close closes Core and waits for all goroutines to return.
func (p *Core) Close() {
	p.ctxCancel()
	<-p.done
}

// Wait waits for the Core to exit.
func (p *Core) Wait() {
	<-p.done
}

func (p *Core) run() {
	defer close(p.done)

	// confChanged := func() chan struct{} {
	// 	if p.confWatcher != nil {
	// 		return p.confWatcher.Watch()
	// 	}
	// 	return make(chan struct{})
	// }()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

outer:
	for {
		select {
		// case <-confChanged:
		// 	log.Logger.Info( "reloading configuration (file changed)")

		// 	newConf, _, err := conf.Load("livego.yml")
		// 	if err != nil {
		// 		p.Log(logger.Error, "%s", err)
		// 		break outer
		// 	}

		// 	err = p.reloadConf(newConf, false)
		// 	if err != nil {
		// 		p.Log(logger.Error, "%s", err)
		// 		break outer
		// 	}

		case newConf := <-p.chAPIConfigSet:
			log.Logger.Info("reloading configuration (API request)")

			err := p.reloadConf(newConf, true)
			if err != nil {
				log.Logger.Error("%s", err)
				break outer
			}

		case <-interrupt:
			log.Logger.Info("shutting down gracefully")
			break outer

		case <-p.ctx.Done():
			break outer
		}
	}

	p.ctxCancel()

	p.closeResources(nil, false)
}

func (p *Core) createResources(initial bool) error {
	var err error

	if initial {
		if !p.confFound {
			log.Logger.Warn("configuration file not found, using an empty configuration")
		}

		// on Linux, try to raise the number of file descriptors that can be opened
		// to allow the maximum possible number of clients
		// do not check for errors
		rlimit.Raise() //nolint:errcheck

		gin.SetMode(gin.ReleaseMode)

		p.externalCmdPool = externalcmd.NewPool()
	}

	if p.conf.Metrics &&
		p.metrics == nil {
		p.metrics, err = newMetrics(
			p.conf.MetricsAddress,
			p.conf.ReadTimeout,
			p,
		)
		if err != nil {
			return err
		}
	}

	if p.conf.PPROF &&
		p.pprof == nil {
		p.pprof, err = newPPROF(
			p.conf.PPROFAddress,
			p.conf.ReadTimeout,
			p,
		)
		if err != nil {
			return err
		}
	}

	if p.pathManager == nil {
		p.pathManager = NewPathManager(
			p.conf.ExternalAuthenticationURL,
			p.conf.RTSPAddress,
			p.conf.AuthMethods,
			p.conf.ReadTimeout,
			p.conf.WriteTimeout,
			p.conf.WriteQueueSize,
			p.conf.UDPMaxPayloadSize,
			p.conf.Paths,
			p.externalCmdPool,
			p.metrics,
		)
	}

	if p.conf.RTSP &&
		(p.conf.Encryption == conf.EncryptionNo ||
			p.conf.Encryption == conf.EncryptionOptional) &&
		p.rtspServer == nil {
		_, useUDP := p.conf.Protocols[conf.Protocol(gortsplib.TransportUDP)]
		_, useMulticast := p.conf.Protocols[conf.Protocol(gortsplib.TransportUDPMulticast)]

		p.rtspServer, err = newRTSPServer(
			p.conf.RTSPAddress,
			p.conf.AuthMethods,
			p.conf.ReadTimeout,
			p.conf.WriteTimeout,
			p.conf.WriteQueueSize,
			useUDP,
			useMulticast,
			p.conf.RTPAddress,
			p.conf.RTCPAddress,
			p.conf.MulticastIPRange,
			p.conf.MulticastRTPPort,
			p.conf.MulticastRTCPPort,
			false,
			"",
			"",
			p.conf.RTSPAddress,
			p.conf.Protocols,
			p.conf.RunOnConnect,
			p.conf.RunOnConnectRestart,
			p.externalCmdPool,
			p.metrics,
			p.pathManager,
			p,
		)
		if err != nil {
			return err
		}
	}

	if p.conf.RTSP &&
		(p.conf.Encryption == conf.EncryptionStrict ||
			p.conf.Encryption == conf.EncryptionOptional) &&
		p.rtspsServer == nil {
		p.rtspsServer, err = newRTSPServer(
			p.conf.RTSPSAddress,
			p.conf.AuthMethods,
			p.conf.ReadTimeout,
			p.conf.WriteTimeout,
			p.conf.WriteQueueSize,
			false,
			false,
			"",
			"",
			"",
			0,
			0,
			true,
			p.conf.ServerCert,
			p.conf.ServerKey,
			p.conf.RTSPAddress,
			p.conf.Protocols,
			p.conf.RunOnConnect,
			p.conf.RunOnConnectRestart,
			p.externalCmdPool,
			p.metrics,
			p.pathManager,
			p,
		)
		if err != nil {
			return err
		}
	}

	if p.conf.RTMP &&
		(p.conf.RTMPEncryption == conf.EncryptionNo ||
			p.conf.RTMPEncryption == conf.EncryptionOptional) &&
		p.rtmpServer == nil {
		p.rtmpServer, err = newRTMPServer(
			p.conf.RTMPAddress,
			p.conf.ReadTimeout,
			p.conf.WriteTimeout,
			p.conf.WriteQueueSize,
			false,
			"",
			"",
			p.conf.RTSPAddress,
			p.conf.RunOnConnect,
			p.conf.RunOnConnectRestart,
			p.externalCmdPool,
			p.metrics,
			p.pathManager,
			p,
		)
		if err != nil {
			return err
		}
	}

	if p.conf.RTMP &&
		(p.conf.RTMPEncryption == conf.EncryptionStrict ||
			p.conf.RTMPEncryption == conf.EncryptionOptional) &&
		p.rtmpsServer == nil {
		p.rtmpsServer, err = newRTMPServer(
			p.conf.RTMPSAddress,
			p.conf.ReadTimeout,
			p.conf.WriteTimeout,
			p.conf.WriteQueueSize,
			true,
			p.conf.RTMPServerCert,
			p.conf.RTMPServerKey,
			p.conf.RTSPAddress,
			p.conf.RunOnConnect,
			p.conf.RunOnConnectRestart,
			p.externalCmdPool,
			p.metrics,
			p.pathManager,
			p,
		)
		if err != nil {
			return err
		}
	}

	if p.conf.WebRTC && p.webRTCManager == nil {
		if p.webRTCManager, err = NewWebRTCManager(
			p.conf.WebRTCAddress,
			p.conf.WebRTCEncryption,
			p.conf.WebRTCServerKey,
			p.conf.WebRTCServerCert,
			p.conf.WebRTCAllowOrigin,
			p.conf.WebRTCTrustedProxies,
			p.conf.WebRTCICEServers2,
			p.conf.ReadTimeout,
			p.conf.WriteQueueSize,
			p.conf.WebRTCICEHostNAT1To1IPs,
			p.conf.WebRTCICEUDPMuxAddress,
			p.conf.WebRTCICETCPMuxAddress,
			p.pathManager,
			p.metrics,
		); err != nil {
			return err
		}
	}

	if p.conf.SRT &&
		p.srtServer == nil {
		p.srtServer, err = newSRTServer(
			p.conf.SRTAddress,
			p.conf.ReadTimeout,
			p.conf.WriteTimeout,
			p.conf.WriteQueueSize,
			p.conf.UDPMaxPayloadSize,
			p.externalCmdPool,
			p.pathManager,
		)
		if err != nil {
			return err
		}
	}

	if p.conf.API &&
		p.api == nil {
		p.api, err = newAPI(
			p.conf.APIAddress,
			p.conf.ReadTimeout,
			p.conf,
			p.pathManager,
			p.rtspServer,
			p.rtspsServer,
			p.rtmpServer,
			p.rtmpsServer,
			p.webRTCManager,
			p.srtServer,
			p,
		)
		if err != nil {
			return err
		}
	}

	// if initial && p.confFound {
	// 	p.confWatcher, err = confwatcher.New("livego.yml")
	// 	if err != nil {
	// 		return err
	// 	}
	// }

	return nil
}

func (p *Core) closeResources(newConf *conf.Conf, calledByAPI bool) {
	closeMetrics := newConf == nil ||
		newConf.Metrics != p.conf.Metrics ||
		newConf.MetricsAddress != p.conf.MetricsAddress ||
		newConf.ReadTimeout != p.conf.ReadTimeout

	closePPROF := newConf == nil ||
		newConf.PPROF != p.conf.PPROF ||
		newConf.PPROFAddress != p.conf.PPROFAddress ||
		newConf.ReadTimeout != p.conf.ReadTimeout

	closePathManager := newConf == nil ||
		newConf.ExternalAuthenticationURL != p.conf.ExternalAuthenticationURL ||
		newConf.RTSPAddress != p.conf.RTSPAddress ||
		!reflect.DeepEqual(newConf.AuthMethods, p.conf.AuthMethods) ||
		newConf.ReadTimeout != p.conf.ReadTimeout ||
		newConf.WriteTimeout != p.conf.WriteTimeout ||
		newConf.WriteQueueSize != p.conf.WriteQueueSize ||
		newConf.UDPMaxPayloadSize != p.conf.UDPMaxPayloadSize ||
		closeMetrics
	if !closePathManager && !reflect.DeepEqual(newConf.Paths, p.conf.Paths) {
		p.pathManager.confReload(newConf.Paths)
	}

	closeRTSPServer := newConf == nil ||
		newConf.RTSP != p.conf.RTSP ||
		newConf.Encryption != p.conf.Encryption ||
		newConf.RTSPAddress != p.conf.RTSPAddress ||
		!reflect.DeepEqual(newConf.AuthMethods, p.conf.AuthMethods) ||
		newConf.ReadTimeout != p.conf.ReadTimeout ||
		newConf.WriteTimeout != p.conf.WriteTimeout ||
		newConf.WriteQueueSize != p.conf.WriteQueueSize ||
		!reflect.DeepEqual(newConf.Protocols, p.conf.Protocols) ||
		newConf.RTPAddress != p.conf.RTPAddress ||
		newConf.RTCPAddress != p.conf.RTCPAddress ||
		newConf.MulticastIPRange != p.conf.MulticastIPRange ||
		newConf.MulticastRTPPort != p.conf.MulticastRTPPort ||
		newConf.MulticastRTCPPort != p.conf.MulticastRTCPPort ||
		newConf.RTSPAddress != p.conf.RTSPAddress ||
		!reflect.DeepEqual(newConf.Protocols, p.conf.Protocols) ||
		newConf.RunOnConnect != p.conf.RunOnConnect ||
		newConf.RunOnConnectRestart != p.conf.RunOnConnectRestart ||
		closeMetrics ||
		closePathManager

	closeRTSPSServer := newConf == nil ||
		newConf.RTSP != p.conf.RTSP ||
		newConf.Encryption != p.conf.Encryption ||
		newConf.RTSPSAddress != p.conf.RTSPSAddress ||
		!reflect.DeepEqual(newConf.AuthMethods, p.conf.AuthMethods) ||
		newConf.ReadTimeout != p.conf.ReadTimeout ||
		newConf.WriteTimeout != p.conf.WriteTimeout ||
		newConf.WriteQueueSize != p.conf.WriteQueueSize ||
		newConf.ServerCert != p.conf.ServerCert ||
		newConf.ServerKey != p.conf.ServerKey ||
		newConf.RTSPAddress != p.conf.RTSPAddress ||
		!reflect.DeepEqual(newConf.Protocols, p.conf.Protocols) ||
		newConf.RunOnConnect != p.conf.RunOnConnect ||
		newConf.RunOnConnectRestart != p.conf.RunOnConnectRestart ||
		closeMetrics ||
		closePathManager

	closeRTMPServer := newConf == nil ||
		newConf.RTMP != p.conf.RTMP ||
		newConf.RTMPEncryption != p.conf.RTMPEncryption ||
		newConf.RTMPAddress != p.conf.RTMPAddress ||
		newConf.ReadTimeout != p.conf.ReadTimeout ||
		newConf.WriteTimeout != p.conf.WriteTimeout ||
		newConf.WriteQueueSize != p.conf.WriteQueueSize ||
		newConf.RTSPAddress != p.conf.RTSPAddress ||
		newConf.RunOnConnect != p.conf.RunOnConnect ||
		newConf.RunOnConnectRestart != p.conf.RunOnConnectRestart ||
		closeMetrics ||
		closePathManager

	closeRTMPSServer := newConf == nil ||
		newConf.RTMP != p.conf.RTMP ||
		newConf.RTMPEncryption != p.conf.RTMPEncryption ||
		newConf.RTMPSAddress != p.conf.RTMPSAddress ||
		newConf.ReadTimeout != p.conf.ReadTimeout ||
		newConf.WriteTimeout != p.conf.WriteTimeout ||
		newConf.WriteQueueSize != p.conf.WriteQueueSize ||
		newConf.RTMPServerCert != p.conf.RTMPServerCert ||
		newConf.RTMPServerKey != p.conf.RTMPServerKey ||
		newConf.RTSPAddress != p.conf.RTSPAddress ||
		newConf.RunOnConnect != p.conf.RunOnConnect ||
		newConf.RunOnConnectRestart != p.conf.RunOnConnectRestart ||
		closeMetrics ||
		closePathManager

	closeWebRTCManager := newConf == nil ||
		newConf.WebRTC != p.conf.WebRTC ||
		newConf.WebRTCAddress != p.conf.WebRTCAddress ||
		newConf.WebRTCEncryption != p.conf.WebRTCEncryption ||
		newConf.WebRTCServerKey != p.conf.WebRTCServerKey ||
		newConf.WebRTCServerCert != p.conf.WebRTCServerCert ||
		newConf.WebRTCAllowOrigin != p.conf.WebRTCAllowOrigin ||
		!reflect.DeepEqual(newConf.WebRTCTrustedProxies, p.conf.WebRTCTrustedProxies) ||
		!reflect.DeepEqual(newConf.WebRTCICEServers2, p.conf.WebRTCICEServers2) ||
		newConf.ReadTimeout != p.conf.ReadTimeout ||
		newConf.WriteQueueSize != p.conf.WriteQueueSize ||
		!reflect.DeepEqual(newConf.WebRTCICEHostNAT1To1IPs, p.conf.WebRTCICEHostNAT1To1IPs) ||
		newConf.WebRTCICEUDPMuxAddress != p.conf.WebRTCICEUDPMuxAddress ||
		newConf.WebRTCICETCPMuxAddress != p.conf.WebRTCICETCPMuxAddress ||
		closeMetrics ||
		closePathManager

	closeSRTServer := newConf == nil ||
		newConf.SRT != p.conf.SRT ||
		newConf.SRTAddress != p.conf.SRTAddress ||
		newConf.ReadTimeout != p.conf.ReadTimeout ||
		newConf.WriteTimeout != p.conf.WriteTimeout ||
		newConf.WriteQueueSize != p.conf.WriteQueueSize ||
		newConf.UDPMaxPayloadSize != p.conf.UDPMaxPayloadSize ||
		closePathManager

	closeAPI := newConf == nil ||
		newConf.API != p.conf.API ||
		newConf.APIAddress != p.conf.APIAddress ||
		newConf.ReadTimeout != p.conf.ReadTimeout ||
		closePathManager ||
		closeRTSPServer ||
		closeRTSPSServer ||
		closeRTMPServer ||
		closeWebRTCManager ||
		closeSRTServer

	// if newConf == nil && p.confWatcher != nil {
	// 	p.confWatcher.Close()
	// 	p.confWatcher = nil
	// }

	if p.api != nil {
		if closeAPI {
			p.api.close()
			p.api = nil
		} else if !calledByAPI { // avoid a loop
			p.api.confReload(newConf)
		}
	}

	if closeSRTServer && p.srtServer != nil {
		p.srtServer.close()
		p.srtServer = nil
	}

	if closeWebRTCManager && p.webRTCManager != nil {
		p.webRTCManager.close()
		p.webRTCManager = nil
	}

	if closeRTMPSServer && p.rtmpsServer != nil {
		p.rtmpsServer.close()
		p.rtmpsServer = nil
	}

	if closeRTMPServer && p.rtmpServer != nil {
		p.rtmpServer.close()
		p.rtmpServer = nil
	}

	if closeRTSPSServer && p.rtspsServer != nil {
		p.rtspsServer.close()
		p.rtspsServer = nil
	}

	if closeRTSPServer && p.rtspServer != nil {
		p.rtspServer.close()
		p.rtspServer = nil
	}

	if closePathManager && p.pathManager != nil {
		p.pathManager.close()
		p.pathManager = nil
	}

	if closePPROF && p.pprof != nil {
		p.pprof.close()
		p.pprof = nil
	}

	if closeMetrics && p.metrics != nil {
		p.metrics.close()
		p.metrics = nil
	}

	if newConf == nil && p.externalCmdPool != nil {
		log.Logger.Info("waiting for external commands")
		p.externalCmdPool.Close()
	}
}

func (p *Core) reloadConf(newConf *conf.Conf, calledByAPI bool) error {
	p.closeResources(newConf, calledByAPI)
	p.conf = newConf
	return p.createResources(false)
}

// apiConfigSet is called by api.
func (p *Core) apiConfigSet(conf *conf.Conf) {
	select {
	case p.chAPIConfigSet <- conf:
	case <-p.ctx.Done():
	}
}
