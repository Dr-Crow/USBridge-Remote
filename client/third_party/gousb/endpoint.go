// Copyright 2013 Google Inc.  All rights reserved.
// Copyright 2016 the gousb Authors.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gousb

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// EndpointAddress is a unique identifier for the endpoint, combining the endpoint number and direction.
type EndpointAddress uint8

// String implements the Stringer interface.
func (a EndpointAddress) String() string {
	return fmt.Sprintf("0x%02x", uint8(a))
}

// EndpointDesc contains the information about an interface endpoint, extracted
// from the descriptor.
type EndpointDesc struct {
	// Address is the unique identifier of the endpoint within the interface.
	Address EndpointAddress
	// Number represents the endpoint number. Note that the endpoint number is different from the
	// address field in the descriptor - address 0x82 means endpoint number 2,
	// with endpoint direction IN.
	// The device can have up to two endpoints with the same number but with
	// different directions.
	Number int
	// Direction defines whether the data is flowing IN or OUT from the host perspective.
	Direction EndpointDirection
	// MaxPacketSize is the maximum USB packet size for a single frame/microframe.
	MaxPacketSize int
	// TransferType defines the endpoint type - bulk, interrupt, isochronous.
	TransferType TransferType
	// PollInterval is the maximum time between transfers for interrupt and isochronous transfer,
	// or the NAK interval for a control transfer. See endpoint descriptor bInterval documentation
	// in the USB spec for details.
	PollInterval time.Duration
	// IsoSyncType is the isochronous endpoint synchronization type, as defined by USB spec.
	IsoSyncType IsoSyncType
	// UsageType is the isochronous or interrupt endpoint usage type, as defined by USB spec.
	UsageType UsageType
}

// String returns the human-readable description of the endpoint.
func (e EndpointDesc) String() string {
	ret := make([]string, 0, 3)
	ret = append(ret, fmt.Sprintf("ep #%d %s (address %s) %s", e.Number, e.Direction, e.Address, e.TransferType))
	switch e.TransferType {
	case TransferTypeIsochronous:
		ret = append(ret, fmt.Sprintf("- %s %s", e.IsoSyncType, e.UsageType))
	case TransferTypeInterrupt:
		ret = append(ret, fmt.Sprintf("- %s", e.UsageType))
	}
	ret = append(ret, fmt.Sprintf("[%d bytes]", e.MaxPacketSize))
	return strings.Join(ret, " ")
}

type endpoint struct {
	h *libusbDevHandle

	InterfaceSetting
	Desc EndpointDesc

	ctx *Context
}

// String returns a human-readable description of the endpoint.
func (e *endpoint) String() string {
	return e.Desc.String()
}

func (e *endpoint) transfer(ctx context.Context, buf []byte) (int, error) {
	t, err := newUSBTransfer(e.ctx, e.h, &e.Desc, len(buf))
	if err != nil {
		return 0, err
	}
	defer t.free()
	if e.Desc.Direction == EndpointDirectionOut {
		copy(t.data(), buf)
	}

	if err := t.submit(); err != nil {
		return 0, err
	}

	n, err := t.wait(ctx)
	if e.Desc.Direction == EndpointDirectionIn {
		copy(buf, t.data())
	}
	if err != nil {
		return n, err
	}
	return n, nil
}

// IsoPacket is one packet of an isochronous transfer (TransferIso).
type IsoPacket struct {
	// Length is what the packet asked for, Actual what it carried.
	Length, Actual int
	Status         TransferStatus
}

// IsoTransfer is a submitted isochronous transfer (SubmitIso).
type IsoTransfer struct {
	t     *usbTransfer
	xfer  *libusbTransfer
	buf   []byte
	total int
	in    bool
}

// SubmitIso submits one isochronous transfer of len(lengths) packets on an
// isochronous endpoint without waiting for it: packet i is lengths[i] bytes
// at its offset in buf (the sum of the lengths before it), the layout
// USB/IP uses. Transfers submitted one after another run back to back, so a
// stream keeps several in flight. buf must stay untouched until Wait.
func (e *endpoint) SubmitIso(buf []byte, lengths []int) (*IsoTransfer, error) {
	if e.Desc.TransferType != TransferTypeIsochronous {
		return nil, fmt.Errorf("SubmitIso on %s, which is not isochronous", e)
	}
	if len(lengths) == 0 {
		return nil, fmt.Errorf("SubmitIso: no packets")
	}
	total := 0
	for _, l := range lengths {
		total += l
	}
	if total > len(buf) {
		return nil, fmt.Errorf("SubmitIso: packets need %d bytes, buffer has %d", total, len(buf))
	}
	bufLen := total
	if bufLen == 0 {
		bufLen = 1
	}
	done := make(chan struct{}, 1)
	xfer, err := e.ctx.libusb.alloc(e.h, &e.Desc, len(lengths), bufLen, done)
	if err != nil {
		return nil, err
	}
	for i, l := range lengths {
		setIsoPacketLength(xfer, i, l)
	}
	t := &usbTransfer{xfer: xfer, buf: e.ctx.libusb.buffer(xfer), done: done, ctx: e.ctx, rawIso: true}
	in := e.Desc.Direction == EndpointDirectionIn
	if !in {
		copy(t.buf, buf[:total])
	}
	if err := t.submit(); err != nil {
		t.free()
		return nil, err
	}
	return &IsoTransfer{t: t, xfer: xfer, buf: buf, total: total, in: in}, nil
}

