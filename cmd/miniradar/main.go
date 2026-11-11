// MiniRadar looks at the Miyoo's own Wi-Fi network: which devices are on it
// (address, maker, name, open ports), and wakes saved computers with
// Wake-on-LAN. It only ever scans the network wlan0 is on, at most a /24,
// and only with ordinary connections: no Wi-Fi tricks, nothing intrusive.
package main

import (
	"context"
	"fmt"
	"image"
	"log"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/zakijss/minideck/appdir"
	"github.com/zakijss/minideck/ui"
	"github.com/zakijss/minideck/version"
)

type app struct {
	scr     *ui.Screen
	input   <-chan ui.Event
	dir     string
	cfg     Settings
	sub     Subnet
	subErr  error
	scan    *Scan
	cancel  context.CancelFunc
	changed chan struct{} // the scan found or updated something
	wake    bool          // which tab: Devices or Wake
	hostL   ui.List
	wakeL   ui.List
	selIP   netip.Addr // keeps the same device selected as the list grows
	devices []Device
	note    string // a one-off message in the subtitle, until the next press

	known    map[string]bool // MACs found before this session, upper case
	seen     []string        // seen.json
	recorded *Scan           // the last scan added to seen.json
}

func main() {
	version.Flag("miniradar")
	version.Note("miniradar")
	scr, err := ui.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer scr.Close()
	a := &app{scr: scr, input: ui.Input(), dir: appdir.Dir(), changed: make(chan struct{}, 1)}
	a.cfg = loadSettings(a.dir)
	a.seen = loadSeen(a.dir)
	a.known = make(map[string]bool, len(a.seen))
	for _, m := range a.seen {
		a.known[m] = true
	}
	defer func() {
		if a.cancel != nil {
			a.cancel()
		}
	}()
	a.devices, err = loadDevices(a.dir)
	if err != nil {
		scr.Notice(a.input, "Saved devices unreadable", err.Error()+"\n\nFix devices.json in the app's folder first: saving a device now replaces it.")
	}
	a.startScan()
	a.run()
}

// message is an error whose text is written to be shown on screen as it is.
type message string

func (m message) Error() string { return string(m) }

// tryAgain ends a notice whose error is raw, so it says what to do next.
const tryAgain = "\n\nTry again."

func (a *app) poke() {
	select {
	case a.changed <- struct{}{}:
	default:
	}
}

func (a *app) scanning() bool { return a.scan != nil && a.scan.Running() }

// startScan reads the Wi-Fi network afresh and scans it.
func (a *app) startScan() {
	if a.scanning() {
		return
	}
	a.sub, a.subErr = localSubnet()
	if a.subErr != nil {
		a.scan = nil
		return
	}
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.scan = NewScan(a.sub, a.poke)
	a.scan.SkipPorts = !a.cfg.ports()
	a.scan.Start(ctx)
}

var listRect = image.Rect(20, 78, ui.W-26, ui.H-58)

func (a *app) run() {
	for {
		a.recordSeen()
		if a.wake {
			a.drawWake()
		} else {
			a.drawHosts()
		}
		// While a scan runs, redraw five times a second (the progress bar
		// and new devices) rather than on every find; when idle, twice a
		// minute, for the "scanned 3 minutes ago" stamp.
		changed := a.changed
		var tick <-chan time.Time
		if a.wake {
			changed = nil
		} else if a.scanning() {
			changed, tick = nil, time.After(200*time.Millisecond)
		} else {
			tick = time.After(30 * time.Second)
		}
		select {
		case ev, ok := <-a.input:
			if !ok {
				return
			}
			if !ev.Pressed {
				continue
			}
			a.note = ""
			switch ev.Button {
			case ui.B, ui.Menu:
				return
			case ui.L1, ui.R1:
				a.wake = !a.wake
			case ui.Select:
				a.scr.Appearance(a.input)
			case ui.Start:
				a.settings()
			default:
				if a.wake {
					a.wakeKey(ev)
				} else {
					a.hostKey(ev)
				}
			}
		case <-changed:
		case <-tick:
		}
	}
}

