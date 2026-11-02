package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// maxBits caps a scan at a /24 (254 addresses), even on a wider network.
const maxBits = 24

// Subnet is the Miyoo's own Wi-Fi network: the only one MiniRadar scans.
type Subnet struct {
	Iface     string
	Self      netip.Addr
	MAC       net.HardwareAddr
	Net       netip.Prefix // the interface's network
	Scan      netip.Prefix // what is scanned: Net, or the /24 around Self if Net is wider
	Broadcast netip.Addr   // Net's broadcast address, for Wake-on-LAN
}

// iface is wlan0 on the Miyoo. NETWORK_IFACE picks another of this
// machine's own interfaces, for trying the app on a computer.
func iface() string {
	if v := os.Getenv("NETWORK_IFACE"); v != "" {
		return v
	}
	return "wlan0"
}

var (
	errWiFiOff   = message("Wi-Fi is off. Turn it on in Settings, then press X.")
	errNoAddress = message("Wi-Fi isn't connected to a network yet. Press X to try again.")
)

// localSubnet reads the Wi-Fi interface's IPv4 address and mask.
func localSubnet() (Subnet, error) {
	name := iface()
	ifc, err := net.InterfaceByName(name)
	if err != nil || ifc.Flags&net.FlagUp == 0 {
		return Subnet{}, errWiFiOff
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return Subnet{}, err
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipn.IP.To4()
		ones, bits := ipn.Mask.Size()
		if ip4 == nil || bits != 32 {
			continue
		}
		return newSubnet(name, netip.AddrFrom4([4]byte(ip4)), ones, ifc.HardwareAddr)
	}
	return Subnet{}, errNoAddress
}

func newSubnet(name string, self netip.Addr, bits int, mac net.HardwareAddr) (Subnet, error) {
	if !self.Is4() || bits < 0 || bits > 32 {
		return Subnet{}, fmt.Errorf("unusable address %v/%d", self, bits)
	}
	n := netip.PrefixFrom(self, bits).Masked()
	return Subnet{
		Iface:     name,
		Self:      self,
		MAC:       mac,
		Net:       n,
		Scan:      capPrefix(self, bits),
		Broadcast: lastAddr(n),
	}, nil
}

// capPrefix is the network to scan: self's network, narrowed to the /24
// around self when it is wider.
func capPrefix(self netip.Addr, bits int) netip.Prefix {
	return netip.PrefixFrom(self, max(bits, maxBits)).Masked()
}

// lastAddr is the highest address in p (its broadcast address).
func lastAddr(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr().As4()
	v := uint64(binary.BigEndian.Uint32(a[:])) | (uint64(1)<<(32-p.Bits()) - 1)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	return netip.AddrFrom4(b)
}

// Hosts lists the addresses to probe: every address in Scan but the
// network and broadcast addresses (a /31 or /32 has none to leave out).
// Self is included: the Miyoo's own open ports are worth seeing too.
func (s Subnet) Hosts() []netip.Addr {
	first, last := s.Scan.Addr(), lastAddr(s.Scan)
	if s.Scan.Bits() < 31 {
		first, last = first.Next(), last.Prev()
	}
	var out []netip.Addr
	for a := first; a.IsValid() && a.Compare(last) <= 0; a = a.Next() {
		out = append(out, a)
	}
	return out
}

// parseARP reads /proc/net/arp and returns the MAC of every complete entry
// on the interface (all interfaces if iface is "").
func parseARP(r io.Reader, iface string) map[netip.Addr]string {
	out := map[netip.Addr]string{}
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 6 || (iface != "" && f[5] != iface) {
			continue
		}
		flags, err := strconv.ParseUint(f[2], 0, 32)
		if err != nil || flags&0x2 == 0 { // ATF_COM: the address is resolved
			continue
		}
		ip, err := netip.ParseAddr(f[0])
		if err != nil || !ip.Is4() {
			continue
		}
		mac, err := ParseMAC(f[3])
		if err != nil || isZero(mac) {
			continue
		}
		out[ip] = FormatMAC(mac)
	}
	return out
}

func isZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// parseRoute finds the default gateway on iface in /proc/net/route.
func parseRoute(r io.Reader, iface string) netip.Addr {
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || f[0] != iface || f[1] != "00000000" {
			continue
		}
		v, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil || v == 0 {
			continue
		}
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(v)) // stored in host (little-endian) order
		return netip.AddrFrom4(b)
	}
	return netip.Addr{}
}

var (
	arpPath   = "/proc/net/arp"
	routePath = "/proc/net/route"
)

func readARP(iface string) map[netip.Addr]string {
	f, err := os.Open(arpPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	return parseARP(f, iface)
}

func gateway(iface string) netip.Addr {
	f, err := os.Open(routePath)
	if err != nil {
		return netip.Addr{}
	}
	defer f.Close()
	return parseRoute(f, iface)
}
