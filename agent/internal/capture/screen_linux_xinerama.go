//go:build linux

package capture

// Direct Xinerama/XShm screen capture, bypassing kbinani/screenshot's own
// Capture() dispatcher entirely on Linux.
//
// Root cause: that library's Capture() (nix_dbus_available.go) branches
// purely on os.Getenv("XDG_SESSION_TYPE") -- "wayland" always routes to
// captureDbus, which calls org.freedesktop.portal.Screenshot.Screenshot
// and blocks on a dbus signal with NO timeout of its own. That portal call
// requires a human to click "Share" in an interactive KDE dialog; on an
// unattended/remote session nobody is there to answer it, and the whole
// MCP request (screen.get_image, and click_at/double_click_at's own
// before/after diff) hung for 30-45s before the client's own proxy
// timeout cut it off, every single time -- confirmed live against this
// exact agent.
//
// But XWayland capture via plain Xinerama/XShm (the NON-Wayland branch,
// captureXinerama in the same library) works perfectly fine on this same
// machine/session -- confirmed live: NumActiveDisplays()/GetDisplayBounds
// (also in that library, and NOT gated by the same env check) already
// succeed, and capturing directly via Xinerama below returns a correct,
// non-blank image. So the fix isn't "handle Wayland properly" -- it's
// "don't trust the library's own broken env-var heuristic that a Wayland
// session can't be captured via XWayland at all". captureXinerama itself
// is unexported in the vendored library, so this is a straight port of
// it (same xgb/xinerama/shm calls, already transitive deps of that
// library) rather than a call into it.
import (
	"fmt"
	"image"
	"image/color"

	"github.com/gen2brain/shm"
	"github.com/jezek/xgb"
	mshm "github.com/jezek/xgb/shm"
	"github.com/jezek/xgb/xinerama"
	"github.com/jezek/xgb/xproto"
)

// captureXineramaPrimary grabs the primary Xinerama screen's full bounds
// via XShm (falling back to plain XGetImage if the X server has no MIT-SHM
// extension) -- the same approach screen_linux_x11.go's Snapshot() already
// uses successfully for kbinani/screenshot.NumActiveDisplays/GetDisplayBounds,
// just not routed through Capture()'s broken Wayland/dbus branch.
func captureXineramaPrimary() (img *image.RGBA, e error) {
	defer func() {
		if r := recover(); r != nil {
			img = nil
			e = fmt.Errorf("capture panic: %v", r)
		}
	}()

	c, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("x11 connect: %w", err)
	}
	defer c.Close()

	if err := xinerama.Init(c); err != nil {
		return nil, fmt.Errorf("xinerama init: %w", err)
	}
	reply, err := xinerama.QueryScreens(c).Reply()
	if err != nil {
		return nil, fmt.Errorf("xinerama query: %w", err)
	}
	if reply.Number == 0 {
		return nil, fmt.Errorf("no xinerama screens")
	}

	primary := reply.ScreenInfo[0]
	width := int(primary.Width)
	height := int(primary.Height)
	x0 := int(primary.XOrg)
	y0 := int(primary.YOrg)

	useShm := true
	if err := mshm.Init(c); err != nil {
		useShm = false
	}

	screen := xproto.Setup(c).DefaultScreen(c)
	wholeScreenBounds := image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels))
	targetBounds := image.Rect(x0, y0, x0+width, y0+height)
	intersect := wholeScreenBounds.Intersect(targetBounds)
	if intersect.Empty() {
		return nil, fmt.Errorf("primary screen bounds don't intersect the root window")
	}

	img = image.NewRGBA(image.Rect(0, 0, width, height))

	var data []byte
	if useShm {
		shmSize := intersect.Dx() * intersect.Dy() * 4
		shmId, err := shm.Get(shm.IPC_PRIVATE, shmSize, shm.IPC_CREAT|0777)
		if err != nil {
			return nil, fmt.Errorf("shm get: %w", err)
		}
		seg, err := mshm.NewSegId(c)
		if err != nil {
			_ = shm.Rm(shmId)
			return nil, fmt.Errorf("shm segid: %w", err)
		}
		data, err = shm.At(shmId, 0, 0)
		if err != nil {
			_ = shm.Rm(shmId)
			return nil, fmt.Errorf("shm attach: %w", err)
		}
		mshm.Attach(c, seg, uint32(shmId), false)
		defer mshm.Detach(c, seg)
		defer func() { _ = shm.Rm(shmId) }()
		defer func() { _ = shm.Dt(data) }()

		_, err = mshm.GetImage(c, xproto.Drawable(screen.Root),
			int16(intersect.Min.X), int16(intersect.Min.Y),
			uint16(intersect.Dx()), uint16(intersect.Dy()), 0xffffffff,
			byte(xproto.ImageFormatZPixmap), seg, 0).Reply()
		if err != nil {
			return nil, fmt.Errorf("shm get image: %w", err)
		}
	} else {
		xImg, err := xproto.GetImage(c, xproto.ImageFormatZPixmap, xproto.Drawable(screen.Root),
			int16(intersect.Min.X), int16(intersect.Min.Y),
			uint16(intersect.Dx()), uint16(intersect.Dy()), 0xffffffff).Reply()
		if err != nil {
			return nil, fmt.Errorf("get image: %w", err)
		}
		data = xImg.Data
	}

	offset := 0
	for iy := intersect.Min.Y; iy < intersect.Max.Y; iy++ {
		for ix := intersect.Min.X; ix < intersect.Max.X; ix++ {
			r := data[offset+2]
			g := data[offset+1]
			b := data[offset]
			img.SetRGBA(ix-x0, iy-y0, color.RGBA{R: r, G: g, B: b, A: 255})
			offset += 4
		}
	}

	return img, nil
}
