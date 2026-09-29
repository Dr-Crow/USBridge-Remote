//go:build !linux

package input

// CurrentKeyboardLayout: see layout_linux.go.
func CurrentKeyboardLayout() (string, error) { return "", ErrLayoutUnsupported }

// SetKeyboardLayout: see layout_linux.go.
func SetKeyboardLayout(string) (string, error) { return "", ErrLayoutUnsupported }