// ---- Devices tab ----

const progressH = 58

// hostHints leave out A (Open): the rows' chevrons say A opens them, and
// the bar has no room for seven buttons.
var hostHints = []ui.Hint{{Button: "Y", Label: "Save"}, {Button: "X", Label: "Scan"},
	{Button: "R1", Label: "Wake"}, settingsHint, {Button: "B", Label: "Exit"}}

// settingsHint is the footer hint for Settings, on both tabs. Settings
// holds Appearance too; SELECT still opens Appearance directly.
var settingsHint = ui.Hint{Button: "START", Label: "Settings"}

// scanLabel names what is scanned: "192.168.1.0/24", noting the cap on a
// wider network.
func (a *app) scanLabel() string {
	if a.sub.Scan == a.sub.Net {
		return a.sub.Scan.String()
	}
	return a.sub.Scan.String() + " (of " + a.sub.Net.String() + ")"
}

// displayName is what a device is called in the list: its own name if it
// gave one, else its role or maker, else its address.
func displayName(h Host) string {
	switch {
	case h.Self:
		return "This Miyoo"
	case h.Name != "":
		return h.Name
	case h.Router:
		return "Router"
	case h.Vendor != "" && h.Vendor != "Private MAC":
		return h.Vendor + " device"
	}
	return h.Addr.String()
}

// hostItem is one row of the device list; isNew marks a device no earlier
// scan found.
func hostItem(h Host, isNew bool) ui.Item {
	label := displayName(h)
	parts := []string{h.Addr.String()}
	if label == h.Addr.String() {
		parts = nil
	}
	if h.Vendor != "" && !strings.HasPrefix(label, h.Vendor) {
		parts = append(parts, h.Vendor)
	}
	if h.Router && label != "Router" {
		parts = append(parts, "router")
	}
	if len(parts) == 0 {
		parts = append(parts, h.MAC)
	}
	if isNew {
		parts = append([]string{"New"}, parts...)
	}
	icon := "server"
	switch {
	case h.Router:
		icon = "wifi"
	case h.Self:
		icon = "star"
	}
	return ui.Item{Label: label, Sub: strings.Join(parts, " · "), Detail: formatPorts(h.Ports, 2), Icon: icon, Chevron: true}
}

func (a *app) drawHosts() {
	s := a.scr
	if a.subErr != nil {
		top := s.StaticFrame("Devices", "Not connected",
			ui.Hint{Button: "X", Label: "Try again"}, ui.Hint{Button: "R1", Label: "Wake"}, settingsHint, ui.Hint{Button: "B", Label: "Exit"})
		a.card(top, "No network to scan", a.subErr.Error())
		s.Present()
		return
	}
	var hosts []Host
	var p Progress
	if a.scan != nil {
		hosts, p = a.hosts(), a.scan.Progress()
	}
	sub := fmt.Sprintf("%s on %s · %s", plural(len(hosts), "device"), a.scanLabel(), ago(time.Since(p.Started.Add(p.Took))))
	if p.Phase != phaseDone {
		sub = "Scanning your network " + a.scanLabel()
	}
	if a.note != "" {
		sub = a.note
	}
	top := s.StaticFrame("Devices", sub, hostHints...)
	r := image.Rect(20, top, ui.W-26, ui.H-58)
	if p.Phase != phaseDone {
		a.drawProgress(top, p)
		r.Min.Y += progressH
	}
	items := make([]ui.Item, len(hosts))
	for i, h := range hosts {
		items[i] = hostItem(h, a.isNew(h))
		if h.Addr == a.selIP {
			a.hostL.Sel = i
		}
	}
	a.hostL.Items = items
	if a.hostL.Sel >= len(items) {
		a.hostL.Sel = max(0, len(items)-1)
	}
	if len(hosts) > 0 {
		a.selIP = hosts[a.hostL.Sel].Addr
	}
	a.hostL.Draw(s, r)
	s.Present()
}

