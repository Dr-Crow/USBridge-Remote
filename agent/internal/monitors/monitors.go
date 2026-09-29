// Package monitors enumerates the host's monitors and finds or moves a
// process's windows between them. The streamer benchmark uses it to capture
// and play its test video on one monitor the client picked, for every
// streamer alike (see api/bench.go).
package monitors

import "errors"

// Monitor is one active monitor. ID is its Windows GDI device name
// (`\\.\DISPLAY1`) -- the one name every Windows capture backend reports
// alongside its own identifier (streamhost.CaptureDevice.GDIName).
// Coordinates are physical pixels on the virtual desktop.
type Monitor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Primary bool   `json:"primary"`
}

// ErrUnsupported is returned where monitors can't be enumerated.
var ErrUnsupported = errors.New("monitor enumeration is not supported on this platform")

// Find returns the monitor with the given ID.
func Find(list []Monitor, id string) (Monitor, bool) {
	for _, m := range list {
		if m.ID == id {
			return m, true
		}
	}
	return Monitor{}, false
}
