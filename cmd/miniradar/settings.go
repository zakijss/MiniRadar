package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zakijss/minideck/atomicfile"
	"github.com/zakijss/minideck/ui"
)

// Settings is settings.json. A missing file or field keeps the defaults:
// devices sorted by address, and common ports checked.
type Settings struct {
	// Sort orders the device list: "" (by address), "name" or "maker".
	Sort string `json:"sort,omitempty"`
	// Ports checks the common ports on each device found (default on).
	// Off, a scan only finds devices and their names, which is quicker.
	Ports *bool `json:"scan_ports,omitempty"`
}

// The device list orders, in the order Settings steps through them.
var sortOrders = []string{"", "name", "maker"}

// ports reports whether scans check the common ports.
func (s Settings) ports() bool { return s.Ports == nil || *s.Ports }

// loadSettings reads settings.json; a missing or unreadable file gives the
// defaults.
func loadSettings(dir string) Settings {
	var s Settings
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("settings: %v", err)
		}
		return s
	}
	if err := json.Unmarshal(b, &s); err != nil {
		log.Printf("settings.json: %v", err)
		return Settings{}
	}
	if !slices.Contains(sortOrders, s.Sort) {
		s.Sort = ""
	}
	return s
}

// saveSettings writes settings.json.
func saveSettings(dir string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, "settings.json"), append(b, '\n'), 0o644)
}

// sortHosts orders hosts (sorted by address) for the device list. By name,
// devices with a name come first, A to Z, then the rest by address; by
// maker, devices are grouped by maker, A to Z, with unknown makers last.
func sortHosts(hosts []Host, by string) {
	named := func(h Host) bool { return displayName(h) != h.Addr.String() }
	switch by {
	case "name":
		slices.SortStableFunc(hosts, func(a, b Host) int {
			na, nb := named(a), named(b)
			switch {
			case na && !nb:
				return -1
			case !na && nb:
				return 1
			case !na:
				return 0
			}
			return cmp.Compare(strings.ToLower(displayName(a)), strings.ToLower(displayName(b)))
		})
	case "maker":
		slices.SortStableFunc(hosts, func(a, b Host) int {
			switch {
			case a.Vendor != "" && b.Vendor == "":
				return -1
			case a.Vendor == "" && b.Vendor != "":
				return 1
			}
			return cmp.Compare(strings.ToLower(a.Vendor), strings.ToLower(b.Vendor))
		})
	}
}

// hosts returns the devices found so far, in the order Settings picks.
func (a *app) hosts() []Host {
	if a.scan == nil {
		return nil
	}
	hs := a.scan.Hosts()
	sortHosts(hs, a.cfg.Sort)
	return hs
}

// Settings rows, by Value.
const (
	rowSort = iota + 1
	rowPorts
	rowLook
)

// settingsItems is the Settings list.
func settingsItems(s Settings) []ui.Item {
	sortRow := ui.Item{Label: "Sort devices", Icon: "list", Detail: "Address", Sub: "Lowest address first", Value: rowSort}
	switch s.Sort {
	case "name":
		sortRow.Detail, sortRow.Sub = "Name", "Named devices first, A to Z"
	case "maker":
		sortRow.Detail, sortRow.Sub = "Maker", "Grouped by maker, A to Z"
	}
	ports := ui.Item{Label: "Common ports", Icon: "server", Detail: "On",
		Sub: fmt.Sprintf("Each scan checks %d common ports", len(services)), Value: rowPorts}
	if !s.ports() {
		ports.Detail, ports.Sub = "Off", "Quicker scans, without open ports"
	}
	look := ui.AppearanceItem()
	look.Value = rowLook
	return []ui.Item{sortRow, ports, look}
}

// step moves the setting on row by step (1 or -1).
func (s *Settings) step(row, step int) {
	switch row {
	case rowSort:
		i := slices.Index(sortOrders, s.Sort)
		s.Sort = sortOrders[((i+step)%len(sortOrders)+len(sortOrders))%len(sortOrders)]
	case rowPorts:
		on := !s.ports()
		s.Ports = &on
	}
}

// settings is the Settings screen: A, left and right change the selected
// setting, which is saved at once. A port change applies from the next
// scan.
func (a *app) settings() {
	var l ui.List
	for {
		l.Items = settingsItems(a.cfg)
		s := a.scr
		top := s.StaticFrame("Settings", "Left and right change a setting",
			ui.Hint{Button: "A", Label: "Change"}, ui.Hint{Button: "B", Label: "Back"})
		l.Draw(s, image.Rect(20, top, ui.W-26, ui.H-58))
		s.Present()
		ev, ok, frame := s.Next(a.input)
		if !ok {
			return
		}
		if frame || !ev.Pressed {
			continue
		}
		step := 0
		switch ev.Button {
		case ui.A, ui.Right:
			step = 1
		case ui.Left:
			step = -1
		case ui.B, ui.Menu, ui.Start:
			return
		default:
			l.Handle(ev)
			continue
		}
		row, _ := l.Items[l.Sel].Value.(int)
		if row == rowLook {
			if ev.Button == ui.A {
				s.Appearance(a.input)
			}
			continue
		}
		old := a.cfg
		a.cfg.step(row, step)
		if err := saveSettings(a.dir, a.cfg); err != nil {
			a.cfg = old
			s.Notice(a.input, "Couldn't save", err.Error())
		}
	}
}
