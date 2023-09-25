package core

import (
	"net/http"
	"time"

	// start pprof
	_ "net/http/pprof"

	"github.com/liuhengloveyou/livego/common"
	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/httpserv"
)

type pprofParent interface {
}

type pprof struct {
	parent pprofParent

	httpServer *httpserv.WrappedServer
}

func newPPROF(
	address string,
	readTimeout conf.StringDuration,
	parent pprofParent,
) (*pprof, error) {
	pp := &pprof{
		parent: parent,
	}

	network, address := RestrictNetwork("tcp", address)

	var err error
	pp.httpServer, err = httpserv.NewWrappedServer(
		network,
		address,
		time.Duration(readTimeout),
		"",
		"",
		http.DefaultServeMux,
	)
	if err != nil {
		return nil, err
	}

	common.Logger.Info("listener opened on " + address)

	return pp, nil
}

func (pp *pprof) close() {
	common.Logger.Info("listener is closing")
	pp.httpServer.Close()
}
