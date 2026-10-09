//go:build usbpass_gousb && !darwin && !android

package usbpass

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/google/gousb"
	"github.com/sirupsen/logrus"
)

// Alternate settings and isochronous endpoints: what a webcam or a USB
// audio interface needs. Their streaming interface claims alt 0, which has
// no endpoints (no bus bandwidth); the host's driver selects another alt
// setting to start the stream (setAlternate) and then keeps several
// isochronous URBs queued (SubmitIso, see IsoBackend).

// isoDevLock takes devMu around the isochronous path's libusb calls only on
// Windows, where it works around WinUSB's limits on concurrent calls (see
// devMu). Elsewhere libusb's asynchronous calls are thread-safe, and devMu
// can be held for seconds by an idle interrupt endpoint's poll -- a
// webcam's status endpoint -- which would leave the stream with no URB on
// the bus (an isochronous packet with nobody asking for it is gone).
func (b *gousbBackend) isoDevLock() func() {
	if runtime.GOOS != "windows" {
		return func() {}
	}
	b.devMu.Lock()
	return b.devMu.Unlock
}

// endpointOf is the claimed interface serving endpoint addr (direction bit
// included) and the endpoint's transfer type; nil if no claimed interface's
// current setting has it.
func (b *gousbBackend) endpointOf(addr uint8) (*gousb.Interface, gousb.TransferType) {
	b.epMu.RLock()
	defer b.epMu.RUnlock()
	return b.epOwner[addr], b.epType[addr]
}

// setAlternate switches claimed interface num to alternate setting alt
// (SET_INTERFACE); returns the URB status.
func (b *gousbBackend) setAlternate(num, alt uint8) int32 {
	b.epMu.Lock()
	defer b.epMu.Unlock()
	intf := b.ifaces[num]
	if intf == nil {
		logrus.Warnf("usbpass: SET_INTERFACE(iface=%d alt=%d) on an interface not claimed", num, alt)
		return errnoEPIPE
	}
	if b.ifaceAlts[num] == alt {
		// Forwarding it would make the host controller rebuild the
		// endpoints of a setting that is already live (see HandleControl).
		return 0
	}
	old := intf.Setting.Endpoints
	unlock := b.isoDevLock()
	err := intf.SetAlternate(int(alt))
	unlock()
	if err != nil {
		logrus.Warnf("usbpass: SET_INTERFACE(iface=%d alt=%d): %v", num, alt, err)
		return errnoEPIPE
	}
	for addr := range old {
		delete(b.epOwner, uint8(addr))
		delete(b.epType, uint8(addr))
	}
	for addr, epd := range intf.Setting.Endpoints {
		b.epOwner[uint8(addr)] = intf
		b.epType[uint8(addr)] = epd.TransferType
	}
	b.ifaceAlts[num] = alt
	logrus.Infof("usbpass: interface %d switched to alt %d (%d endpoints)", num, alt, len(intf.Setting.Endpoints))
	return 0
}

// SubmitIso implements IsoBackend.
func (b *gousbBackend) SubmitIso(ep uint8, dirIn bool, packets []IsoPacket, outData []byte) (func(ctx context.Context) (int32, []byte), error) {
	num := int(ep & 0x7f)
	addr := uint8(num)
	if dirIn {
		addr |= 0x80
	}
	// libusb lays the packets out back to back; USB/IP gives each its
	// offset in the transfer buffer (back to back too, in practice).
	lengths := make([]int, len(packets))
	total := 0
	for i, p := range packets {
		lengths[i] = int(p.Length)
		total += int(p.Length)
	}
	buf := make([]byte, total)
	if !dirIn {
		off := 0
		for i, p := range packets {
			end := int(p.Offset) + lengths[i]
			if end > len(outData) {
				return nil, fmt.Errorf("iso OUT packet %d past the transfer buffer", i)
			}
			copy(buf[off:], outData[p.Offset:end])
			off += lengths[i]
		}
	}

	b.epMu.RLock()
	owner, typ := b.epOwner[addr], b.epType[addr]
	var x *gousb.IsoTransfer
	err := fmt.Errorf("ep=%#02x: not an isochronous endpoint of a claimed interface's current setting", addr)
	if owner != nil && typ == gousb.TransferTypeIsochronous {
		if dirIn {
			var in *gousb.InEndpoint
			if in, err = owner.InEndpoint(num); err == nil {
				unlock := b.isoDevLock()
				x, err = in.SubmitIso(buf, lengths)
				unlock()
			}
		} else {
			var out *gousb.OutEndpoint
			if out, err = owner.OutEndpoint(num); err == nil {
				unlock := b.isoDevLock()
				x, err = out.SubmitIso(buf, lengths)
				unlock()
			}
		}
	}
	b.epMu.RUnlock()
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context) (int32, []byte) {
		res, err := x.Wait(ctx)
		if len(res) != len(packets) {
			for i := range packets {
				packets[i].Actual, packets[i].Status = 0, errnoEXDEV
			}
			if ctx.Err() != nil {
				return errnoECONNRESET, nil
			}
			return errnoEPIPE, nil
		}
		var data []byte
		off := 0
		for i, r := range res {
			packets[i].Actual = uint32(r.Actual)
			packets[i].Status = transferErrno(r.Status)
			if dirIn {
				data = append(data, buf[off:off+r.Actual]...)
			}
			off += lengths[i]
		}
		var status gousb.TransferStatus
		if err != nil && !errors.As(err, &status) {
			return errnoEPIPE, data
		}
		return transferErrno(status), data
	}, nil
}

// transferErrno is the Linux errno a URB (or one isochronous packet) with
// libusb status st ends with, as USB/IP carries it.
func transferErrno(st gousb.TransferStatus) int32 {
	switch st {
	case gousb.TransferCompleted:
		return 0
	case gousb.TransferStall:
		return errnoEPIPE
	case gousb.TransferCancelled:
		return errnoECONNRESET
	case gousb.TransferTimedOut:
		return -110 // ETIMEDOUT
	case gousb.TransferNoDevice:
		return -108 // ESHUTDOWN
	case gousb.TransferOverflow:
		return -75 // EOVERFLOW
	default:
		return -71 // EPROTO
	}
}
