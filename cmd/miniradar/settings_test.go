package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zakijss/minideck/ui"
)

// A missing settings.json keeps today's behaviour, and the settings save
// and load back.
func TestSettingsFile(t *testing.T) {
	dir := t.TempDir()
	s := loadSettings(dir)
	if s.Sort != "" || !s.ports() {
		t.Fatalf("defaults: %+v", s)
	}
	off := false
	s = Settings{Sort: "maker", Ports: &off}
	if err := saveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	got := loadSettings(dir)
	if got.Sort != "maker" || got.ports() {
		t.Fatalf("loaded %+v", got)
	}
	// an order this version doesn't know falls back to the address
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"sort":"colour"}`), 0o644)
	if got := loadSettings(dir); got.Sort != "" || !got.ports() {
		t.Fatalf("unknown sort: %+v", got)
	}
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{`), 0o644)
	if got := loadSettings(dir); got.Sort != "" || !got.ports() {
		t.Fatalf("broken file: %+v", got)
	}
}

// Each row steps through its values both ways and back to the start.
func TestSettingsStep(t *testing.T) {
	var s Settings
	var seen []string
	for range sortOrders {
		s.step(rowSort, 1)
		seen = append(seen, s.Sort)
	}
	if strings.Join(seen, ",") != "name,maker," {
		t.Errorf("forwards: %q", seen)
	}
	s.step(rowSort, -1)
	if s.Sort != "maker" {
		t.Errorf("back from address: %q", s.Sort)
	}
	s.step(rowPorts, 1)
	if s.ports() {
		t.Error("ports still on")
	}
	s.step(rowPorts, -1)
	if !s.ports() {
		t.Error("ports still off")
	}
	items := settingsItems(Settings{Sort: "name"})
	if items[0].Detail != "Name" || items[1].Detail != "On" || items[len(items)-1].Label != "Appearance" {
		t.Errorf("rows: %+v", items)
	}
}

func hostOrder(hs []Host) string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Addr.String())
	}
	return strings.Join(out, " ")
}

func TestSortHosts(t *testing.T) {
	ip := netip.MustParseAddr
	hosts := func() []Host {
		return []Host{
			{Addr: ip("192.168.1.1"), Router: true, Vendor: "TP-Link"},
			{Addr: ip("192.168.1.5")},
			{Addr: ip("192.168.1.9"), Name: "nas", Vendor: "Synology"},
			{Addr: ip("192.168.1.12"), Name: "Gaming-PC", Vendor: "Intel"},
			{Addr: ip("192.168.1.30"), Vendor: "Apple"},
			{Addr: ip("192.168.1.40")},
		}
	}
	for _, c := range []struct{ by, want string }{
		{"", "192.168.1.1 192.168.1.5 192.168.1.9 192.168.1.12 192.168.1.30 192.168.1.40"},
		// Apple device, Gaming-PC, nas, Router; then unnamed by address
		{"name", "192.168.1.30 192.168.1.12 192.168.1.9 192.168.1.1 192.168.1.5 192.168.1.40"},
		// Apple, Intel, Synology, TP-Link; then unknown makers by address
		{"maker", "192.168.1.30 192.168.1.12 192.168.1.9 192.168.1.1 192.168.1.5 192.168.1.40"},
	} {
		hs := hosts()
		sortHosts(hs, c.by)
		if got := hostOrder(hs); got != c.want {
			t.Errorf("sort %q:\n got %s\nwant %s", c.by, got, c.want)
		}
	}
	hs := hosts()
	hs[4].Vendor = "Zyxel"
	sortHosts(hs, "maker")
	if got := hostOrder(hs); !strings.HasPrefix(got, "192.168.1.12 192.168.1.9 192.168.1.1 192.168.1.30 ") {
		t.Errorf("maker sort: %s", got)
	}
}

// With ports off, a scan says so on the device page and its progress bar
// spends the port share on finding devices.
func TestSkipPorts(t *testing.T) {
	h := Host{Addr: netip.MustParseAddr("192.168.1.9")}
	items := detailItems(h, Progress{Phase: phaseFind, SkipPorts: true})
	if last := items[len(items)-1]; last.Label != "Ports not checked" {
		t.Errorf("last row %+v", last)
	}
	if f := (Progress{Phase: phaseFind, Done: 1, Total: 2, SkipPorts: true}).Fraction(); f != 0.45 {
		t.Errorf("fraction %v", f)
	}
	if f := (Progress{Phase: phaseFind, Done: 1, Total: 2}).Fraction(); f != 0.25 {
		t.Errorf("fraction with ports %v", f)
	}
}

// START opens Settings on both tabs; RIGHT on the first row changes the
// sort, which is saved at once.
func TestStartOpensSettings(t *testing.T) {
	for _, wake := range []bool{false, true} {
		scr, _ := lookScreen(t)
		dir := t.TempDir()
		a := &app{scr: scr, input: presses(ui.Start, ui.Right, ui.Right, ui.Left, ui.Down, ui.A, ui.B, ui.B), dir: dir,
			changed: make(chan struct{}, 1), wake: wake, subErr: message("Not connected to Wi-Fi.")}
		a.run()
		if a.cfg.Sort != "name" || a.cfg.ports() {
			t.Errorf("wake %v: settings %+v", wake, a.cfg)
		}
		if got := loadSettings(dir); got.Sort != "name" || got.ports() {
			t.Errorf("wake %v: saved %+v", wake, got)
		}
	}
}
