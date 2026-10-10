// Package sourcebroker is an opt-in process boundary for the independently
// authored USB/IP importer client. It does not attach devices to the OS and is
// not the stock agent's USBP/VHCI broker protocol.
package sourcebroker

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"usbridge_agent/internal/componentjson"
)

const Profile = "source-broker-v1"
const Role = "usbip-importer-client"
const MaxMessage = 64 << 10
const MaxTransfer = 16 << 10

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
var busPattern = regexp.MustCompile(`^[0-9]{1,5}-[0-9]{1,5}(\.[0-9]{1,5})*$`)

type Launch struct {
	Version         int    `json:"version"`
	Address         string `json:"address"`
	CertificateFile string `json:"certificate_file"`
	KeyFile         string `json:"key_file"`
	CAFile          string `json:"ca_file"`
	ServerName      string `json:"server_name"`
	ServerPin       string `json:"server_pin"`
	BusID           string `json:"bus_id"`
	Consent         bool   `json:"consent"`
	Capacity        int    `json:"capacity"`
}

func (r Launch) Validate() error {
	if r.Version != 1 || !r.Consent {
		return errors.New("source-broker v1 needs explicit device consent")
	}
	host, port, err := net.SplitHostPort(r.Address)
	ip := net.ParseIP(host)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || n < 1 || n > 65535 || ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return errors.New("source-broker address must be a literal loopback or private IP and port")
	}
	for _, p := range []string{r.CertificateFile, r.KeyFile, r.CAFile} {
		if !filepath.IsAbs(p) || len(p) > 4096 || strings.ContainsAny(p, "\x00\r\n") {
			return errors.New("source-broker credentials require explicit absolute file paths")
		}
	}
	pin, err := hex.DecodeString(r.ServerPin)
	if err != nil || len(pin) != 32 || bytes.Equal(pin, make([]byte, 32)) {
		return errors.New("source-broker requires an exact SHA-256 TLS server pin")
	}
	if r.ServerName == "" || len(r.ServerName) > 253 || strings.ContainsAny(r.ServerName, " /\\\x00\r\n") {
		return errors.New("source-broker requires a TLS server name")
	}
	if !busPattern.MatchString(r.BusID) || len(r.BusID) > 31 || r.Capacity < 1 || r.Capacity > 16 {
		return errors.New("source-broker requires a selected bus ID and bounded capacity")
	}
	return nil
}
func decode(raw []byte, out any) error {
	if len(raw) > MaxMessage {
		return errors.New("source-broker message exceeds limit")
	}
	if err := componentjson.Decode(raw, out); err != nil {
		return errors.New("invalid source-broker JSON")
	}
	return nil
}

type Command struct {
	Op        string   `json:"op"`
	ID        string   `json:"id,omitempty"`
	Direction uint32   `json:"direction,omitempty"`
	Endpoint  uint32   `json:"endpoint,omitempty"`
	Length    uint32   `json:"length,omitempty"`
	Flags     uint32   `json:"flags,omitempty"`
	Setup     []uint16 `json:"setup,omitempty"`
	Data      string   `json:"data,omitempty"`
	TimeoutMS int      `json:"timeout_ms,omitempty"`
}

func (c Command) Validate() error {
	if c.Op == "close" {
		if c.ID != "" || c.Direction != 0 || c.Endpoint != 0 || c.Length != 0 || c.Flags != 0 || c.TimeoutMS != 0 || len(c.Setup) != 0 || c.Data != "" {
			return errors.New("source-broker close has transfer fields")
		}
		return nil
	}
	if !idPattern.MatchString(c.ID) {
		return errors.New("invalid source-broker transfer ID")
	}
	if c.Op == "cancel" {
		if c.Direction != 0 || c.Endpoint != 0 || c.Length != 0 || c.Flags != 0 || c.TimeoutMS != 0 || len(c.Setup) != 0 || c.Data != "" {
			return errors.New("source-broker cancel has transfer fields")
		}
		return nil
	}
	if c.Op != "transfer" || c.Direction > 1 || c.Endpoint > 15 || c.Length > MaxTransfer || c.TimeoutMS < 0 || c.TimeoutMS > 10000 || (len(c.Setup) != 0 && len(c.Setup) != 8) {
		return errors.New("unsupported source-broker command or transfer bounds")
	}
	for _, v := range c.Setup {
		if v > 255 {
			return errors.New("invalid source-broker setup byte")
		}
	}
	data, err := base64.StdEncoding.Strict().DecodeString(c.Data)
	if err != nil || len(data) > MaxTransfer || len(c.Data) != base64.StdEncoding.EncodedLen(len(data)) {
		return errors.New("invalid source-broker transfer data")
	}
	if (c.Direction == 1 && len(data) != 0) || (c.Direction == 0 && len(data) != int(c.Length)) {
		return errors.New("source-broker transfer data/length mismatch")
	}
	return nil
}

type Device struct {
	BusID     string `json:"bus_id"`
	Bus       uint32 `json:"bus"`
	Number    uint32 `json:"number"`
	VendorID  uint16 `json:"vendor_id"`
	ProductID uint16 `json:"product_id"`
}
type Event struct {
	Event           string  `json:"event"`
	ProtocolVersion int     `json:"protocol_version,omitempty"`
	Role            string  `json:"role,omitempty"`
	OSAttached      bool    `json:"os_attached"`
	Capacity        int     `json:"capacity,omitempty"`
	Device          *Device `json:"device,omitempty"`
	ID              string  `json:"id,omitempty"`
	Status          int32   `json:"status,omitempty"`
	ActualLength    uint32  `json:"actual_length,omitempty"`
	Data            string  `json:"data,omitempty"`
	Error           string  `json:"error,omitempty"`
}

func (e Event) validateReady(r Launch) error {
	if e.Event != "ready" || e.ProtocolVersion != 1 || e.Role != Role || e.OSAttached || e.Capacity != r.Capacity || e.Device == nil || e.Device.BusID != r.BusID {
		return errors.New("source-broker readiness identity, role, or device mismatch")
	}
	return nil
}
func (e Event) validateStatus() error {
	if e.OSAttached || e.Device != nil || e.ProtocolVersion != 0 || e.Role != "" {
		return errors.New("source-broker status claimed an unsupported device attachment")
	}
	switch e.Event {
	case "complete":
		if !idPattern.MatchString(e.ID) || e.ActualLength > MaxTransfer {
			return errors.New("source-broker completion exceeds bounds")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(e.Data)
		if err != nil || len(data) > MaxTransfer || len(data) > int(e.ActualLength) || len(e.Data) != base64.StdEncoding.EncodedLen(len(data)) {
			return errors.New("invalid source-broker completion payload")
		}
		if e.Error != "" && e.Error != "canceled" && e.Error != "deadline_exceeded" && e.Error != "transfer_failed" {
			return errors.New("unrecognized source-broker transfer error")
		}
	case "cancel_requested":
		if !idPattern.MatchString(e.ID) {
			return errors.New("invalid source-broker cancellation ID")
		}
	case "error":
		allowed := map[string]bool{"unknown_transfer": true, "invalid_command": true, "invalid_transfer": true, "invalid_operation": true, "duplicate_transfer": true, "capacity_exhausted": true, "session_closed": true}
		if !allowed[e.Error] {
			return errors.New("unrecognized source-broker protocol error")
		}
	case "stopped":
	default:
		return errors.New("unexpected source-broker event")
	}
	return nil
}
