// rawhidprobe sends a connected Wacom tablet through the raw HID path
// (internal/usbpass/rawhid.go) without a video stream: the chunks the stream
// would carry go over TCP to rust-shine's rawhid_bridge example, which hands
// them to the same RawHidHub the streamer uses. The bridge's machine should
// then have the tablet on its USB bus, bound by its own driver.
//
//	go run -tags usbpass_gousb ./cmd/rawhidprobe -bridge 127.0.0.1:18192 -seconds 20
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"usbridge-client/internal/models"
	"usbridge-client/internal/usbpass"
)

// tcpLink stands in for the video stream (usbpass.RawHIDLink).
type tcpLink struct {
	mu      sync.Mutex
	conn    net.Conn
	reports int
}

func (l *tcpLink) RawHIDEpoch() uint64 { return 1 }

func (l *tcpLink) SendRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, _ bool) bool {
	head := []byte{kind, slot, endpoint, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(head[3:], total)
	binary.LittleEndian.PutUint16(head[5:], offset)
	binary.LittleEndian.PutUint16(head[7:], uint16(len(data)))
	l.mu.Lock()
	defer l.mu.Unlock()
	if kind == 1 { // a report
		l.reports++
	}
	_, err := l.conn.Write(append(head, data...))
	return err == nil
}

func main() {
	bridge := flag.String("bridge", "127.0.0.1:18192", "address of rust-shine's rawhid_bridge example")
	seconds := flag.Int("seconds", 20, "how long to keep the tablet on the bridge")
	flag.Parse()

	devs, err := usbpass.ListLocal()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var tablet *models.USBPassthroughDevice
	for i := range devs {
		if usbpass.RawHIDEligible(devs[i]) {
			tablet = &devs[i]
			break
		}
	}
	if tablet == nil {
		fmt.Fprintln(os.Stderr, "no Wacom tablet connected")
		os.Exit(1)
	}
	fmt.Printf("tablet %s:%s %q at %s\n", tablet.VID, tablet.PID, tablet.Description, tablet.BusID)

	conn, err := net.Dial("tcp", *bridge)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	link := &tcpLink{conn: conn}
	if err := usbpass.StartRawHIDSession(link, []models.USBPassthroughDevice{*tablet}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("mounted: %v\n", usbpass.ActiveBusIDs())
	for i := 0; i < *seconds; i++ {
		time.Sleep(time.Second)
		link.mu.Lock()
		n := link.reports
		link.mu.Unlock()
		fmt.Printf("%2ds: %d report chunks sent\n", i+1, n)
	}
	usbpass.StopRawHIDSession()
	fmt.Println("released")
}
