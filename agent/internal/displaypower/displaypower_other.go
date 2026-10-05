//go:build !windows

package displaypower

// List reports ErrUnsupported off Windows.
func List() ([]Monitor, error) { return nil, ErrUnsupported }

// SetEnabled reports ErrUnsupported off Windows.
func SetEnabled(id string, on bool) error { return ErrUnsupported }
