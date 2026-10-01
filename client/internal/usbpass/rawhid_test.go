package usbpass

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

type rawHIDChunk struct {
	kind, slot, endpoint uint8
	total, offset        uint16
	data                 []byte
	reliable             bool
}

// fakeRawHIDLink records what a session sends. busy makes the first sends fail,
// like a full input queue.
type fakeRawHIDLink struct {
	mu     sync.Mutex
	epoch  uint64
	busy   int
	chunks []rawHIDChunk
}

func (l *fakeRawHIDLink) RawHIDEpoch() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.epoch
}

func (l *fakeRawHIDLink) SendRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, reliable bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.busy > 0 {
		l.busy--
		return false
	}
	l.chunks = append(l.chunks, rawHIDChunk{kind, slot, endpoint, total, offset, append([]byte(nil), data...), reliable})
	return true
}

func (l *fakeRawHIDLink) take() []rawHIDChunk {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.chunks
	l.chunks = nil
	return out
}

func (l *fakeRawHIDLink) waitFor(t *testing.T, what string, ok func([]rawHIDChunk) bool) []rawHIDChunk {
	t.Helper()
	var got []rawHIDChunk
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		got = append(got, l.take()...)
		if ok(got) {
			return got
		}
	}
	t.Fatalf("%s: got %d chunks", what, len(got))
	return nil
}

func intuosDevice(t *testing.T) (*ExportedDevice, *hidGenBackend) {
	t.Helper()
	m := wacomModelFor(0x056A, 0x0374)
	if m == nil {
		t.Fatal("no CTL-4100 model")
	}
	dev := &ExportedDevice{BusID: "1-1"}
	return dev, applyWacomModel(dev, m)
}

// The model is everything the agent's driver asks for, in the layout
// virtual_rawhid.rs parses.
func TestRawHIDModel(t *testing.T) {
	dev, backend := intuosDevice(t)
	d, err := newRawHIDDevice(context.Background(), dev, 1)
	if err != nil {
		t.Fatal(err)
	}
	m := d.model
	if string(m[:4]) != "UBH1" || binary.LittleEndian.Uint16(m[4:]) != 0x056A || binary.LittleEndian.Uint16(m[6:]) != 0x0374 {
		t.Fatalf("header % x", m[:12])
	}
	if speed, ep := m[10], m[11]; speed != 2 || ep != 0x81 {
		t.Fatalf("speed %d endpoint %#x", speed, ep)
	}
	if len(d.eps) != 1 || d.eps[0].addr != 0x81 || d.eps[0].maxPacket != 64 {
		t.Fatalf("endpoints %+v", d.eps)
	}
	r := m[12:]
	blob := func() []byte {
		n := int(binary.LittleEndian.Uint16(r))
		b := r[2 : 2+n]
		r = r[2+n:]
		return b
	}
	if !bytes.Equal(blob(), backend.deviceDesc) || !bytes.Equal(blob(), backend.configDesc) {
		t.Fatal("descriptors differ from the device's")
	}
	nStr := int(r[0])
	r = r[1:]
	for i := 0; i < nStr; i++ {
		r = r[1:]
		blob()
	}
	if nStr != 3 {
		t.Fatalf("%d strings", nStr)
	}
	if r[0] != 1 || r[1] != 0 {
		t.Fatalf("report descriptors: n=%d iface=%d", r[0], r[1])
	}
	r = r[2:]
	if rd := blob(); !bytes.Equal(rd, backend.ifaces[0].ReportDesc) {
		t.Fatal("report descriptor differs")
	}
	nFeat := int(binary.LittleEndian.Uint16(r))
	r = r[2:]
	feats := map[uint8][]byte{}
	for i := 0; i < nFeat; i++ {
		if r[0] != 0 || r[1] != 3 {
			t.Fatalf("feature key % x", r[:3])
		}
		id := r[2]
		r = r[3:]
		feats[id] = blob()
	}
	if len(r) != 0 {
		t.Fatalf("%d bytes left over", len(r))
	}
	// The tablet-mode switch, and a report the tablet refuses is left out.
	if !bytes.Equal(feats[2], []byte{2, 2}) {
		t.Fatalf("feature 2 = % x", feats[2])
	}
	if _, ok := feats[21]; ok {
		t.Fatal("a stalled feature report is in the model")
	}
}

func TestRawHIDSessionSendsModelThenReports(t *testing.T) {
	dev, backend := intuosDevice(t)
	ctx, cancel := context.WithCancel(context.Background())
	d, err := newRawHIDDevice(ctx, dev, 0)
	if err != nil {
		t.Fatal(err)
	}
	link := &fakeRawHIDLink{epoch: 1, busy: 3}
	s := &RawHIDSession{link: link, devs: []*rawHIDDevice{d}, cancel: cancel}
	s.wg.Add(2)
	go s.announceLoop(ctx)
	go s.pump(ctx, d, d.eps[0])
	defer func() { cancel(); s.wg.Wait() }()

	// The model arrives whole and in order despite the busy queue.
	chunks := link.waitFor(t, "model", func(c []rawHIDChunk) bool {
		n := 0
		for _, x := range c {
			n += len(x.data)
		}
		return n >= len(d.model)
	})
	var model []byte
	for _, c := range chunks {
		if c.kind != rawHIDKindModel || !c.reliable || int(c.total) != len(d.model) || int(c.offset) != len(model) || len(c.data) > rawHIDMaxChunk {
			t.Fatalf("model chunk %+v at %d", c, len(model))
		}
		model = append(model, c.data...)
	}
	if !bytes.Equal(model, d.model) {
		t.Fatal("model was not reassembled")
	}
	for s.epoch.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	// A report goes out at once, unreliably, and is repeated reliably when
	// nothing follows it.
	rep := append([]byte{0x10, 0x61}, make([]byte, 25)...)
	if !wacomModelFor(0x056A, 0x0374).pushReport(backend, rep, -1) {
		t.Fatal("report not routed")
	}
	got := link.waitFor(t, "report and its repeat", func(c []rawHIDChunk) bool { return len(c) >= 2 })
	for i, c := range got {
		if c.kind != rawHIDKindReport || c.endpoint != 0x81 || c.offset != 0 || !bytes.Equal(c.data, rep[:len(c.data)]) || c.reliable != (i == 1) {
			t.Fatalf("chunk %d: %+v", i, c)
		}
	}

	// A new stream connection gets the model again.
	link.mu.Lock()
	link.epoch = 2
	link.mu.Unlock()
	link.waitFor(t, "model after reconnect", func(c []rawHIDChunk) bool {
		return len(c) > 0 && c[0].kind == rawHIDKindModel && c[0].offset == 0
	})
}
