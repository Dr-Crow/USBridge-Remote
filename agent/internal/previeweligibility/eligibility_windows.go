//go:build windows

package previeweligibility

import (
	"syscall"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var getWindowStation = user32.NewProc("GetProcessWindowStation")
var getUserObjectInformation = user32.NewProc("GetUserObjectInformationW")

type userObjectFlags struct {
	Inherit, Reserved int32
	Flags             uint32
}

// Observe queries the current process token, never an impersonation or linked
// token. Its only acquired resource is a TOKEN_QUERY handle. The window-station
// handle is borrowed and must not be closed. No API here changes identity,
// privileges, session, desktop, window station, or security settings.
func Observe() Snapshot {
	var s Snapshot
	token, err := syscall.OpenCurrentProcessToken()
	if err == nil {
		var elevated, session, n uint32
		elevationErr := syscall.GetTokenInformation(token, syscall.TokenElevation, (*byte)(unsafe.Pointer(&elevated)), 4, &n)
		elevationKnown := elevationErr == nil && n == 4 && elevated <= 1
		n = 0
		sessionErr := syscall.GetTokenInformation(token, syscall.TokenSessionId, (*byte)(unsafe.Pointer(&session)), 4, &n)
		sessionKnown := sessionErr == nil && n == 4
		s.TokenQueried = elevationKnown && sessionKnown
		s.NotElevated = elevationKnown && elevated == 0
		s.InteractiveSession = sessionKnown && session != 0
		s.TokenClosed = token.Close() == nil
	}
	station, _, _ := getWindowStation.Call()
	if station != 0 {
		var flags userObjectFlags
		var n uint32
		const uoiFlags = 1
		const wsfVisible = 1
		ok, _, _ := getUserObjectInformation.Call(station, uoiFlags, uintptr(unsafe.Pointer(&flags)), unsafe.Sizeof(flags), uintptr(unsafe.Pointer(&n)))
		s.WindowStationQueried = ok != 0 && n == uint32(unsafe.Sizeof(flags)) && unsafe.Sizeof(flags) == 12
		s.WindowStationVisible = s.WindowStationQueried && flags.Flags&wsfVisible != 0
	}
	return s
}
