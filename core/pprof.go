package core

import (
	"net/http"
	"time"

	// start pprof
	_ "net/http/pprof"

	"github.com/liuhengloveyou/livego/conf"
	"github.com/liuhengloveyou/livego/httpserv"
)

type pprof struct {
	httpServer *httpserv.WrappedServer
}

func newPPROF(
	address string,
	readTimeout conf.StringDuration,
) (*pprof, error) {
	pp := &pprof{}

	network, address := restrictNetwork("tcp", address)

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

	return pp, nil
}

func (pp *pprof) close() {
	pp.httpServer.Close()
}
