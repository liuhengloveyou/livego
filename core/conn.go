package core

import "github.com/liuhengloveyou/livego/proto"

type conn struct {
	rtspAddress string
	// runOnConnect        string
	// runOnConnectRestart bool
	// runOnDisconnect     string
}

func newConn(
	rtspAddress string,
	// runOnConnect string,
	// runOnConnectRestart bool,
	// runOnDisconnect string,
) *conn {
	return &conn{
		rtspAddress: rtspAddress,
		// runOnConnect:        runOnConnect,
		// runOnConnectRestart: runOnConnectRestart,
		// runOnDisconnect:     runOnDisconnect,
	}
}

func (c *conn) open(desc proto.ApiPathSourceOrReader) {
	// if c.runOnConnect != "" {
	// _, port, _ := net.SplitHostPort(c.rtspAddress)
	// env := externalcmd.Environment{
	// 	"RTSP_PORT":     port,
	// 	"MTX_CONN_TYPE": desc.Type,
	// 	"MTX_CONN_ID":   desc.ID,
	// }

	// c.onConnectCmd = externalcmd.NewCmd(
	// 	c.externalCmdPool,
	// 	c.runOnConnect,
	// 	c.runOnConnectRestart,
	// 	env,
	// 	func(err error) {
	// 		fmt.Println("runOnConnect command exited: %v", err)
	// 	})
	// }
}

func (c *conn) close(desc proto.ApiPathSourceOrReader) {
	// if c.onConnectCmd != nil {
	// 	c.onConnectCmd.Close()
	// 	fmt.Println("runOnConnect command stopped")
	// }

	// if c.runOnDisconnect != "" {
	// 	fmt.Println("runOnDisconnect command launched")

	// 	_, port, _ := net.SplitHostPort(c.rtspAddress)
	// 	env := externalcmd.Environment{
	// 		"RTSP_PORT":     port,
	// 		"MTX_CONN_TYPE": desc.Type,
	// 		"MTX_CONN_ID":   desc.ID,
	// 	}

	// 	externalcmd.NewCmd(
	// 		c.externalCmdPool,
	// 		c.runOnDisconnect,
	// 		false,
	// 		env,
	// 		nil)
	// }
}
