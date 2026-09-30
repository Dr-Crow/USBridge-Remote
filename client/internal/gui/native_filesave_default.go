//go:build !windows && !darwin && (!linux || android)

package gui

func nativeSaveAvailable() bool { return false }

func nativeSaveFile(string, string) (string, error) {
	return "", nil
}