// Wait waits for the transfer and returns every packet's result; IN data
// lands in the submitted buffer at the packets' offsets -- unlike Read,
// which packs the packets together and so loses where each one starts. A
// packet's own error is in its Status, err is for the transfer as a whole.
// Cancelling ctx cancels the transfer.
func (x *IsoTransfer) Wait(ctx context.Context) ([]IsoPacket, error) {
	_, err := x.t.wait(ctx)
	if err == TransferCancelled && x.t.submitted {
		// wait gave up on the cancellation: libusb still owns the
		// transfer, don't read it.
		return nil, err
	}
	defer x.t.free()
	res := isoPacketResults(x.xfer)
	if x.in {
		copy(x.buf, x.t.buf[:x.total])
	}
	return res, err
}

// TransferIso is SubmitIso and Wait.
func (e *endpoint) TransferIso(ctx context.Context, buf []byte, lengths []int) ([]IsoPacket, error) {
	x, err := e.SubmitIso(buf, lengths)
	if err != nil {
		return nil, err
	}
	return x.Wait(ctx)
}

// InEndpoint represents an IN endpoint open for transfer.
// InEndpoint implements the io.Reader interface.
// For high-throughput transfers, consider creating a buffered read stream
// through InEndpoint.ReadStream.
type InEndpoint struct {
	*endpoint
}

// Read reads data from an IN endpoint. Read returns number of bytes obtained
// from the endpoint. Read may return non-zero length even if
// the returned error is not nil (partial read).
// It's recommended to use buffer sizes that are multiples of
// EndpointDesc.MaxPacketSize to avoid overflows.
// When a USB device receives a read request, it doesn't know the size of the
// buffer and may send too much data in one packet to fit in the buffer.
// If that happens, Read will return an error signaling an overflow.
// See http://libusb.sourceforge.net/api-1.0/libusb_packetoverflow.html
// for more details.
func (e *InEndpoint) Read(buf []byte) (int, error) {
	return e.transfer(context.Background(), buf)
}

// ReadContext reads data from an IN endpoint. ReadContext returns number of
// bytes obtained from the endpoint. ReadContext may return non-zero length
// even if the returned error is not nil (partial read).
// The passed context can be used to control the cancellation of the read. If
// the context is cancelled, ReadContext will cancel the underlying transfers,
// resulting in TransferCancelled error.
// It's recommended to use buffer sizes that are multiples of
// EndpointDesc.MaxPacketSize to avoid overflows.
// When a USB device receives a read request, it doesn't know the size of the
// buffer and may send too much data in one packet to fit in the buffer.
// If that happens, Read will return an error signaling an overflow.
// See http://libusb.sourceforge.net/api-1.0/libusb_packetoverflow.html
// for more details.
func (e *InEndpoint) ReadContext(ctx context.Context, buf []byte) (int, error) {
	return e.transfer(ctx, buf)
}

// OutEndpoint represents an OUT endpoint open for transfer.
type OutEndpoint struct {
	*endpoint
}

// Write writes data to an OUT endpoint. Write returns number of bytes comitted
// to the endpoint. Write may return non-zero length even if the returned error
// is not nil (partial write).
func (e *OutEndpoint) Write(buf []byte) (int, error) {
	return e.transfer(context.Background(), buf)
}

// WriteContext writes data to an OUT endpoint. WriteContext returns number of
// bytes comitted to the endpoint. WriteContext may return non-zero length even
// if the returned error is not nil (partial write).
// The passed context can be used to control the cancellation of the write. If
// the context is cancelled, WriteContext will cancel the underlying transfers,
// resulting in TransferCancelled error.
func (e *OutEndpoint) WriteContext(ctx context.Context, buf []byte) (int, error) {
	return e.transfer(ctx, buf)
}
