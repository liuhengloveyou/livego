package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/bluenviron/gortsplib/v4/pkg/auth"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/headers"
	"github.com/bluenviron/gortsplib/v4/pkg/url"
	"github.com/google/uuid"

	"github.com/liuhengloveyou/livego/conf"
)

func sha256Base64(in string) string {
	h := sha256.New()
	h.Write([]byte(in))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func checkCredential(right string, guess string) bool {
	if strings.HasPrefix(right, "sha256:") {
		return right[len("sha256:"):] == sha256Base64(guess)
	}

	return right == guess
}

type errAuthentication struct {
	message string
}

// Error implements the error interface.
func (e *errAuthentication) Error() string {
	return "authentication failed: " + e.message
}

type authProtocol string

const (
	authProtocolRTSP   authProtocol = "rtsp"
	authProtocolRTMP   authProtocol = "rtmp"
	authProtocolHLS    authProtocol = "hls"
	AuthProtocolWebRTC authProtocol = "webrtc"
	authProtocolSRT    authProtocol = "srt"
)

type AuthCredentials struct {
	Query       string
	Ip          net.IP
	User        string
	Pass        string
	Proto       authProtocol
	ID          *uuid.UUID
	RtspRequest *base.Request
	RtspBaseURL *url.URL
	RtspNonce   string
}

func doExternalAuthentication(
	ur string,
	path string,
	publish bool,
	credentials AuthCredentials,
) error {
	enc, _ := json.Marshal(struct {
		IP       string     `json:"ip"`
		User     string     `json:"user"`
		Password string     `json:"password"`
		Path     string     `json:"path"`
		Protocol string     `json:"protocol"`
		ID       *uuid.UUID `json:"id"`
		Action   string     `json:"action"`
		Query    string     `json:"query"`
	}{
		IP:       credentials.Ip.String(),
		User:     credentials.User,
		Password: credentials.Pass,
		Path:     path,
		Protocol: string(credentials.Proto),
		ID:       credentials.ID,
		Action: func() string {
			if publish {
				return "publish"
			}
			return "read"
		}(),
		Query: credentials.Query,
	})
	res, err := http.Post(ur, "application/json", bytes.NewReader(enc))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		if resBody, err := io.ReadAll(res.Body); err == nil && len(resBody) != 0 {
			return fmt.Errorf("server replied with code %d: %s", res.StatusCode, string(resBody))
		}
		return fmt.Errorf("server replied with code %d", res.StatusCode)
	}

	return nil
}

func doAuthentication(
	externalAuthenticationURL string,
	rtspAuthMethods conf.AuthMethods,
	pathName string,
	pathConf *conf.PathConf,
	publish bool,
	credentials AuthCredentials,
) error {
	var rtspAuth headers.Authorization
	if credentials.RtspRequest != nil {
		err := rtspAuth.Unmarshal(credentials.RtspRequest.Header["Authorization"])
		if err == nil && rtspAuth.Method == headers.AuthBasic {
			credentials.User = rtspAuth.BasicUser
			credentials.Pass = rtspAuth.BasicPass
		}
	}

	if externalAuthenticationURL != "" {
		err := doExternalAuthentication(
			externalAuthenticationURL,
			pathName,
			publish,
			credentials,
		)
		if err != nil {
			return &errAuthentication{message: fmt.Sprintf("external authentication failed: %s", err)}
		}
	}

	var pathIPs conf.IPsOrCIDRs
	var pathUser string
	var pathPass string

	if publish {
		pathIPs = pathConf.PublishIPs
		pathUser = string(pathConf.PublishUser)
		pathPass = string(pathConf.PublishPass)
	} else {
		pathIPs = pathConf.ReadIPs
		pathUser = string(pathConf.ReadUser)
		pathPass = string(pathConf.ReadPass)
	}

	if pathIPs != nil {
		if !ipEqualOrInRange(credentials.Ip, pathIPs) {
			return &errAuthentication{message: fmt.Sprintf("IP %s not allowed", credentials.Ip)}
		}
	}

	if pathUser != "" {
		if credentials.RtspRequest != nil && rtspAuth.Method == headers.AuthDigest {
			err := auth.Validate(
				credentials.RtspRequest,
				pathUser,
				pathPass,
				credentials.RtspBaseURL,
				rtspAuthMethods,
				"IPCAM",
				credentials.RtspNonce)
			if err != nil {
				return &errAuthentication{message: err.Error()}
			}
		} else if !checkCredential(pathUser, credentials.User) ||
			!checkCredential(pathPass, credentials.Pass) {
			return &errAuthentication{message: "invalid credentials"}
		}
	}

	return nil
}
