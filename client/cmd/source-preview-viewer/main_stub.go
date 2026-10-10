//go:build !linux || android || !cgo

package main

import "os"

func main() { os.Exit(2) } // Only the native same-host Linux renderer is supported.