// ago says how long ago a scan ended, to the minute, short enough to fit
// the subtitle beside the network.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%d h ago", int(d.Hours()))
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func (a *app) drawProgress(top int, p Progress) {
	s := a.scr
	r := image.Rect(20, top, ui.W-26, top+progressH-10)
	s.Panel(r, 16)
	var txt string
	switch p.Phase {
	case phaseFind:
		txt = fmt.Sprintf("Finding devices · %d of %d addresses", p.Done, p.Total)
	case phasePorts:
		txt = "Checking common ports on " + plural(p.Hosts, "device")
	default:
		txt = "Looking up names"
	}
	s.Text(r.Min.X+16, r.Min.Y+6, ui.Fit(ui.Small, txt, r.Dx()-32), ui.Small, ui.Text)
	s.Bar(image.Rect(r.Min.X+16, r.Max.Y-13, r.Max.X-16, r.Max.Y-8), p.Fraction(), ui.Text)
}

// card draws a panel with a heading and wrapped text.
func (a *app) card(top int, title, body string) {
	s := a.scr
	lines := ui.Wrap(ui.Body, body, ui.W-90)
	r := image.Rect(20, top, ui.W-20, top+36+ui.LineHeight(ui.Heading)+len(lines)*ui.LineHeight(ui.Body))
	s.Panel(r, 18)
	y := r.Min.Y + 16
	s.Text(r.Min.X+22, y, title, ui.Heading, ui.Text)
	y += ui.LineHeight(ui.Heading) + 6
	for _, l := range lines {
		s.Text(r.Min.X+22, y, l, ui.Body, ui.Muted)
		y += ui.LineHeight(ui.Body)
	}
}

func (a *app) selectedHost() (Host, bool) {
	if a.scan == nil || !a.selIP.IsValid() {
		return Host{}, false
	}
	return a.host(a.selIP)
}

// host is the scanned device at addr, under the name it's saved as for
// Wake if it is.
func (a *app) host(addr netip.Addr) (Host, bool) {
	h, ok := a.scan.Host(addr)
	return a.savedName(h), ok
}

// savedName gives h the name its MAC is saved under for Wake, if any: the
// name you chose beats what the device calls itself.
func (a *app) savedName(h Host) Host {
	if h.MAC == "" {
		return h
	}
	for _, d := range a.devices {
		if d.Name != "" && strings.EqualFold(d.MAC, h.MAC) {
			h.Name, h.NameFrom = d.Name, "saved"
			break
		}
	}
	return h
}

// jump moves l to its first (L2) or last (R2) row; it reports whether ev
// was one of those.
func jump(l *ui.List, ev ui.Event) bool {
	switch ev.Button {
	case ui.L2:
		l.Sel = 0
	case ui.R2:
		l.Sel = max(0, len(l.Items)-1)
	default:
		return false
	}
	return true
}

func (a *app) hostKey(ev ui.Event) {
	switch ev.Button {
	case ui.X:
		a.startScan()
	case ui.A:
		if h, ok := a.selectedHost(); ok {
			a.details(h.Addr)
		}
	case ui.Y:
		if h, ok := a.selectedHost(); ok {
			a.saveHost(h)
		}
	default:
		if jump(&a.hostL, ev) || a.hostL.Handle(ev) {
			if it := a.hostL.Selected(); it != nil && a.scan != nil {
				if hs := a.hosts(); a.hostL.Sel < len(hs) {
					a.selIP = hs[a.hostL.Sel].Addr
				}
			}
		}
	}
}

