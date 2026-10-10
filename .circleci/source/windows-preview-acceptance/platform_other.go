//go:build !windows

package main

func execute(c config, r *receipt) error { return failure("native_windows_required") }
