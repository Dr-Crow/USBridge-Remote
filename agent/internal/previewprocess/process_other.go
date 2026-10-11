//go:build !windows

package previewprocess

import "context"

func start(context.Context, Spec) (*Process, error) { return nil, ErrUnsupported }
