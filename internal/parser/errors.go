package parser

// UnsupportedError marks a node that is valid but cannot be represented on the
// target platform. It carries enough context for the preview/report endpoints.
type UnsupportedError struct {
	Scheme string
	Name   string
	Reason string
}

func (e *UnsupportedError) Error() string { return e.Reason }

func unsupported(scheme, name, reason string) error {
	return &UnsupportedError{Scheme: scheme, Name: name, Reason: reason}
}
