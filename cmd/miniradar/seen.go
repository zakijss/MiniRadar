package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zakijss/minideck/atomicfile"
)

// seen.json lists the MAC addresses earlier scans found, newest first, so
// a device the network hasn't had before can be marked New.

// maxSeen caps seen.json at a few kilobytes; the oldest drop off first.
const maxSeen = 512

// loadSeen reads seen.json; a missing or unreadable file is an empty list
// (and then nothing is marked New, rather than everything).
func loadSeen(dir string) []string {
	var f struct {
		MACs []string `json:"macs"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "seen.json"))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("seen: %v", err)
		}
		return nil
	}
	if err := json.Unmarshal(b, &f); err != nil {
		log.Printf("seen.json: %v", err)
		return nil
	}
	return f.MACs
}

// saveSeen writes seen.json.
func saveSeen(dir string, macs []string) error {
	b, err := json.Marshal(struct {
		MACs []string `json:"macs"`
	}{macs})
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, "seen.json"), append(b, '\n'), 0o644)
}

// mergeSeen puts the MACs a scan found first, then the older ones it
// didn't find, keeping the newest maxSeen.
func mergeSeen(found, old []string) []string {
	out := make([]string, 0, len(found)+len(old))
	for _, m := range append(slices.Clone(found), old...) {
		m = strings.ToUpper(m)
		if m != "" && !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out[:min(len(out), maxSeen)]
}

// isNew reports whether a scanned device wasn't found by any scan before
// this session's. With no earlier scans at all, nothing is new.
func (a *app) isNew(h Host) bool {
	return len(a.known) > 0 && h.MAC != "" && !h.Self && !a.known[strings.ToUpper(h.MAC)]
}

// recordSeen adds a finished scan's devices to seen.json, once per scan.
func (a *app) recordSeen() {
	if a.scan == nil || a.scan.Running() || a.recorded == a.scan {
		return
	}
	a.recorded = a.scan
	var found []string
	for _, h := range a.scan.Hosts() {
		found = append(found, h.MAC)
	}
	a.seen = mergeSeen(found, a.seen)
	if err := saveSeen(a.dir, a.seen); err != nil {
		log.Printf("seen: %v", err) // only New marks are lost: not worth a notice
	}
}
