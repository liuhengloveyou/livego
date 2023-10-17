package core

import "github.com/liuhengloveyou/livego/proto"

// sourceRedirect is a source that redirects to another one.
type sourceRedirect struct{}

// apiSourceDescribe implements source.
func (*sourceRedirect) apiSourceDescribe() proto.ApiPathSourceOrReader {
	return proto.ApiPathSourceOrReader{
		Type: "redirect",
		ID:   "",
	}
}
