package core

// sourceRedirect is a source that redirects to another one.
type sourceRedirect struct{}

// apiSourceDescribe implements source.
func (*sourceRedirect) apiSourceDescribe() apiPathSourceOrReader {
	return apiPathSourceOrReader{
		Type: "redirect",
		ID:   "",
	}
}
