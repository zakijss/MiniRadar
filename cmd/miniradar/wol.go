package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zakijss/minideck/atomicfile"
)

var errMAC = message("A MAC address is six pairs of hex digits, like 3C:7C:3F:12:AB:9E.")

// ParseMAC reads a MAC address written with colons, dashes, dots or
// nothing between the digits.
func ParseMAC(s string) (net.HardwareAddr, error) {
	var digits []byte
	for _, c := range strings.TrimSpace(s) {
		switch {
		case c == ':' || c == '-' || c == '.' || c == ' ':
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			digits = append(digits, byte(c))
		default:
			return nil, errMAC
		}
	}
	if len(digits) != 12 {
		return nil, errMAC
	}
	mac := make(net.HardwareAddr, 6)
	if _, err := hex.Decode(mac, digits); err != nil {
		return nil, errMAC
	}
	return mac, nil
}

// FormatMAC writes a MAC as AA:BB:CC:DD:EE:FF.
func FormatMAC(m net.HardwareAddr) string { return strings.ToUpper(m.String()) }

// MagicPacket is the Wake-on-LAN frame for mac: six 0xFF bytes, then the
// MAC sixteen times.
func MagicPacket(mac net.HardwareAddr) []byte {
	p := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		p = append(p, 0xff)
	}
	for i := 0; i < 16; i++ {
		p = append(p, mac[:6]...)
	}
	return p
}

// wakePorts are where magic packets go: 9 (discard) is the usual one, 7
// (echo) the older one some network cards listen on.
var wakePorts = []int{9, 7}

// Wake sends the magic packet for mac to the network's broadcast address.
// Wi-Fi sends broadcasts once, unacknowledged, so it goes three times: the
// first synchronously (to report errors), the others in the background.
func Wake(sub Subnet, mac net.HardwareAddr) error {
	if len(mac) != 6 {
		return errMAC
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: sub.Self.AsSlice()})
	if err != nil {
		return err
	}
	pkt := MagicPacket(mac)
	send := func() error {
		var first error
		for _, port := range wakePorts {
			if _, err := c.WriteToUDP(pkt, &net.UDPAddr{IP: sub.Broadcast.AsSlice(), Port: port}); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
	if err := send(); err != nil {
		c.Close()
		return err
	}
	go func() {
		defer c.Close()
		for i := 0; i < 2; i++ {
			time.Sleep(150 * time.Millisecond)
			send()
		}
	}()
	return nil
}

// Device is a saved Wake-on-LAN target.
type Device struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	IP   string `json:"ip,omitempty"` // where a scan last saw it (for reference)
}

// loadDevices reads devices.json; a missing file is an empty list.
func loadDevices(dir string) ([]Device, error) {
	b, err := os.ReadFile(filepath.Join(dir, "devices.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		Devices []Device `json:"devices"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("devices.json: %w", err)
	}
	return f.Devices, nil
}

// saveDevices writes devices.json.
func saveDevices(dir string, ds []Device) error {
	if ds == nil {
		ds = []Device{}
	}
	b, err := json.MarshalIndent(struct {
		Devices []Device `json:"devices"`
	}{ds}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, "devices.json"), append(b, '\n'), 0o644)
}