// details shows everything known about one device, updating while the
// scan fills in its ports and name.
func (a *app) details(addr netip.Addr) {
	s := a.scr
	var l ui.List
	for {
		h, _ := a.host(addr)
		p := a.scan.Progress()
		hints := []ui.Hint{{Button: "Y", Label: "Save for Wake"}, {Button: "B", Label: "Back"}}
		if h.MAC == "" || h.Self {
			hints = hints[1:]
		}
		sub := h.Addr.String()
		if h.Vendor != "" {
			sub += " · " + h.Vendor
		}
		top := s.StaticFrame(displayName(h), sub, hints...)
		l.Items = detailItems(h, p)
		if l.Sel >= len(l.Items) {
			l.Sel = len(l.Items) - 1
		}
		l.Draw(s, image.Rect(20, top, ui.W-26, ui.H-58))
		s.Present()

		changed := a.changed
		var tick <-chan time.Time
		if p.Phase != phaseDone {
			changed, tick = nil, time.After(300*time.Millisecond)
		}
		select {
		case ev, ok := <-a.input:
			if !ok || !ev.Pressed {
				if !ok {
					return
				}
				continue
			}
			switch ev.Button {
			case ui.B, ui.Menu:
				return
			case ui.Y:
				if h.MAC != "" && !h.Self {
					a.saveHost(h)
				}
			default:
				if !jump(&l, ev) {
					l.Handle(ev)
				}
			}
		case <-changed:
		case <-tick:
		}
	}
}

func detailItems(h Host, p Progress) []ui.Item {
	val := func(label, v string) ui.Item {
		return ui.Item{Label: label, Detail: ui.Fit(ui.Small, v, 360)}
	}
	unknown := func(v string) string {
		if v == "" {
			return "Not known"
		}
		return v
	}
	items := []ui.Item{val("IP address", h.Addr.String()), val("MAC address", unknown(h.MAC)), val("Maker", unknown(h.Vendor))}
	if h.Name != "" {
		items = append(items, val("Name", h.Name+" ("+h.NameFrom+")"))
	}
	switch {
	case h.Self:
		items = append(items, val("Role", "This Miyoo"))
	case h.Router:
		items = append(items, val("Role", "Your router"))
	}
	if h.Ping {
		items = append(items, val("Ping", "Answers"))
	}
	for _, port := range h.Ports {
		sv := service(port)
		items = append(items, ui.Item{Label: sv.Name, Detail: fmt.Sprintf("Port %d open", port)})
	}
	switch {
	case p.SkipPorts:
		items = append(items, ui.Item{Label: "Ports not checked", Sub: "Settings › Common ports is off"})
	case p.Phase <= phasePorts:
		items = append(items, ui.Item{Label: "Checking common ports…"})
	case len(h.Ports) == 0:
		items = append(items, ui.Item{Label: "No common ports open"})
	}
	return items
}

// saveHost saves a scanned device for Wake (or edits it if its MAC is
// already saved).
func (a *app) saveHost(h Host) {
	switch {
	case h.Self:
		a.scr.Notice(a.input, "This is the Miyoo", "It can't wake itself.")
		return
	case h.MAC == "":
		a.scr.Notice(a.input, "MAC address not known", "The scan didn't see this device's MAC address, so it can't be saved for Wake. Scan again with X.")
		return
	}
	i := slices.IndexFunc(a.devices, func(d Device) bool { return strings.EqualFold(d.MAC, h.MAC) })
	d := Device{MAC: h.MAC, IP: h.Addr.String()}
	if i >= 0 {
		d = a.devices[i]
		d.IP = h.Addr.String()
	} else if n := displayName(h); n != h.Addr.String() && !h.Router {
		d.Name = n
	}
	if a.editDevice(i, d) {
		a.note = "Saved for Wake: press R1 to wake it"
	}
}

// ---- Wake tab ----

