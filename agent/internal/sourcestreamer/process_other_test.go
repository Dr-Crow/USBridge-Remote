//go:build !windows

package sourcestreamer

func platformSourceHelper(string) bool { return false }
