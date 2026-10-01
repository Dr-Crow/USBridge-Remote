package usbpass

// A HID device sent to the agent over the video stream instead of USB/IP.
//
// With USB/IP every poll of the agent's driver is a round trip to this machine
// over TCP, and one lost segment holds back everything behind it: a pen lags,
// worst on Wi-Fi. Here this side plays the USB host itself. It asks the claimed
// device everything a driver asks before it reads input (descriptors, feature
// reports), sends that once as a model, and then pushes each input report as
// the device produces it, on the stream's control channel (the same path as
// keyboard, mouse and gamepads). The agent's streamer rebuilds the device from
// the model as a virtual USB device for the native driver (Wacom's) and answers
// the driver's polls and requests locally; see rust-shine's
// usb-passthrough/src/virtual_rawhid.rs.
//
// It works on top of any DeviceBackend, so the same code serves the HID
// bridges (Windows, macOS) and the libusb claim (Linux).
//
// What the agent's driver writes to the device stays on the agent. The one
// write a Wacom tablet needs, the switch out of its mouse-compatible mode, is
// made here before the model is read (wacomEnterTabletMode).

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"usbridge-client/internal/models"
)

// Chunk kinds: LI_RAW_HID_* in moonlight-common-c's Limelight.h.
const (
	rawHIDKindModel  = 0
	rawHIDKindReport = 1
	rawHIDKindDetach = 2

	rawHIDMaxChunk = 64 // LI_RAW_HID_MAX_CHUNK
	rawHIDMaxSlots = 4  // MAX_SLOTS in virtual_rawhid.rs
)

// rawHIDIdleFlush is how long an endpoint must stay silent before its last
// report is repeated reliably. Reports in motion go unreliably, so the last
// one of a stroke (the pen lifting) may be the one that got lost.
const rawHIDIdleFlush = 20 * time.Millisecond

// RawHIDLink is the stream that carries the devices
// (service.MoonlightRawHIDSender).
type RawHIDLink interface {
	// RawHIDEpoch is non-zero while a stream is up whose host takes raw HID
	// devices, and changes with every new connection.
	RawHIDEpoch() uint64
	// SendRawHID queues one chunk; false if it was not queued.
	SendRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, reliable bool) bool
}

// RawHIDEligible reports whether a device is sent over the stream rather than
// exported over USB/IP when a stream is up: the Wacom tablets.
func RawHIDEligible(d models.USBPassthroughDevice) bool {
	vid, _, err := ParseVIDPID(d.VID, d.PID)
	return err == nil && vid == wacomVendorID
}

type rawHIDEndpoint struct {
	addr      uint8
	maxPacket int

	mu    sync.Mutex
	last  []byte
	timer *time.Timer
}

type rawHIDDevice struct {
	dev   *ExportedDevice
	slot  uint8
	model []byte
	eps   []*rawHIDEndpoint
}

// RawHIDSession owns the devices being sent over the stream.
type RawHIDSession struct {
	link   RawHIDLink
	devs   []*rawHIDDevice
	busIDs []string
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// epoch is the stream connection that holds the models; 0 while none does,
	// and reports are dropped meanwhile.
	epoch atomic.Uint64
}

var activeRawHID *RawHIDSession // guarded by sessionMu

func rawHIDBusIDs() []string {
	sessionMu.Lock()
	s := activeRawHID
	sessionMu.Unlock()
	if s == nil {
		return nil
	}
	return append([]string(nil), s.busIDs...)
}

// StartRawHIDSession claims the devices and sends them over link. It replaces
// a previous raw HID session; the USB/IP session (StartSession) is separate.
func StartRawHIDSession(link RawHIDLink, devices []models.USBPassthroughDevice) error {
	StopRawHIDSession()
	if len(devices) > rawHIDMaxSlots {
		return fmt.Errorf("at most %d devices can be sent over the stream", rawHIDMaxSlots)
	}
	exported, busIDs, err := claimForExport(devices)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &RawHIDSession{link: link, busIDs: busIDs, cancel: cancel}
	for i, ed := range exported {
		d, err := newRawHIDDevice(ctx, ed, uint8(i))
		if err != nil {
			cancel()
			closeExported(exported)
			return fmt.Errorf("%s: %w", ed.BusID, err)
		}
		s.devs = append(s.devs, d)
	}
	s.wg.Add(1)
	go s.announceLoop(ctx)
	for _, d := range s.devs {
		for _, ep := range d.eps {
			s.wg.Add(1)
			go s.pump(ctx, d, ep)
		}
	}
	sessionMu.Lock()
	activeRawHID = s
	sessionMu.Unlock()
	return nil
}

