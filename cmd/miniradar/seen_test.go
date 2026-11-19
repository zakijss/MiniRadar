package main

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// A scan's MACs go first, upper case and once each; older ones follow and
// the list stays capped.
func TestMergeSeen(t *testing.T) {
	got := mergeSeen([]string{"aa:bb:cc:dd:ee:01", "", "AA:BB:CC:DD:EE:02"}, []string{"AA:BB:CC:DD:EE:02", "AA:BB:CC:DD:EE:03"})
	want := []string{"AA:BB:CC:DD:EE:01", "AA:BB:CC:DD:EE:02", "AA:BB:CC:DD:EE:03"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	var many []string
	for i := range maxSeen + 10 {
		many = append(many, fmt.Sprintf("AA:BB:CC:DD:%02X:%02X", i>>8, i&0xff))
	}
	if got := mergeSeen([]string{"AA:BB:CC:00:00:00"}, many); len(got) != maxSeen || got[0] != "AA:BB:CC:00:00:00" {
		t.Errorf("len %d, first %s: want %d, the new one first", len(got), got[0], maxSeen)
	}
}

// seen.json survives a round trip; nothing is New until a scan has been
// recorded, and then only devices it didn't find.
func TestSeenNew(t *testing.T) {
	dir := t.TempDir()
	a := &app{dir: dir}
	old := Host{MAC: "aa:bb:cc:dd:ee:01"}
	if a.isNew(old) {
		t.Error("no earlier scans: nothing should be New")
	}
	if err := saveSeen(dir, []string{"AA:BB:CC:DD:EE:01"}); err != nil {
		t.Fatal(err)
	}
	a.known = map[string]bool{}
	for _, m := range loadSeen(dir) {
		a.known[m] = true
	}
	if a.isNew(old) {
		t.Error("a device seen before is New")
	}
	if !a.isNew(Host{MAC: "AA:BB:CC:DD:EE:09"}) {
		t.Error("an unseen device isn't New")
	}
	if a.isNew(Host{MAC: "AA:BB:CC:DD:EE:09", Self: true}) || a.isNew(Host{}) {
		t.Error("the Miyoo, or a device without a MAC, is New")
	}
}

// A device saved for Wake is listed under its saved name.
func TestSavedName(t *testing.T) {
	a := &app{devices: []Device{{Name: "Gaming PC", MAC: "3C:7C:3F:12:AB:9E"}}}
	h := a.savedName(Host{Addr: netip.MustParseAddr("192.168.1.12"), MAC: "3c:7c:3f:12:ab:9e", Name: "DESKTOP-1", NameFrom: "NetBIOS"})
	if h.Name != "Gaming PC" || h.NameFrom != "saved" {
		t.Errorf("got %q from %q", h.Name, h.NameFrom)
	}
	if h := a.savedName(Host{MAC: "00:11:22:33:44:55", Name: "nas"}); h.Name != "nas" {
		t.Errorf("an unsaved device renamed to %q", h.Name)
	}
}

// A finished scan stamps when and where it found each device saved for
// Wake, and keeps that in devices.json.
func TestRecordSeenSaved(t *testing.T) {
	dir := t.TempDir()
	s := NewScan(Subnet{}, func() {})
	addr := netip.MustParseAddr("192.168.1.12")
	s.hosts[addr] = &Host{Addr: addr, MAC: "3c:7c:3f:12:ab:9e"}
	s.prog.Phase = phaseDone
	a := &app{dir: dir, scan: s, devices: []Device{{Name: "Gaming PC", MAC: "3C:7C:3F:12:AB:9E", IP: "192.168.1.5"}, {Name: "NAS", MAC: "00:11:22:33:44:55"}}}
	a.recordSeen()
	ds, err := loadDevices(dir)
	if err != nil || len(ds) != 2 {
		t.Fatalf("devices.json: %v %+v", err, ds)
	}
	if ds[0].Seen.IsZero() || ds[0].IP != "192.168.1.12" {
		t.Errorf("found device: %+v", ds[0])
	}
	if !ds[1].Seen.IsZero() {
		t.Errorf("device not found was stamped: %+v", ds[1])
	}
}

// Probes start no faster than the pace, however many workers there are.
func TestParallelPace(t *testing.T) {
	start := time.Now()
	var n atomic.Int64
	parallel(context.Background(), 20, 8, 5*time.Millisecond, func(int) { n.Add(1) })
	if d := time.Since(start); n.Load() != 20 || d < 90*time.Millisecond {
		t.Errorf("%d probes in %v", n.Load(), d)
	}
}
