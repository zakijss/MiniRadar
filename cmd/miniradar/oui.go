package main

import (
	_ "embed"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// oui.txt lists the MAC prefixes of about 180 vendors common on home
// networks (see mkoui.go), not the whole IEEE registry.
//
//go:embed oui.txt
var ouiText string

var oui struct {
	once  sync.Once
	names []string
	table []uint64 // prefix<<32 | index into names, sorted
}

// loadOUI parses the embedded table into oui.
func loadOUI() {
	for _, line := range strings.Split(ouiText, "\n") {
		name, list, ok := strings.Cut(line, "\t")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		idx := uint64(len(oui.names))
		oui.names = append(oui.names, name)
		for _, h := range strings.Fields(list) {
			if v, err := strconv.ParseUint(h, 16, 32); err == nil && v < 1<<24 {
				oui.table = append(oui.table, v<<32|idx)
			}
		}
	}
	slices.Sort(oui.table)
}

// Vendor names the maker of a network card from its MAC's first three
// bytes, or "Private MAC" for a randomised address (phones use one per
// network), or "".
func Vendor(mac net.HardwareAddr) string {
	if len(mac) < 3 {
		return ""
	}
	oui.once.Do(loadOUI)
	key := uint64(mac[0])<<16 | uint64(mac[1])<<8 | uint64(mac[2])
	i, _ := slices.BinarySearchFunc(oui.table, key, func(e, k uint64) int {
		switch {
		case e>>32 < k:
			return -1
		case e>>32 > k:
			return 1
		}
		return 0
	})
	if i < len(oui.table) && oui.table[i]>>32 == key {
		return oui.names[oui.table[i]&0xffffffff]
	}
	if mac[0]&0x02 != 0 { // locally administered
		return "Private MAC"
	}
	return ""
}

// vendorOf is Vendor for a MAC address written as text.
func vendorOf(mac string) string {
	m, err := ParseMAC(mac)
	if err != nil {
		return ""
	}
	return Vendor(m)
}
