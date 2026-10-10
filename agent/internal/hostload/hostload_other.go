//go:build !windows && !linux && !darwin

package hostload

import "errors"

type reader struct{}

func newReader() (*reader, error) {
	return nil, errors.New("host load sampling is not implemented on this platform (Windows, Linux and macOS only)")
}

func (*reader) read() (Sample, error) { return Sample{}, nil }
func (*reader) close()                {}

func logf(string, ...any) {}