// StopRawHIDSession unplugs the devices on the agent and releases them here.
func StopRawHIDSession() {
	sessionMu.Lock()
	s := activeRawHID
	activeRawHID = nil
	sessionMu.Unlock()
	if s == nil {
		return
	}
	s.cancel()
	// A backend that honours the cancelled context lets its pump leave on its
	// own; closing the others is what ends a read they are blocked in.
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
	}
	for _, d := range s.devs {
		for _, ep := range d.eps {
			ep.mu.Lock()
			if ep.timer != nil {
				ep.timer.Stop()
			}
			ep.mu.Unlock()
		}
		if s.epoch.Load() != 0 {
			s.sendReliable(context.Background(), rawHIDKindDetach, d.slot, 0, nil)
		}
		if d.dev.Backend != nil {
			_ = d.dev.Backend.Close()
		}
	}
	<-done
}

// announceLoop sends the models whenever a stream connection that takes them
// comes up: at the start, and again after every reconnect, since the agent
// unplugs the devices when a connection ends.
func (s *RawHIDSession) announceLoop(ctx context.Context) {
	defer s.wg.Done()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if e := s.link.RawHIDEpoch(); e != s.epoch.Load() {
			s.epoch.Store(0)
			if e != 0 && s.announce(ctx) {
				s.epoch.Store(e)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *RawHIDSession) announce(ctx context.Context) bool {
	for _, d := range s.devs {
		if !s.sendReliable(ctx, rawHIDKindModel, d.slot, 0, d.model) {
			logrus.Warnf("usbpass: rawhid: %s: the stream did not take the device model; will retry", d.dev.BusID)
			return false
		}
		logrus.Infof("usbpass: rawhid: %s (%04x:%04x) sent over the stream, slot %d, %d-byte model",
			d.dev.BusID, d.dev.VID, d.dev.PID, d.slot, len(d.model))
	}
	return true
}

// sendReliable sends data in order, in chunks, waiting out a full input queue.
func (s *RawHIDSession) sendReliable(ctx context.Context, kind, slot, endpoint uint8, data []byte) bool {
	deadline := time.Now().Add(2 * time.Second)
	for off := 0; ; {
		n := min(len(data)-off, rawHIDMaxChunk)
		for !s.link.SendRawHID(kind, slot, endpoint, uint16(len(data)), uint16(off), data[off:off+n], true) {
			if ctx.Err() != nil || time.Now().After(deadline) {
				return false
			}
			time.Sleep(2 * time.Millisecond)
		}
		off += n
		if off >= len(data) {
			return true
		}
	}
}

// pump reads one interrupt endpoint the way the agent's driver would and
// forwards every report.
func (s *RawHIDSession) pump(ctx context.Context, d *rawHIDDevice, ep *rawHIDEndpoint) {
	defer s.wg.Done()
	failures := 0
	for ctx.Err() == nil {
		status, rep := d.dev.Backend.HandleBulk(ctx, ep.addr, true, ep.maxPacket, nil)
		if status != 0 || len(rep) == 0 {
			// A timed out read is routine (no input); a device that is gone
			// fails every time, so back off.
			failures++
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(min(failures, 50)) * time.Millisecond):
			}
			continue
		}
		failures = 0
		if s.epoch.Load() == 0 {
			continue
		}
		s.sendReport(ctx, d, ep, rep)
	}
}

