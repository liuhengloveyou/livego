package core

// sourceRedirect is a source that redirects to another one.
type SourceRedirect struct{}

// ApiSourceDescribe implements source.
func (*SourceRedirect) ApiSourceDescribe() PathAPISourceOrReader {
	return PathAPISourceOrReader{
		Type: "redirect",
		ID:   "",
	}
}
