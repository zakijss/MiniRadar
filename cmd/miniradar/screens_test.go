package main

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zakijss/minideck/ui"
)

// TestScreens draws the app's screens with made-up devices to PNG files
// for review. It runs only with MINIDECK_GALLERY=DIR set.
func TestScreens(t *testing.T) {
	dir := os.Getenv("MINIDECK_GALLERY")
	if dir == "" {
		t.Skip("set MINIDECK_GALLERY=DIR to draw the screens")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sub, err := newSubnet("wlan0", netip.MustParseAddr("192.168.1.27"), 24, net.HardwareAddr{0x02, 0x11, 0x22, 0x33, 0x44, 0x55})
	if err != nil {
		t.Fatal(err)
	}
	scan := func(phase, done int) *Scan {
		s := NewScan(sub, func() {})
		s.gw = netip.MustParseAddr("192.168.1.1")
		for _, h := range []Host{
			{Addr: s.gw, MAC: "A0:04:60:11:22:33", Ports: []int{53, 80, 443}},
			{Addr: netip.MustParseAddr("192.168.1.12"), MAC: "3C:7C:3F:12:AB:9E", Name: "Gaming-PC", NameFrom: "NetBIOS", Ports: []int{445, 3389}},
			{Addr: netip.MustParseAddr("192.168.1.20"), MAC: "B8:27:EB:01:02:03", Name: "nas", NameFrom: "mDNS", Ports: []int{22, 80, 445, 8096}},
			{Addr: netip.MustParseAddr("192.168.1.27"), MAC: "02:11:22:33:44:55", Ports: []int{22}},
			{Addr: netip.MustParseAddr("192.168.1.41"), MAC: "DA:A1:19:00:00:01", Ping: true},
		} {
			s.update(h.Addr, func(x *Host) {
				x.MAC, x.Name, x.NameFrom, x.Ports, x.Ping = h.MAC, h.Name, h.NameFrom, h.Ports, h.Ping
			})
		}
		s.prog = Progress{Phase: phase, Total: 254, Started: time.Now()}
		s.done.Store(int64(done))
		return s
	}
	closed := make(chan ui.Event)
	close(closed)
	shot := func(name string, a *app, draw func(*app)) {
		t.Setenv("MINIDECK_UI_PNG", filepath.Join(dir, "network-"+name+".png"))
		scr, err := ui.Open()
		if err != nil {
			t.Fatal(err)
		}
		a.scr, a.input, a.sub, a.changed = scr, closed, sub, make(chan struct{}, 1)
		draw(a)
	}
	shot("devices", &app{scan: scan(phaseDone, 0)}, (*app).drawHosts)
	shot("devices-by-name", &app{cfg: Settings{Sort: "name"}, scan: scan(phaseDone, 0)}, (*app).drawHosts)
	shot("scanning", &app{scan: scan(phaseFind, 120)}, (*app).drawHosts)
	shot("device", &app{scan: scan(phaseDone, 0)}, func(a *app) { a.details(netip.MustParseAddr("192.168.1.20")) })
	shot("settings", &app{cfg: Settings{Sort: "name"}}, (*app).settings)
	shot("device-noports", &app{scan: scan(phaseDone, 0)}, func(a *app) {
		a.scan.prog.SkipPorts = true
		a.details(netip.MustParseAddr("192.168.1.41"))
	})
	shot("devices-new-saved", &app{known: map[string]bool{"A0:04:60:11:22:33": true}, scan: scan(phaseDone, 0),
		devices: []Device{{Name: "Gaming PC", MAC: "3C:7C:3F:12:AB:9E"}}}, (*app).drawHosts)
	shot("wake", &app{wake: true, scan: scan(phaseDone, 0), devices: []Device{{Name: "Gaming PC", MAC: "3C:7C:3F:12:AB:9E"}, {Name: "NAS", MAC: "B8:27:EB:01:02:03"}}}, (*app).drawWake)
}
