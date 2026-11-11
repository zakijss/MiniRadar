package main

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Host is one device found on the network.
type Host struct {
	Addr     netip.Addr
	MAC      string
	Vendor   string
	Name     string
	NameFrom string // "mDNS", "NetBIOS" or "DNS"; "saved" for a name saved for Wake
	Ports    []int  // open, sorted
	Ping     bool   // answered ping
	Self     bool   // this Miyoo
	Router   bool   // the default gateway
}

// Scan phases.
const (
	phaseFind = iota
	phasePorts
	phaseNames
	phaseDone
)

// Progress is how far a scan has got.
type Progress struct {
	Phase       int
	Done, Total int // steps of the current phase
	Hosts       int
	Started     time.Time
	Took        time.Duration
	PingWorked  bool
	Interrupted bool
	SkipPorts   bool // the common ports aren't checked (Settings)
}

// Fraction is the whole scan's progress, 0..1 (finding is the slow part).
func (p Progress) Fraction() float64 {
	part := 0.0
	if p.Total > 0 {
		part = float64(p.Done) / float64(p.Total)
	}
	switch {
	case p.Phase == phaseFind && p.SkipPorts:
		return 0.9 * part
	case p.Phase == phaseFind:
		return 0.5 * part
	case p.Phase == phasePorts:
		return 0.5 + 0.4*part
	case p.Phase == phaseNames:
		return 0.9 + 0.1*part
	}
	return 1
}

// Probe settings: short timeouts and a cap on sockets in flight, so a scan
// of a /24 takes seconds and never more than ~130 file descriptors.
const (
	findWorkers = 128
	findTimeout = 800 * time.Millisecond
	findPort    = 80 // any port does: a refusal proves the device is there
	arpSettle   = 500 * time.Millisecond
	portWorkers = 64
	portTimeout = 700 * time.Millisecond
	nameWorkers = 32
	nameTimeout = 1500 * time.Millisecond
)

// Scan discovers the devices on sub. Start runs it in the background;
// changed is called (from another goroutine) when a device is found or
// updated, and when a phase starts or the scan ends.
type Scan struct {
	sub     Subnet
	changed func()
	// SkipPorts leaves out the common ports: a scan then only finds the
	// devices and their names. Set it before Start.
	SkipPorts bool

	mu    sync.Mutex
	hosts map[netip.Addr]*Host
	prog  Progress
	done  atomic.Int64
	gw    netip.Addr
}

// NewScan prepares a scan of sub; changed is called as described on Scan.
func NewScan(sub Subnet, changed func()) *Scan {
	return &Scan{sub: sub, changed: changed, hosts: map[netip.Addr]*Host{}}
}

// Start runs the scan in the background until it ends or ctx is cancelled.
func (s *Scan) Start(ctx context.Context) {
	s.prog.Started = time.Now()
	s.prog.SkipPorts = s.SkipPorts
	go s.run(ctx)
}

// Hosts returns copies of the devices found so far, by address.
func (s *Scan) Hosts() []Host {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Host, 0, len(s.hosts))
	for _, h := range s.hosts {
		c := *h
		c.Ports = slices.Clone(h.Ports)
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Host) int { return a.Addr.Compare(b.Addr) })
	return out
}

// Host returns the device at a.
func (s *Scan) Host(a netip.Addr) (Host, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hosts[a]
	if !ok {
		return Host{}, false
	}
	c := *h
	c.Ports = slices.Clone(h.Ports)
	return c, true
}

// Progress reports how far the scan has got.
func (s *Scan) Progress() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.prog
	p.Done = int(s.done.Load())
	p.Hosts = len(s.hosts)
	return p
}

// Running reports whether the scan is still going.
func (s *Scan) Running() bool { return s.Progress().Phase != phaseDone }

func (s *Scan) setPhase(ph, total int) {
	s.mu.Lock()
	s.prog.Phase, s.prog.Total = ph, total
	s.done.Store(0)
	s.mu.Unlock()
	s.changed()
}

// update applies fn to the device at a, adding it if new.
func (s *Scan) update(a netip.Addr, fn func(h *Host)) {
	if !s.sub.Scan.Contains(a) {
		return
	}
	s.mu.Lock()
	h, ok := s.hosts[a]
	if !ok {
		h = &Host{Addr: a, Self: a == s.sub.Self, Router: a == s.gw}
		s.hosts[a] = h
	}
	fn(h)
	if h.Vendor == "" && h.MAC != "" {
		h.Vendor = vendorOf(h.MAC)
	}
	s.mu.Unlock()
	s.changed()
}

func (s *Scan) addrs() []netip.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]netip.Addr, 0, len(s.hosts))
	for a := range s.hosts {
		out = append(out, a)
	}
	slices.SortFunc(out, netip.Addr.Compare)
	return out
}

// mergeARP adds every device the kernel resolved while probing: on-link
// devices must answer ARP even when they ignore every probe (phones do).
func (s *Scan) mergeARP() {
	for a, mac := range readARP(s.sub.Iface) {
		s.update(a, func(h *Host) { h.MAC = mac })
	}
}

