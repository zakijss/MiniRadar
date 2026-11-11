package main

import (
	"fmt"
	"net/netip"
	"slices"
	"testing"
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
