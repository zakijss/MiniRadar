package main

import (
	"bytes"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestSubnet(t *testing.T) {
	for _, tc := range []struct {
		self      string
		bits      int
		scan, net string
		bcast     string
		n         int
		first     string
		last      string
	}{
		{"192.168.1.27", 24, "192.168.1.0/24", "192.168.1.0/24", "192.168.1.255", 254, "192.168.1.1", "192.168.1.254"},
		// wider networks are capped at the /24 around the Miyoo, but Wake
		// still uses the real broadcast address
		{"10.1.2.3", 16, "10.1.2.0/24", "10.1.0.0/16", "10.1.255.255", 254, "10.1.2.1", "10.1.2.254"},
		{"10.200.7.9", 8, "10.200.7.0/24", "10.0.0.0/8", "10.255.255.255", 254, "10.200.7.1", "10.200.7.254"},
		{"172.16.5.4", 0, "172.16.5.0/24", "0.0.0.0/0", "255.255.255.255", 254, "172.16.5.1", "172.16.5.254"},
		// narrower ones are kept
		{"192.168.1.37", 28, "192.168.1.32/28", "192.168.1.32/28", "192.168.1.47", 14, "192.168.1.33", "192.168.1.46"},
		{"192.168.1.37", 31, "192.168.1.36/31", "192.168.1.36/31", "192.168.1.37", 2, "192.168.1.36", "192.168.1.37"},
		{"192.168.1.37", 32, "192.168.1.37/32", "192.168.1.37/32", "192.168.1.37", 1, "192.168.1.37", "192.168.1.37"},
	} {
		s, err := newSubnet("wlan0", netip.MustParseAddr(tc.self), tc.bits, nil)
		if err != nil {
			t.Fatal(err)
		}
		hosts := s.Hosts()
		if s.Scan.String() != tc.scan || s.Net.String() != tc.net || s.Broadcast.String() != tc.bcast ||
			len(hosts) != tc.n || hosts[0].String() != tc.first || hosts[len(hosts)-1].String() != tc.last {
			t.Errorf("%s/%d: scan %v net %v bcast %v, %d hosts %v..%v", tc.self, tc.bits, s.Scan, s.Net, s.Broadcast,
				len(hosts), hosts[0], hosts[len(hosts)-1])
		}
		if !slices.Contains(hosts, s.Self) {
			t.Errorf("%s/%d: self not scanned", tc.self, tc.bits)
		}
	}
	if _, err := newSubnet("wlan0", netip.MustParseAddr("fe80::1"), 64, nil); err == nil {
		t.Error("IPv6 accepted")
	}
}

func TestMagicPacket(t *testing.T) {
	mac, err := ParseMAC("3c:7c:3f:12:ab:9e")
	if err != nil {
		t.Fatal(err)
	}
	p := MagicPacket(mac)
	if len(p) != 102 {
		t.Fatalf("length %d", len(p))
	}
	if !bytes.Equal(p[:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Errorf("header % x", p[:6])
	}
	want := []byte{0x3c, 0x7c, 0x3f, 0x12, 0xab, 0x9e}
	for i := 0; i < 16; i++ {
		if got := p[6+6*i : 12+6*i]; !bytes.Equal(got, want) {
			t.Errorf("copy %d: % x", i, got)
		}
	}
}

func TestParseMAC(t *testing.T) {
	for _, in := range []string{"3c:7c:3f:12:ab:9e", "3C-7C-3F-12-AB-9E", "3c7c.3f12.ab9e", "3C7C3F12AB9E", " 3c 7c 3f 12 ab 9e "} {
		m, err := ParseMAC(in)
		if err != nil || FormatMAC(m) != "3C:7C:3F:12:AB:9E" {
			t.Errorf("%q: %v %v", in, m, err)
		}
	}
	for _, in := range []string{"", "3c:7c:3f:12:ab", "3c:7c:3f:12:ab:9e:00", "3c:7c:3f:12:ab:9g", "192.168.1.1"} {
		if _, err := ParseMAC(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestVendor(t *testing.T) {
	for mac, want := range map[string]string{
		"B8:27:EB:01:02:03": "Raspberry Pi",
		"dc:a6:32:aa:bb:cc": "Raspberry Pi",
		"00:11:32:00:00:01": "Synology",
		"24:5E:BE:00:00:01": "QNAP",
		"00:0C:29:12:34:56": "VMware",
		"52:54:00:12:34:56": "QEMU/KVM", // locally administered, but listed
		"3C:22:FB:00:00:01": "Apple",
		"74:AC:B9:00:00:01": "Ubiquiti",
		"02:42:AC:11:00:02": "Private MAC", // Docker, or a phone's random address
		"DA:A1:19:00:00:01": "Private MAC",
		"00:00:0A:00:00:01": "", // a real maker, but not one in the table
	} {
		if got := vendorOf(mac); got != want {
			t.Errorf("%s: %q, want %q", mac, got, want)
		}
	}
	if len(oui.names) > 256 || len(oui.table) < 10000 || !slices.IsSorted(oui.table) {
		t.Errorf("table: %d names, %d prefixes", len(oui.names), len(oui.table))
	}
}

func TestParseARP(t *testing.T) {
	const arp = `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         a0:04:60:11:22:33     *        wlan0
192.168.1.12     0x1         0x2         00:11:32:aa:bb:cc     *        wlan0
192.168.1.40     0x1         0x0         00:00:00:00:00:00     *        wlan0
192.168.1.41     0x1         0x6         b8:27:eb:01:02:03     *        wlan0
10.0.0.5         0x1         0x2         00:0c:29:12:34:56     *        eth0
garbage
`
	got := parseARP(strings.NewReader(arp), "wlan0")
	want := map[netip.Addr]string{
		netip.MustParseAddr("192.168.1.1"):  "A0:04:60:11:22:33",
		netip.MustParseAddr("192.168.1.12"): "00:11:32:AA:BB:CC",
		netip.MustParseAddr("192.168.1.41"): "B8:27:EB:01:02:03",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%v: %q, want %q", k, got[k], v)
		}
	}
	if all := parseARP(strings.NewReader(arp), ""); len(all) != 4 {
		t.Errorf("all interfaces: %v", all)
	}
}

func TestParseRoute(t *testing.T) {
	const route = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
wlan0	0001A8C0	00000000	0001	0	0	0	00FFFFFF	0	0	0
eth0	00000000	0101000A	0003	0	0	0	00000000	0	0	0
wlan0	00000000	0101A8C0	0003	0	0	0	00000000	0	0	0
`
	if gw := parseRoute(strings.NewReader(route), "wlan0"); gw.String() != "192.168.1.1" {
		t.Errorf("gateway %v", gw)
	}
	if gw := parseRoute(strings.NewReader(route), "usb0"); gw.IsValid() {
		t.Errorf("usb0 gateway %v", gw)
	}
}

func TestFormatPorts(t *testing.T) {
	for _, tc := range []struct {
		ports []int
		n     int
		want  string
	}{
		{nil, 2, ""},
		{[]int{22}, 2, "SSH"},
		{[]int{22, 80}, 2, "SSH, HTTP"},
		{[]int{22, 80, 445, 8096}, 2, "SSH, HTTP +2"},
		{[]int{4533, 32400, 61208}, 3, "Navidrome, Plex, Glances"},
		{[]int{12345}, 2, ":12345"},
	} {
		if got := formatPorts(tc.ports, tc.n); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.ports, got, tc.want)
		}
	}
	if !slices.IsSortedFunc(services, func(a, b Service) int { return a.Port - b.Port }) {
		t.Error("services not in port order")
	}
	for _, p := range []int{22, 53, 80, 443, 445, 3389, 5900, 8080, 8096, 32400, 9000, 4533, 61208, 8090} {
		if service(p).Name == "" || strings.HasPrefix(service(p).Name, "Port ") {
			t.Errorf("port %d has no name", p)
		}
	}
	if service(8096).Name != "Jellyfin" || service(1).Name != "Port 1" {
		t.Error("service names")
	}
}

func TestNetBIOS(t *testing.T) {
	q := nbstatQuery(0x1234)
	if len(q) != 50 || q[12] != 0x20 || string(q[13:15]) != "CK" || q[47] != 0x21 {
		t.Fatalf("query % x", q)
	}
	// a node status reply: group name first, then the workstation name
	r := []byte{0x12, 0x34, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	r = append(r, q[12:46]...) // the name again
	r = append(r, 0, 0x21, 0, 1, 0, 0, 0, 0, 0, 0x41, 2)
	entry := func(name string, suffix byte, flags uint16) {
		b := []byte(name + strings.Repeat(" ", 15-len(name)))
		r = append(append(r, b...), suffix, byte(flags>>8), byte(flags))
	}
	entry("WORKGROUP", 0, 0x8400)
	entry("GAMING-PC", 0, 0x0400)
	if got := parseNBSTAT(r); got != "GAMING-PC" {
		t.Errorf("name %q", got)
	}
	for _, bad := range [][]byte{nil, r[:12], r[:50], {0, 0, 0, 0, 0, 0, 0, 0}} {
		if got := parseNBSTAT(bad); got != "" {
			t.Errorf("truncated reply gave %q", got)
		}
	}
}

func TestMDNS(t *testing.T) {
	a := netip.MustParseAddr("192.168.1.12")
	if reverseName(a) != "12.1.168.192.in-addr.arpa." {
		t.Fatal(reverseName(a))
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	b.StartAnswers()
	b.PTRResource(dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(reverseName(a)), Class: dnsmessage.ClassINET, TTL: 10},
		dnsmessage.PTRResource{PTR: dnsmessage.MustNewName("Alexs-MacBook-Air.local.")})
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if got := parseMDNS(msg); got != "Alexs-MacBook-Air" {
		t.Errorf("name %q", got)
	}
	if parseMDNS([]byte{1, 2, 3}) != "" {
		t.Error("garbage parsed")
	}
	for in, want := range map[string]string{"nas.lan.": "nas", "pi.home.arpa": "pi", "Desktop": "Desktop", ".local": ".local"} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q", in, got)
		}
	}
}

func TestEchoChecksum(t *testing.T) {
	p := echoRequest(0xbeef, 7)
	if checksum(p) != 0 { // a correct checksum sums the packet to zero
		t.Errorf("checksum % x", p)
	}
}

func TestDevicesFile(t *testing.T) {
	dir := t.TempDir()
	if ds, err := loadDevices(dir); err != nil || len(ds) != 0 {
		t.Fatal(ds, err)
	}
	in := []Device{{Name: "Gaming PC", MAC: "3C:7C:3F:12:AB:9E", IP: "192.168.1.20"}}
	if err := saveDevices(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := loadDevices(dir)
	if err != nil || !slices.Equal(in, out) {
		t.Fatal(out, err)
	}
	os.WriteFile(dir+"/devices.json", []byte("{"), 0644)
	if _, err := loadDevices(dir); err == nil {
		t.Error("bad JSON accepted")
	}
}

func TestIsEchoReply(t *testing.T) {
	reply := echoRequest(0xbeef, 1)
	reply[0] = 0 // type 0: echo reply
	ipHdr := make([]byte, 20)
	ipHdr[0] = 0x45
	for _, tc := range []struct {
		name string
		m    []byte
		want bool
	}{
		{"bare reply", reply, true},
		{"with IP header", append(slices.Clone(ipHdr), reply...), true},
		{"other id", func() []byte { r := slices.Clone(reply); r[5]++; return r }(), false},
		{"request", echoRequest(0xbeef, 1), false},
		{"empty", nil, false},
		{"short", reply[:7], false},
		{"header only", ipHdr, false},
		{"header length past the end", append([]byte{0x4f}, make([]byte, 30)...), false},
	} {
		if got := isEchoReply(tc.m, 0xbeef); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The parsers read what other devices send and what the kernel reports:
// no input may make them panic.
func TestParsersSurviveGarbage(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	inputs := [][]byte{nil, {0}, {0xff}, bytes.Repeat([]byte{0xc0}, 64), bytes.Repeat([]byte{0x3f}, 300)}
	for range 2000 {
		b := make([]byte, r.IntN(120))
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		inputs = append(inputs, b)
	}
	for _, b := range inputs {
		parseNBSTAT(b)
		parseMDNS(b)
		isEchoReply(b, 1)
		parseARP(bytes.NewReader(b), "")
		parseRoute(bytes.NewReader(b), "wlan0")
		ParseMAC(string(b))
		Vendor(b)
	}
	for _, s := range []string{"IP address\n1.2.3.4 0xzz 0x2 aa *  wlan0", "h\nwlan0 00000000 zz", "h\nwlan0 00000000"} {
		parseARP(strings.NewReader(s), "")
		parseRoute(strings.NewReader(s), "wlan0")
	}
}

// Every vendor in the embedded table can be found by its prefixes.
func TestVendorTable(t *testing.T) {
	Vendor(net.HardwareAddr{0, 0, 0})
	if len(oui.names) < 100 {
		t.Fatalf("only %d vendors loaded", len(oui.names))
	}
	for _, e := range oui.table {
		p := e >> 32
		mac := net.HardwareAddr{byte(p >> 16), byte(p >> 8), byte(p), 1, 2, 3}
		if got := Vendor(mac); got != oui.names[e&0xffffffff] {
			t.Fatalf("%s: %q, want %q", mac, got, oui.names[e&0xffffffff])
		}
	}
}
