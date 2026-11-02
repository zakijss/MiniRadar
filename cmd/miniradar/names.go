package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// lookupName asks the device for its name over mDNS and NetBIOS, and the
// router's DNS for its reverse name, all at once. The device's own answer
// wins.
func lookupName(ctx context.Context, a netip.Addr) (name, from string) {
	ctx, cancel := context.WithTimeout(ctx, nameTimeout)
	defer cancel()
	type res struct {
		name, from string
		rank       int
	}
	ch := make(chan res, 3)
	go func() { ch <- res{mdnsName(ctx, a), "mDNS", 0} }()
	go func() { ch <- res{netbiosName(ctx, a), "NetBIOS", 1} }()
	go func() { ch <- res{dnsName(ctx, a), "DNS", 2} }()
	best := res{rank: 99}
	for i := 0; i < 3; i++ {
		if r := <-ch; r.name != "" && r.rank < best.rank {
			best = r
		}
	}
	return best.name, best.from
}

// cleanName drops the trailing dot and the home-network suffixes routers
// and mDNS add, so "MacBook-Air.local." reads "MacBook-Air".
func cleanName(s string) string {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	for _, suf := range []string{".local", ".lan", ".home.arpa", ".home", ".localdomain", ".internal", ".fritz.box"} {
		if len(s) > len(suf) && strings.EqualFold(s[len(s)-len(suf):], suf) {
			return s[:len(s)-len(suf)]
		}
	}
	return s
}

func dnsName(ctx context.Context, a netip.Addr) string {
	names, err := net.DefaultResolver.LookupAddr(ctx, a.String())
	if err != nil || len(names) == 0 {
		return ""
	}
	return cleanName(names[0])
}

func reverseName(a netip.Addr) string {
	b := a.As4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", b[3], b[2], b[1], b[0])
}

// mdnsName sends a unicast mDNS query for a's reverse name straight to the
// device (RFC 6762 6.7): Apple devices, Linux with Avahi, printers and
// most smart speakers answer it.
func mdnsName(ctx context.Context, a netip.Addr) string {
	name, err := dnsmessage.NewName(reverseName(a))
	if err != nil {
		return ""
	}
	q := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: uint16(rand.Uint32())},
		Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET}},
	}
	pkt, err := q.Pack()
	if err != nil {
		return ""
	}
	return parseMDNS(exchange(ctx, netip.AddrPortFrom(a, 5353), pkt))
}

func parseMDNS(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var m dnsmessage.Message
	if m.Unpack(b) != nil {
		return ""
	}
	for _, rr := range m.Answers {
		if ptr, ok := rr.Body.(*dnsmessage.PTRResource); ok {
			return cleanName(ptr.PTR.String())
		}
	}
	return ""
}

// netbiosName asks for the device's NetBIOS name table (Windows, Samba).
func netbiosName(ctx context.Context, a netip.Addr) string {
	return parseNBSTAT(exchange(ctx, netip.AddrPortFrom(a, 137), nbstatQuery(uint16(rand.Uint32()))))
}

// nbstatQuery is a NetBIOS node status request for the wildcard name "*".
func nbstatQuery(id uint16) []byte {
	p := []byte{byte(id >> 8), byte(id), 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0x20}
	var name [16]byte
	name[0] = '*'
	for _, b := range name { // "first-level encoding": each nibble as 'A'+n
		p = append(p, 'A'+b>>4, 'A'+b&0x0f)
	}
	return append(p, 0, 0, 0x21, 0, 1) // end of name, type NBSTAT, class IN
}

// parseNBSTAT returns the first unique workstation name (suffix 0x00) in a
// node status response.
func parseNBSTAT(b []byte) string {
	if len(b) < 12 || b[6] == 0 && b[7] == 0 { // no answers
		return ""
	}
	i := 12
	for i < len(b) { // skip the name
		l := int(b[i])
		if l == 0 {
			i++
			break
		}
		if l&0xc0 == 0xc0 {
			i += 2
			break
		}
		i += 1 + l
	}
	i += 10 // type, class, TTL, length
	if i >= len(b) {
		return ""
	}
	n := int(b[i])
	i++
	for k := 0; k < n && i+18 <= len(b); k, i = k+1, i+18 {
		name := strings.TrimRight(string(b[i:i+15]), " \x00")
		suffix, group := b[i+15], b[i+16]&0x80 != 0
		if suffix == 0 && !group && name != "" {
			return name
		}
	}
	return ""
}

// exchange sends pkt over UDP and returns the first reply, sending again
// once if the first try gets nothing (Wi-Fi drops the odd packet).
func exchange(ctx context.Context, to netip.AddrPort, pkt []byte) []byte {
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp4", to.String())
	if err != nil {
		return nil
	}
	defer c.Close()
	end, ok := ctx.Deadline()
	if !ok {
		end = time.Now().Add(nameTimeout)
	}
	buf := make([]byte, 1500)
	for try := 0; try < 2; try++ {
		if _, err := c.Write(pkt); err != nil {
			return nil // e.g. refused: nothing listening
		}
		wait := end
		if try == 0 {
			wait = time.Now().Add(time.Until(end) / 2)
		}
		c.SetReadDeadline(wait)
		if n, err := c.Read(buf); err == nil {
			return buf[:n]
		} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
			return nil
		}
	}
	return nil
}
