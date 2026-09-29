//go:build !windows

package hostload

import "errors"

type reader struct{}

func newReader() (*reader, error) { return nil, errors.New("not implemented on this platform") }

func (*reader) read() (Sample, error) { return Sample{}, nil }
func (*reader) close()                {}

func logf(string, ...any) {}
