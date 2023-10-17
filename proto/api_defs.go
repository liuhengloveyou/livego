package proto

import (
	"time"

	"github.com/google/uuid"

	"github.com/liuhengloveyou/livego/conf"
)

type ApiPathManager interface {
	ApiPathsList() (*ApiPathList, error)
	ApiPathsGet(string) (*ApiPath, error)
}

type ApiHLSManager interface {
	ApiMuxersList() (*ApiHLSMuxerList, error)
	ApiMuxersGet(string) (*ApiHLSMuxer, error)
}

type ApiRTSPServer interface {
	ApiConnsList() (*ApiRTSPConnsList, error)
	ApiConnsGet(uuid.UUID) (*ApiRTSPConn, error)
	ApiSessionsList() (*ApiRTSPSessionList, error)
	ApiSessionsGet(uuid.UUID) (*ApiRTSPSession, error)
	ApiSessionsKick(uuid.UUID) error
}

type ApiRTMPServer interface {
	ApiConnsList() (*ApiRTMPConnList, error)
	ApiConnsGet(uuid.UUID) (*ApiRTMPConn, error)
	ApiConnsKick(uuid.UUID) error
}

type ApiWebRTCManager interface {
	ApiSessionsList() (*ApiWebRTCSessionList, error)
	ApiSessionsGet(uuid.UUID) (*ApiWebRTCSession, error)
	ApiSessionsKick(uuid.UUID) error
}

type ApiSRTServer interface {
	ApiConnsList() (*ApiSRTConnList, error)
	ApiConnsGet(uuid.UUID) (*ApiSRTConn, error)
	ApiConnsKick(uuid.UUID) error
}

type ApiParent interface {
	ApiConfigSet(conf *conf.Conf)
}

type ApiPathConfList struct {
	ItemCount int                  `json:"itemCount"`
	PageCount int                  `json:"pageCount"`
	Items     []*conf.OptionalPath `json:"items"`
}

type ApiPathSourceOrReader struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type ApiPath struct {
	Name          string                  `json:"name"`
	ConfName      string                  `json:"confName"`
	Source        *ApiPathSourceOrReader  `json:"source"`
	Ready         bool                    `json:"ready"`
	ReadyTime     *time.Time              `json:"readyTime"`
	Tracks        []string                `json:"tracks"`
	BytesReceived uint64                  `json:"bytesReceived"`
	Readers       []ApiPathSourceOrReader `json:"readers"`
}

type ApiPathList struct {
	ItemCount int        `json:"itemCount"`
	PageCount int        `json:"pageCount"`
	Items     []*ApiPath `json:"items"`
}

type ApiHLSMuxer struct {
	Path        string    `json:"path"`
	Created     time.Time `json:"created"`
	LastRequest time.Time `json:"lastRequest"`
	BytesSent   uint64    `json:"bytesSent"`
}

type ApiHLSMuxerList struct {
	ItemCount int            `json:"itemCount"`
	PageCount int            `json:"pageCount"`
	Items     []*ApiHLSMuxer `json:"items"`
}

type ApiRTSPConn struct {
	ID            uuid.UUID `json:"id"`
	Created       time.Time `json:"created"`
	RemoteAddr    string    `json:"remoteAddr"`
	BytesReceived uint64    `json:"bytesReceived"`
	BytesSent     uint64    `json:"bytesSent"`
}

type ApiRTSPConnsList struct {
	ItemCount int            `json:"itemCount"`
	PageCount int            `json:"pageCount"`
	Items     []*ApiRTSPConn `json:"items"`
}

type ApiRTMPConnState string

const (
	ApiRTMPConnStateIdle    ApiRTMPConnState = "idle"
	ApiRTMPConnStateRead    ApiRTMPConnState = "read"
	ApiRTMPConnStatePublish ApiRTMPConnState = "publish"
)

type ApiRTMPConn struct {
	ID            uuid.UUID        `json:"id"`
	Created       time.Time        `json:"created"`
	RemoteAddr    string           `json:"remoteAddr"`
	State         ApiRTMPConnState `json:"state"`
	Path          string           `json:"path"`
	BytesReceived uint64           `json:"bytesReceived"`
	BytesSent     uint64           `json:"bytesSent"`
}

type ApiRTMPConnList struct {
	ItemCount int            `json:"itemCount"`
	PageCount int            `json:"pageCount"`
	Items     []*ApiRTMPConn `json:"items"`
}

type ApiRTSPSessionState string

const (
	ApiRTSPSessionStateIdle    ApiRTSPSessionState = "idle"
	ApiRTSPSessionStateRead    ApiRTSPSessionState = "read"
	ApiRTSPSessionStatePublish ApiRTSPSessionState = "publish"
)

type ApiRTSPSession struct {
	ID            uuid.UUID           `json:"id"`
	Created       time.Time           `json:"created"`
	RemoteAddr    string              `json:"remoteAddr"`
	State         ApiRTSPSessionState `json:"state"`
	Path          string              `json:"path"`
	Transport     *string             `json:"transport"`
	BytesReceived uint64              `json:"bytesReceived"`
	BytesSent     uint64              `json:"bytesSent"`
}

type ApiRTSPSessionList struct {
	ItemCount int               `json:"itemCount"`
	PageCount int               `json:"pageCount"`
	Items     []*ApiRTSPSession `json:"items"`
}

type ApiSRTConnState string

const (
	ApiSRTConnStateIdle    ApiSRTConnState = "idle"
	ApiSRTConnStateRead    ApiSRTConnState = "read"
	ApiSRTConnStatePublish ApiSRTConnState = "publish"
)

type ApiSRTConn struct {
	ID            uuid.UUID       `json:"id"`
	Created       time.Time       `json:"created"`
	RemoteAddr    string          `json:"remoteAddr"`
	State         ApiSRTConnState `json:"state"`
	Path          string          `json:"path"`
	BytesReceived uint64          `json:"bytesReceived"`
	BytesSent     uint64          `json:"bytesSent"`
}

type ApiSRTConnList struct {
	ItemCount int           `json:"itemCount"`
	PageCount int           `json:"pageCount"`
	Items     []*ApiSRTConn `json:"items"`
}

type ApiWebRTCSessionState string

const (
	ApiWebRTCSessionStateRead    ApiWebRTCSessionState = "read"
	ApiWebRTCSessionStatePublish ApiWebRTCSessionState = "publish"
)

type ApiWebRTCSession struct {
	ID                        uuid.UUID             `json:"id"`
	Created                   time.Time             `json:"created"`
	RemoteAddr                string                `json:"remoteAddr"`
	PeerConnectionEstablished bool                  `json:"peerConnectionEstablished"`
	LocalCandidate            string                `json:"localCandidate"`
	RemoteCandidate           string                `json:"remoteCandidate"`
	State                     ApiWebRTCSessionState `json:"state"`
	Path                      string                `json:"path"`
	BytesReceived             uint64                `json:"bytesReceived"`
	BytesSent                 uint64                `json:"bytesSent"`
}

type ApiWebRTCSessionList struct {
	ItemCount int                 `json:"itemCount"`
	PageCount int                 `json:"pageCount"`
	Items     []*ApiWebRTCSession `json:"items"`
}
