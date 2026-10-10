//go:build (!linux && !windows) || android || !cgo

package main

import "os"

func main() { os.Exit(2) } // Native same-host renderers require Linux or Windows with cgo.