func (s *RawHIDSession) sendReport(ctx context.Context, d *rawHIDDevice, ep *rawHIDEndpoint, rep []byte) {
	if len(rep) > rawHIDMaxChunk {
		// Too long for one packet: every chunk has to arrive.
		s.sendReliable(ctx, rawHIDKindReport, d.slot, ep.addr, rep)
		return
	}
	// Sent under the lock so the idle repeat below can never overtake, or be
	// overtaken by, a newer report.
	ep.mu.Lock()
	defer ep.mu.Unlock()
	s.link.SendRawHID(rawHIDKindReport, d.slot, ep.addr, uint16(len(rep)), 0, rep, false)
	ep.last = append(ep.last[:0], rep...)
	if ep.timer != nil {
		ep.timer.Reset(rawHIDIdleFlush)
		return
	}
	ep.timer = time.AfterFunc(rawHIDIdleFlush, func() {
		ep.mu.Lock()
		defer ep.mu.Unlock()
		if ctx.Err() == nil && s.epoch.Load() != 0 {
			// The agent drops a report equal to the previous one, so this
			// only has an effect if the original was lost.
			s.link.SendRawHID(rawHIDKindReport, d.slot, ep.addr, uint16(len(ep.last)), 0, ep.last, true)
		}
	})
}

// rawHIDControlIn asks the backend a device-to-host control request.
func rawHIDControlIn(ctx context.Context, b DeviceBackend, bm, req uint8, value, index uint16, length int) ([]byte, bool) {
	var setup [8]byte
	setup[0], setup[1] = bm, req
	binary.LittleEndian.PutUint16(setup[2:], value)
	binary.LittleEndian.PutUint16(setup[4:], index)
	binary.LittleEndian.PutUint16(setup[6:], uint16(length))
	status, data := b.HandleControl(ctx, setup, length, nil)
	if status != 0 {
		return nil, false
	}
	if len(data) > length {
		data = data[:length]
	}
	return data, true
}

// walkUSBDescriptors calls f for each descriptor in a configuration.
func walkUSBDescriptors(config []byte, f func(d []byte)) {
	for len(config) >= 2 {
		n := int(config[0])
		if n < 2 || n > len(config) {
			return
		}
		f(config[:n])
		config = config[n:]
	}
}

var errRawHIDNotHID = errors.New("the device has no HID interface with an interrupt endpoint")

