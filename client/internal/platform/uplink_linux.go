//go:build linux && !android

package platform

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
)

// ListMIDIInputs lists the raw MIDI devices (/dev/snd/midiC*D*) that have
// an input -- USB MIDI controllers, keyboards, interfaces. A NanoKVM's own
// MIDI port (its USB gadget's f_midi, "MIDI function") is left out: when the
// target PC is this machine, forwarding it would feed the host's output
// straight back into it.
func ListMIDIInputs() []MIDIInputInfo {
	paths, _ := filepath.Glob("/dev/snd/midiC*D*")
	re := regexp.MustCompile(`midiC(\d+)D(\d+)$`)
	var out []MIDIInputInfo
	for _, p := range paths {
		m := re.FindStringSubmatch(p)
		if m == nil {
			continue
		}
		card, dev := m[1], m[2]
		info, err := os.ReadFile(fmt.Sprintf("/proc/asound/card%s/midi%s", card, dev))
		if err != nil || !strings.Contains(string(info), "Input") {
			continue
		}
		if isGadgetMIDICard(card) {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(string(info), "\n", 2)[0])
		if name == "" {
			name = "MIDI " + card + "," + dev
		} else if dev != "0" {
			// One card, several ports (often named alike).
			name += " " + dev
		}
		out = append(out, MIDIInputInfo{ID: midiInputID(card, dev), Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// midiInputID names a device by its card id rather than its number, so the
// id survives other cards coming and going.
func midiInputID(card, dev string) string {
	id, _ := os.ReadFile("/sys/class/sound/card" + card + "/id")
	return strings.TrimSpace(string(id)) + "," + dev
}

func midiInputPath(id string) (string, error) {
	cardID, dev, ok := strings.Cut(id, ",")
	if !ok {
		return "", fmt.Errorf("bad MIDI input id %q", id)
	}
	cards, _ := filepath.Glob("/sys/class/sound/card*")
	for _, c := range cards {
		if b, _ := os.ReadFile(filepath.Join(c, "id")); strings.TrimSpace(string(b)) == cardID {
			return "/dev/snd/midiC" + strings.TrimPrefix(filepath.Base(c), "card") + "D" + dev, nil
		}
	}
	return "", fmt.Errorf("MIDI input %q is gone", id)
}

// isGadgetMIDICard: the card belongs to a USB device with an f_midi
// interface (a Linux USB gadget's MIDI function, e.g. a NanoKVM's).
func isGadgetMIDICard(card string) bool {
	iface, err := filepath.EvalSymlinks("/sys/class/sound/card" + card + "/device")
	if err != nil {
		return false
	}
	names, _ := filepath.Glob(filepath.Join(filepath.Dir(iface), "*:*", "interface"))
	for _, n := range names {
		if b, _ := os.ReadFile(n); strings.TrimSpace(string(b)) == "MIDI function" {
			return true
		}
	}
	return false
}

type midiCapture struct {
	f    *os.File
	once sync.Once
}

func (c *midiCapture) Stop() { c.once.Do(func() { c.f.Close() }) }

// StartMIDICapture reads the MIDI input id (see ListMIDIInputs) and hands
// what it plays to onData, in chunks of at most 128 bytes, from its own
// goroutine. The bytes are passed on as they come (running status, SysEx
// split anywhere): the host plays them back as one ordered byte stream.
func StartMIDICapture(id string, onData func([]byte)) (UplinkCapture, error) {
	path, err := midiInputPath(id)
	if err != nil {
		return nil, err
	}
	// Non-blocking, so the runtime poller waits for data and Stop's Close
	// ends a pending read.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	c := &midiCapture{f: f}
	go func() {
		buf := make([]byte, 128)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				onData(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				if err != io.EOF && !strings.Contains(err.Error(), "file already closed") {
					fmt.Fprintf(os.Stderr, "MIDI input %s: %v\n", id, err)
				}
				return
			}
		}
	}()
	return c, nil
}
