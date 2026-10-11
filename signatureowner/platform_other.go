//go:build !windows || !amd64

// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import "context"

func run(context.Context, Spec, []byte) (Result, error) { return Result{}, ErrUnsupported }
