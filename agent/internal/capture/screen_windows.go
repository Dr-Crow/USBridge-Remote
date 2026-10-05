//go:build windows

package capture

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/kbinani/screenshot"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/displaypower"
	"usbridge_agent/internal/monitors"
	"usbridge_agent/internal/streamhost"
)

type Service struct {
	devices streamhost.CaptureDeviceLister
}

func New(devices streamhost.CaptureDeviceLister) *Service { return &Service{devices: devices} }

// SetDevices re-points device correlation at a different backend -- needed
// after App.SetStreamBackend swaps streamhost.Backend, since New captured
// the old one by value and nothing else here re-reads it.
func (s *Service) SetDevices(devices streamhost.CaptureDeviceLister) { s.devices = devices }

func (s *Service) Snapshot() (*api.ScreenSnapshot, error) {
	if screenshot.NumActiveDisplays() == 0 {
		return nil, fmt.Errorf("no active displays")
	}
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &api.ScreenSnapshot{
		Format:      "png-base64",
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
		ImageBase64: base64.StdEncoding.EncodeToString(buf.Bytes()),
		Timestamp:   time.Now().Format(time.RFC3339Nano),
	}, nil
}

// Devices reports capturable monitors. Sunshine's Windows display_device
// backend identifies each monitor by a stable device_id (an EDID+instance
// derived string, e.g. "{26932b0f-...}") rather than a simple index — that
// id is what its output_name config key actually expects, and it can only be
// read back from Sunshine's own log (see sunshine.WindowsDisplayDevices),
// never predicted from Windows' own display enumeration order. Without this,
// picking "Display 1" here previously wrote a meaningless literal string
// into output_name, which Sunshine silently ignored, making monitor
// switching on Windows a no-op.
//
// Falls back to kbinani/screenshot-based enumeration ("display:<index>")
// only before Sunshine has ever logged its device list this session (e.g.
// prior to its first launch) — that fallback's index does NOT reliably line
// up with Sunshine's own selection, so switching via it may not work until
// Sunshine has started at least once.
func (s *Service) Devices() []api.VideoDeviceInfo {
	var devices []streamhost.CaptureDevice
	if s.devices != nil {
		devices = s.devices.ListCaptureDevices()
	}
	if len(devices) > 0 {
		out := make([]api.VideoDeviceInfo, 0, len(devices))
		gdi := make([]string, 0, len(devices))
		for i, d := range devices {
			name := d.DisplayName
			out = append(out, api.VideoDeviceInfo{
				Path:           fmt.Sprintf("winid:%s", d.OutputName),
				Name:           fmt.Sprintf("%s (%dx%d)", name, d.Width, d.Height),
				Bus:            "dxgi",
				Index:          i,
				Connected:      true,
				SupportedModes: ModesForResolution(d.Width, d.Height),
			})
			gdi = append(gdi, d.GDIName)
		}
		return withMonitorPower(out, gdi)
	}

	out := make([]api.VideoDeviceInfo, 0, screenshot.NumActiveDisplays())
	for i := 0; i < screenshot.NumActiveDisplays(); i++ {
		bounds := screenshot.GetDisplayBounds(i)
		out = append(out, api.VideoDeviceInfo{
			Path:           fmt.Sprintf("display:%d", i),
			Name:           fmt.Sprintf("Display %d (%dx%d)", i, bounds.Dx(), bounds.Dy()),
			Bus:            "dxgi",
			Index:          i,
			Connected:      true,
			SupportedModes: GetDisplayModes(i),
		})
	}
	// kbinani/screenshot indexes displays in EnumDisplayMonitors order.
	return withMonitorPower(out, monitors.GDINames())
}

// withMonitorPower adds the on/off switch (displaypower) to the listed monitors -- gdi[i]
// is out[i]'s GDI name, "" when unknown -- and lists the switched-off monitors after them,
// so the client can switch them back on.
func withMonitorPower(out []api.VideoDeviceInfo, gdi []string) []api.VideoDeviceInfo {
	all, err := displaypower.List()
	if err != nil {
		return out
	}
	listed := map[string]bool{}
	for i := range out {
		if i >= len(gdi) || gdi[i] == "" {
			continue
		}
		for _, m := range all {
			if m.Active && strings.EqualFold(m.GDIName, gdi[i]) {
				on := true
				out[i].MonitorID, out[i].Enabled = m.ID, &on
				listed[m.ID] = true
				// The fallback's "Display N (WxH)" says nothing; the EDID name does.
				if strings.HasPrefix(out[i].Name, "Display ") && m.Name != "" {
					if j := strings.Index(out[i].Name, " ("); j >= 0 {
						out[i].Name = m.Name + out[i].Name[j:]
					}
				}
			}
		}
	}
	for _, m := range all {
		if m.Active || listed[m.ID] {
			continue
		}
		off := false
		out = append(out, api.VideoDeviceInfo{
			Path:      "monitor:" + m.ID,
			Name:      m.Name + " (off)",
			Bus:       "dxgi",
			Index:     len(out),
			Connected: false,
			MonitorID: m.ID,
			Enabled:   &off,
		})
	}
	return out
}