func (s *Scan) run(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.prog.Phase = phaseDone
		s.prog.Took = time.Since(s.prog.Started)
		s.prog.Interrupted = ctx.Err() != nil
		s.mu.Unlock()
		s.changed()
	}()
	s.gw = gateway(s.sub.Iface)
	addrs := s.sub.Hosts()
	s.update(s.sub.Self, func(h *Host) {
		if len(s.sub.MAC) == 6 {
			h.MAC = FormatMAC(s.sub.MAC)
		}
	})

	// 1. Find devices: one TCP connect each (it also makes the kernel ARP
	// for the address), and a ping each if raw sockets are allowed.
	s.setPhase(phaseFind, len(addrs))
	pg := startPing(addrs, func(a netip.Addr) { s.update(a, func(h *Host) { h.Ping = true }) })
	parallel(ctx, len(addrs), findWorkers, func(i int) {
		if up, open := probe(ctx, addrs[i], findPort, findTimeout); up {
			s.update(addrs[i], func(h *Host) {
				if open && !s.SkipPorts && !slices.Contains(h.Ports, findPort) {
					h.Ports = append(h.Ports, findPort)
				}
			})
		}
		s.done.Add(1)
	})
	sleep(ctx, arpSettle) // late ARP and ping replies
	if pg != nil {
		s.mu.Lock()
		s.prog.PingWorked = pg.stop()
		s.mu.Unlock()
	}
	s.mergeARP()
	if ctx.Err() != nil {
		return
	}

	// 2. Check the common ports on each device found (unless skipped).
	if !s.SkipPorts {
		found := s.addrs()
		s.setPhase(phasePorts, len(found)*len(services))
		parallel(ctx, len(found)*len(services), portWorkers, func(i int) {
			a, p := found[i/len(services)], services[i%len(services)].Port
			if _, open := probe(ctx, a, p, portTimeout); open {
				s.update(a, func(h *Host) {
					if !slices.Contains(h.Ports, p) {
						h.Ports = append(h.Ports, p)
						slices.Sort(h.Ports)
					}
				})
			}
			s.done.Add(1)
		})
		s.mergeARP()
		if ctx.Err() != nil {
			return
		}
	}

	// 3. Names: mDNS, NetBIOS and reverse DNS, all at once per device.
	found := s.addrs()
	s.setPhase(phaseNames, len(found))
	parallel(ctx, len(found), nameWorkers, func(i int) {
		if name, from := lookupName(ctx, found[i]); name != "" {
			s.update(found[i], func(h *Host) { h.Name, h.NameFrom = name, from })
		}
		s.done.Add(1)
	})
}

// probe connects to a:port. up is true when the device answered at all
// (accepted or refused); open when it accepted.
func probe(ctx context.Context, a netip.Addr, port int, timeout time.Duration) (up, open bool) {
	d := net.Dialer{Timeout: timeout}
	c, err := d.DialContext(ctx, "tcp4", netip.AddrPortFrom(a, uint16(port)).String())
	if err == nil {
		c.Close()
		return true, true
	}
	return errors.Is(err, syscall.ECONNREFUSED), false
}

// parallel runs fn(0..n-1) on up to workers goroutines, stopping early
// when ctx ends.
func parallel(ctx context.Context, n, workers int, fn func(i int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < min(n, workers); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// ---- ping (optional: needs a raw socket, which root has) ----

type pinger struct {
	c       net.PacketConn
	id      int
	done    chan struct{}
	replied atomic.Bool
}

// startPing sends one ICMP echo to each address and reports replies until
// stop. It returns nil when raw sockets aren't allowed; the scan then
// relies on TCP and ARP alone.
func startPing(addrs []netip.Addr, alive func(netip.Addr)) *pinger {
	c, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil
	}
	p := &pinger{c: c, id: os.Getpid() & 0xffff, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		buf := make([]byte, 1500)
		for {
			n, from, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			if !isEchoReply(buf[:n], p.id) {
				continue
			}
			if ip, ok := from.(*net.IPAddr); ok {
				if a, ok := netip.AddrFromSlice(ip.IP.To4()); ok {
					p.replied.Store(true)
					alive(a)
				}
			}
		}
	}()
	go func() {
		for i, a := range addrs {
			if _, err := c.WriteTo(echoRequest(p.id, i), &net.IPAddr{IP: a.AsSlice()}); errors.Is(err, net.ErrClosed) {
				return
			}
			if i%32 == 31 {
				time.Sleep(5 * time.Millisecond) // don't flood the Wi-Fi queue
			}
		}
	}()
	return p
}

// isEchoReply reports whether m, a packet read from the ICMP socket, is an
// echo reply to id. An IP header left on is skipped; its length is
// checked, since an ICMP message whose type byte happens to start with 4
// (types 64-79) looks the same and would otherwise slice past the end.
func isEchoReply(m []byte, id int) bool {
	if len(m) >= 20 && m[0]>>4 == 4 {
		if ihl := int(m[0]&0x0f) * 4; ihl >= 20 && ihl <= len(m) {
			m = m[ihl:]
		}
	}
	return len(m) >= 8 && m[0] == 0 && int(binary.BigEndian.Uint16(m[4:])) == id
}

// stop closes the socket and reports whether anything answered.
func (p *pinger) stop() bool {
	p.c.Close()
	<-p.done
	return p.replied.Load()
}

func echoRequest(id, seq int) []byte {
	b := []byte{8, 0, 0, 0, byte(id >> 8), byte(id), byte(seq >> 8), byte(seq), 'm', 'i', 'y', 'o', 'o'}
	cs := checksum(b)
	b[2], b[3] = byte(cs>>8), byte(cs)
	return b
}

func checksum(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = s&0xffff + s>>16
	}
	return ^uint16(s)
}