func (a *app) drawWake() {
	s := a.scr
	sub := "Wakes a computer with a magic packet"
	if a.note != "" {
		sub = a.note
	}
	top := s.StaticFrame("Wake", sub,
		ui.Hint{Button: "A", Label: "Wake"}, ui.Hint{Button: "Y", Label: "Edit"}, ui.Hint{Button: "L1", Label: "Devices"}, settingsHint, ui.Hint{Button: "B", Label: "Exit"})
	// which saved devices the last scan found awake
	awake := map[string]bool{}
	if a.scan != nil {
		for _, h := range a.scan.Hosts() {
			awake[strings.ToUpper(h.MAC)] = h.MAC != ""
		}
	}
	items := make([]ui.Item, 0, len(a.devices)+1)
	for _, d := range a.devices {
		sub := d.MAC
		if v := vendorOf(d.MAC); v != "" {
			sub += " · " + v
		}
		it := ui.Item{Label: d.Name, Sub: sub, Icon: "server"}
		if awake[strings.ToUpper(d.MAC)] {
			it.Detail = "Awake"
		}
		items = append(items, it)
	}
	items = append(items, ui.Item{Label: "Add a device", Sub: "By its MAC address, or Y on a scanned one", Icon: "plus", Chevron: true})
	a.wakeL.Items = items
	if a.wakeL.Sel >= len(items) {
		a.wakeL.Sel = len(items) - 1
	}
	a.wakeL.Draw(s, listRect.Add(image.Pt(0, top-listRect.Min.Y)))
	s.Present()
}

func (a *app) wakeKey(ev ui.Event) {
	i := a.wakeL.Sel
	switch ev.Button {
	case ui.A:
		if i >= len(a.devices) {
			a.editDevice(-1, Device{})
			return
		}
		a.wakeDevice(a.devices[i])
	case ui.Y:
		if i >= len(a.devices) {
			a.editDevice(-1, Device{})
		} else {
			a.editDevice(i, a.devices[i])
		}
	default:
		if !jump(&a.wakeL, ev) {
			a.wakeL.Handle(ev)
		}
	}
}

func (a *app) wakeDevice(d Device) {
	sub, err := localSubnet() // afresh: Wi-Fi may have reconnected
	if err != nil {
		a.scr.Notice(a.input, "Not connected", err.Error())
		return
	}
	mac, err := ParseMAC(d.MAC)
	if err == nil {
		err = Wake(sub, mac)
	}
	if err != nil {
		a.scr.Notice(a.input, "Couldn't send", err.Error()+tryAgain)
		return
	}
	a.note = "Sent " + d.Name + " a wake-up (to " + sub.Broadcast.String() + ")"
}

// editDevice adds (i < 0) or edits a saved device; it reports whether it
// was saved (not cancelled or removed).
func (a *app) editDevice(i int, d Device) bool {
	name := &ui.Field{Label: "Name", Value: d.Name, Placeholder: "e.g. Gaming PC"}
	mac := &ui.Field{Label: "MAC address", Value: d.MAC, Placeholder: "3C:7C:3F:12:AB:9E",
		Hint: "Six pairs of hex digits. Colons are optional."}
	title := "Add a device"
	if i >= 0 {
		title = "Edit device"
	}
	f := &ui.Form{Title: title, Fields: []*ui.Field{name, mac},
		Intro: "Wake-on-LAN must be on in the computer's BIOS and network settings."}
	f.Commit = func() error {
		m, err := ParseMAC(mac.Value)
		if err != nil {
			return err
		}
		nd := Device{Name: strings.TrimSpace(name.Value), MAC: FormatMAC(m), IP: d.IP}
		for j, o := range a.devices {
			if j != i && strings.EqualFold(o.MAC, nd.MAC) {
				return message("That MAC address is already saved as " + o.Name + ".")
			}
		}
		list := slices.Clone(a.devices)
		if i >= 0 {
			list[i] = nd
		} else {
			list = append(list, nd)
		}
		if err := saveDevices(a.dir, list); err != nil {
			return err
		}
		a.devices = list
		return nil
	}
	removed := false
	if i >= 0 {
		f.Delete = func() {
			list := slices.Delete(slices.Clone(a.devices), i, i+1)
			if err := saveDevices(a.dir, list); err != nil {
				a.scr.Notice(a.input, "Couldn't remove it", err.Error()+tryAgain)
				return
			}
			a.devices, removed = list, true
		}
	}
	return f.Run(a.scr, a.input) && !removed
}