// newRawHIDDevice reads the model of a claimed device: everything the agent's
// driver will ask the virtual device for.
func newRawHIDDevice(parent context.Context, dev *ExportedDevice, slot uint8) (*rawHIDDevice, error) {
	b := dev.Backend
	if b == nil {
		return nil, errors.New("the device could not be claimed")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()

	deviceDesc, ok := rawHIDControlIn(ctx, b, 0x80, 0x06, 0x0100, 0, 18)
	if !ok || len(deviceDesc) != 18 {
		deviceDesc = dev.DeviceDesc
	}
	if len(deviceDesc) != 18 {
		return nil, errors.New("no device descriptor")
	}
	configDesc := dev.ConfigDesc
	if head, ok := rawHIDControlIn(ctx, b, 0x80, 0x06, 0x0200, 0, 9); ok && len(head) == 9 {
		total := int(binary.LittleEndian.Uint16(head[2:]))
		if full, ok := rawHIDControlIn(ctx, b, 0x80, 0x06, 0x0200, 0, total); ok && len(full) == total {
			configDesc = full
		}
	}
	if len(configDesc) < 9 {
		return nil, errors.New("no configuration descriptor")
	}

	// The HID interfaces, their report descriptor sizes and interrupt-IN
	// endpoints, and every string the descriptors name.
	type hidIface struct {
		number  uint8
		descLen int
	}
	var (
		ifaces  []hidIface
		eps     []*rawHIDEndpoint
		strIdx  = []uint8{deviceDesc[14], deviceDesc[15], deviceDesc[16], configDesc[6]}
		current = -1
		isHID   bool
	)
	walkUSBDescriptors(configDesc, func(d []byte) {
		switch {
		case d[1] == 0x04 && len(d) >= 9:
			current, isHID = int(d[2]), d[5] == 0x03
			strIdx = append(strIdx, d[8])
		case d[1] == 0x21 && len(d) >= 9 && isHID && d[6] == 0x22:
			for _, f := range ifaces {
				if int(f.number) == current {
					return // an alternate setting of an interface already seen
				}
			}
			ifaces = append(ifaces, hidIface{uint8(current), int(binary.LittleEndian.Uint16(d[7:]))})
		case d[1] == 0x05 && len(d) >= 7 && isHID && d[2]&0x80 != 0 && d[3]&0x03 == 0x03:
			for _, e := range eps {
				if e.addr == d[2] {
					return
				}
			}
			mps := int(binary.LittleEndian.Uint16(d[4:]) & 0x07FF)
			eps = append(eps, &rawHIDEndpoint{addr: d[2], maxPacket: max(mps, 8)})
		}
	})
	if len(ifaces) == 0 || len(eps) == 0 {
		return nil, errRawHIDNotHID
	}

	m := []byte("UBH1")
	m = binary.LittleEndian.AppendUint16(m, dev.VID)
	m = binary.LittleEndian.AppendUint16(m, dev.PID)
	m = append(m, deviceDesc[12], deviceDesc[13]) // bcdDevice
	speed := uint8(dev.Speed)
	if speed < 1 || speed > 3 {
		speed = 2
	}
	m = append(m, speed, eps[0].addr)
	blob := func(p []byte) {
		m = binary.LittleEndian.AppendUint16(m, uint16(len(p)))
		m = append(m, p...)
	}
	blob(deviceDesc)
	blob(configDesc)

	strs := map[uint8][]byte{}
	for _, i := range strIdx {
		if _, done := strs[i]; i == 0 || done {
			continue
		}
		if s, ok := rawHIDControlIn(ctx, b, 0x80, 0x06, 0x0300|uint16(i), 0x0409, 255); ok && len(s) >= 2 {
			strs[i] = s
		}
	}
	m = append(m, uint8(len(strs)))
	for i, s := range strs {
		m = append(m, i)
		blob(s)
	}

	type reportKey struct{ iface, typ, id uint8 }
	reportDescs := map[uint8][]byte{}
	features := map[reportKey][]byte{}
	for _, f := range ifaces {
		rd, ok := rawHIDControlIn(ctx, b, 0x81, 0x06, 0x2200, uint16(f.number), f.descLen)
		if !ok || len(rd) == 0 {
			return nil, fmt.Errorf("interface %d has no report descriptor", f.number)
		}
		reportDescs[f.number] = rd
		_, _, feat := hidReportSizes(rd)
		if dev.VID == wacomVendorID {
			wacomEnterTabletMode(ctx, b, f.number, feat)
		}
		for id, size := range feat {
			if id != 0 {
				size++ // the report id leads
			}
			if rep, ok := rawHIDControlIn(ctx, b, 0xA1, 0x01, 0x0300|uint16(id), uint16(f.number), size); ok && len(rep) > 0 {
				features[reportKey{f.number, 3, id}] = rep
			}
		}
	}
	m = append(m, uint8(len(reportDescs)))
	for i, rd := range reportDescs {
		m = append(m, i)
		blob(rd)
	}
	m = binary.LittleEndian.AppendUint16(m, uint16(len(features)))
	for k, rep := range features {
		m = append(m, k.iface, k.typ, k.id)
		blob(rep)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("reading the device model: %w", err)
	}
	if len(m) > 0xFFFF {
		return nil, fmt.Errorf("the device model is too large (%d bytes)", len(m))
	}
	return &rawHIDDevice{dev: dev, slot: slot, model: m, eps: eps}, nil
}

// wacomEnterTabletMode switches a Wacom tablet from the mouse-compatible mode
// it powers up in to the mode its driver reads (feature report 2, value 2: what
// every Wacom driver writes first). The agent's driver writes it too, but to
// the virtual device; this is the write that reaches the tablet. Behind a HID
// bridge the local driver already did it and this changes nothing.
func wacomEnterTabletMode(ctx context.Context, b DeviceBackend, iface uint8, feat map[uint8]int) {
	if feat[2] != 1 {
		return
	}
	setup := [8]byte{0x21, 0x09, 0x02, 0x03, iface, 0, 2, 0}
	if status, _ := b.HandleControl(ctx, setup, 2, []byte{0x02, 0x02}); status != 0 {
		logrus.Warnf("usbpass: rawhid: interface %d refused the switch to tablet mode (status %d)", iface, status)
	}
}
