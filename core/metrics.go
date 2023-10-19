package core

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/liuhengloveyou/livego/common"
	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/httpserv"
	"github.com/liuhengloveyou/livego/proto"
)

func metric(key string, tags string, value int64) string {
	return key + tags + " " + strconv.FormatInt(value, 10) + "\n"
}

type metrics struct {
	httpServer    *httpserv.WrappedServer
	mutex         sync.Mutex
	pathManager   proto.ApiPathManager
	rtspServer    proto.ApiRTSPServer
	rtspsServer   proto.ApiRTSPServer
	rtmpServer    proto.ApiRTMPServer
	webRTCManager proto.ApiWebRTCManager
}

func newMetrics(
	address string,
	readTimeout conf.StringDuration,
) (*metrics, error) {
	m := &metrics{}

	router := gin.New()
	router.SetTrustedProxies(nil) //nolint:errcheck

	router.GET("/metrics", m.onMetrics)

	network, address := common.RestrictNetwork("tcp", address)

	var err error
	m.httpServer, err = httpserv.NewWrappedServer(
		network,
		address,
		time.Duration(readTimeout),
		"",
		"",
		router,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println("listener opened on " + address)

	return m, nil
}

func (m *metrics) close() {
	fmt.Println("listener is closing")
	m.httpServer.Close()
}

func (m *metrics) onMetrics(ctx *gin.Context) {
	out := ""

	data, err := m.pathManager.ApiPathsList()
	if err == nil && len(data.Items) != 0 {
		for _, i := range data.Items {
			var state string
			if i.Ready {
				state = "ready"
			} else {
				state = "notReady"
			}

			tags := "{name=\"" + i.Name + "\",state=\"" + state + "\"}"
			out += metric("paths", tags, 1)
			out += metric("paths_bytes_received", tags, int64(i.BytesReceived))
		}
	} else {
		out += metric("paths", "", 0)
	}

	// if !interfaceIsEmpty(m.hlsManager) {
	// 	data, err := m.hlsManager.apiMuxersList()
	// 	if err == nil && len(data.Items) != 0 {
	// 		for _, i := range data.Items {
	// 			tags := "{name=\"" + i.Path + "\"}"
	// 			out += metric("hls_muxers", tags, 1)
	// 			out += metric("hls_muxers_bytes_sent", tags, int64(i.BytesSent))
	// 		}
	// 	} else {
	// 		out += metric("hls_muxers", "", 0)
	// 		out += metric("hls_muxers_bytes_sent", "", 0)
	// 	}
	// }

	// if !interfaceIsEmpty(m.rtspServer) { //nolint:dupl
	// 	func() {
	// 		data, err := m.rtspServer.apiConnsList()
	// 		if err == nil && len(data.Items) != 0 {
	// 			for _, i := range data.Items {
	// 				tags := "{id=\"" + i.ID.String() + "\"}"
	// 				out += metric("rtsp_conns", tags, 1)
	// 				out += metric("rtsp_conns_bytes_received", tags, int64(i.BytesReceived))
	// 				out += metric("rtsp_conns_bytes_sent", tags, int64(i.BytesSent))
	// 			}
	// 		} else {
	// 			out += metric("rtsp_conns", "", 0)
	// 			out += metric("rtsp_conns_bytes_received", "", 0)
	// 			out += metric("rtsp_conns_bytes_sent", "", 0)
	// 		}
	// 	}()

	// 	func() {
	// 		data, err := m.rtspServer.apiSessionsList()
	// 		if err == nil && len(data.Items) != 0 {
	// 			for _, i := range data.Items {
	// 				tags := "{id=\"" + i.ID.String() + "\",state=\"" + string(i.State) + "\"}"
	// 				out += metric("rtsp_sessions", tags, 1)
	// 				out += metric("rtsp_sessions_bytes_received", tags, int64(i.BytesReceived))
	// 				out += metric("rtsp_sessions_bytes_sent", tags, int64(i.BytesSent))
	// 			}
	// 		} else {
	// 			out += metric("rtsp_sessions", "", 0)
	// 			out += metric("rtsp_sessions_bytes_received", "", 0)
	// 			out += metric("rtsp_sessions_bytes_sent", "", 0)
	// 		}
	// 	}()
	// }

	// if !interfaceIsEmpty(m.rtspsServer) { //nolint:dupl
	// 	func() {
	// 		data, err := m.rtspsServer.apiConnsList()
	// 		if err == nil && len(data.Items) != 0 {
	// 			for _, i := range data.Items {
	// 				tags := "{id=\"" + i.ID.String() + "\"}"
	// 				out += metric("rtsps_conns", tags, 1)
	// 				out += metric("rtsps_conns_bytes_received", tags, int64(i.BytesReceived))
	// 				out += metric("rtsps_conns_bytes_sent", tags, int64(i.BytesSent))
	// 			}
	// 		} else {
	// 			out += metric("rtsps_conns", "", 0)
	// 			out += metric("rtsps_conns_bytes_received", "", 0)
	// 			out += metric("rtsps_conns_bytes_sent", "", 0)
	// 		}
	// 	}()

	// 	func() {
	// 		data, err := m.rtspsServer.apiSessionsList()
	// 		if err == nil && len(data.Items) != 0 {
	// 			for _, i := range data.Items {
	// 				tags := "{id=\"" + i.ID.String() + "\",state=\"" + string(i.State) + "\"}"
	// 				out += metric("rtsps_sessions", tags, 1)
	// 				out += metric("rtsps_sessions_bytes_received", tags, int64(i.BytesReceived))
	// 				out += metric("rtsps_sessions_bytes_sent", tags, int64(i.BytesSent))
	// 			}
	// 		} else {
	// 			out += metric("rtsps_sessions", "", 0)
	// 			out += metric("rtsps_sessions_bytes_received", "", 0)
	// 			out += metric("rtsps_sessions_bytes_sent", "", 0)
	// 		}
	// 	}()
	// }

	// if !interfaceIsEmpty(m.rtmpServer) {
	// 	data, err := m.rtmpServer.apiConnsList()
	// 	if err == nil && len(data.Items) != 0 {
	// 		for _, i := range data.Items {
	// 			tags := "{id=\"" + i.ID.String() + "\",state=\"" + string(i.State) + "\"}"
	// 			out += metric("rtmp_conns", tags, 1)
	// 			out += metric("rtmp_conns_bytes_received", tags, int64(i.BytesReceived))
	// 			out += metric("rtmp_conns_bytes_sent", tags, int64(i.BytesSent))
	// 		}
	// 	} else {
	// 		out += metric("rtmp_conns", "", 0)
	// 		out += metric("rtmp_conns_bytes_received", "", 0)
	// 		out += metric("rtmp_conns_bytes_sent", "", 0)
	// 	}
	// }

	// if !interfaceIsEmpty(m.webRTCManager) {
	// 	data, err := m.webRTCManager.apiSessionsList()
	// 	if err == nil && len(data.Items) != 0 {
	// 		for _, i := range data.Items {
	// 			tags := "{id=\"" + i.ID.String() + "\"}"
	// 			out += metric("webrtc_sessions", tags, 1)
	// 			out += metric("webrtc_sessions_bytes_received", tags, int64(i.BytesReceived))
	// 			out += metric("webrtc_sessions_bytes_sent", tags, int64(i.BytesSent))
	// 		}
	// 	} else {
	// 		out += metric("webrtc_sessions", "", 0)
	// 		out += metric("webrtc_sessions_bytes_received", "", 0)
	// 		out += metric("webrtc_sessions_bytes_sent", "", 0)
	// 	}
	// }

	ctx.Writer.WriteHeader(http.StatusOK)
	io.WriteString(ctx.Writer, out) //nolint:errcheck
}

// pathManagerSet is called by pathManager.
func (m *metrics) pathManagerSet(s proto.ApiPathManager) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	// m.pathManager = s
}

// setRTSPServer is called by rtspServer (plain).
func (m *metrics) setRTSPServer(s proto.ApiRTSPServer) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.rtspServer = s
}

// setRTSPSServer is called by rtspServer (tls).
func (m *metrics) setRTSPSServer(s proto.ApiRTSPServer) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.rtspsServer = s
}

// rtmpServerSet is called by rtmpServer.
func (m *metrics) rtmpServerSet(s proto.ApiRTMPServer) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.rtmpServer = s
}

// webRTCManagerSet is called by webRTCManager.
func (m *metrics) webRTCManagerSet(s proto.ApiWebRTCManager) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.webRTCManager = s
}
