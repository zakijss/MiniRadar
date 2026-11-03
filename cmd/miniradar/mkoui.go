//go:build ignore

// mkoui writes oui.txt, the vendor table MiniRadar embeds: every 24-bit MAC
// prefix (OUI) of about 180 vendors common on home networks, under a short
// display name. The rest of the IEEE registry (some 39,000 prefixes, mostly
// telecom and industrial makers) is left out to keep the app small.
//
// Input is any copy of the IEEE MA-L registry in "HEX6 Organisation" lines,
// such as Nmap's nmap-mac-prefixes:
//
//	go run mkoui.go /opt/homebrew/share/nmap/nmap-mac-prefixes > oui.txt
package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// vendors maps a display name to a pattern for the registry's organisation
// names (case-insensitive, matched at the start). The first match wins, so
// more specific entries come first.
var vendors = []struct{ name, re string }{
	// phones, tablets, computers
	{"Apple", `Apple$|Apple,`},
	{"Samsung", `Samsung Electr`},
	{"Google", `Google`},
	{"Nest", `Nest Labs`},
	{"Microsoft", `Microsoft`},
	{"Intel", `Intel( Corporate)?$`},
	{"Dell", `Dell`},
	{"HPE", `Hewlett Packard Enterprise`},
	{"HP", `Hewlett Packard$|Hewlett-Packard|HP$|HP Inc`},
	{"Lenovo", `Lenovo`},
	{"Motorola", `Motorola Mobility|Motorola \(Wuhan`},
	{"Huawei", `Huawei`},
	{"Honor", `Honor Device`},
	{"ZTE", `zte`},
	{"Xiaomi", `Xiaomi|Beijing Xiaomi`},
	{"Oppo", `Guangdong Oppo`},
	{"Vivo", `vivo Mobile`},
	{"OnePlus", `OnePlus`},
	{"Realme", `Realme`},
	{"Nothing", `Nothing Technology`},
	{"Tecno", `Tecno Mobile|Shenzhen Tecno`},
	{"Infinix", `Infinix`},
	{"TCL", `TCT mobile|TCL`},
	{"LG", `LG Electronics|LG Innotek`},
	{"PlayStation", `Sony Interactive`},
	{"Sony", `Sony`},
	{"Nintendo", `Nintendo`},
	{"HTC", `HTC`},
	{"BlackBerry", `BlackBerry|Research In Motion`},
	{"Fairphone", `FairPhone`},
	{"Acer", `Acer`},
	{"Toshiba", `Toshiba`},
	{"Sharp", `Sharp`},
	{"Panasonic", `Panasonic`},
	{"Razer", `Razer`},
	{"Framework", `Framework Computer`},
	{"Valve", `Valve`},
	{"Meta", `Meta Platforms|Oculus|Facebook`},
	{"Garmin", `Garmin`},
	{"Fitbit", `Fitbit`},
	{"Amazfit", `Anhui Huami`},
	{"Polar", `Polar Electro`},
	{"GoPro", `GoPro`},
	{"DJI", `SZ DJI`},
	{"Peloton", `Peloton`},
	// PC parts, boards and module makers (the name on many laptops' Wi-Fi)
	{"Raspberry Pi", `Raspberry Pi`},
	{"Gigabyte", `Giga-byte`},
	{"MSI", `Micro-star`},
	{"ASRock", `ASRock`},
	{"Supermicro", `Super Micro`},
	{"ASUS", `ASUSTek`},
	{"Realtek", `Realtek`},
	{"Broadcom", `Broadcom`},
	{"Qualcomm", `Qualcomm`},
	{"MediaTek", `MediaTek`},
	{"Nvidia", `Nvidia`},
	{"Texas Instruments", `Texas Instruments`},
	{"Silicon Labs", `Silicon Laboratories`},
	{"Espressif", `Espressif`},
	{"AzureWave", `AzureWave`},
	{"Murata", `Murata`},
	{"Liteon", `Liteon`},
	{"AMPAK", `Ampak`},
	{"Foxconn", `Hon Hai|Cloud Network Technology Singapore|Chongqing Fugui`},
	{"USI", `Universal Global Scientific`},
	{"Compal", `Compal Information`},
	{"Pegatron", `Pegatron`},
	{"Quanta", `Quanta Computer`},
	{"Wistron", `Wistron`},
	{"Allwinner", `Allwinner`},
	{"Amlogic", `Amlogic`},
	// virtual machines
	{"VMware", `VMware`},
	{"VirtualBox", `Oracle VirtualBox`},
	{"QEMU/KVM", `QEMU virtual`},
	{"Proxmox", `Proxmox`},
	{"Parallels", `Parallels`},
	{"Xen", `Xensource`},
	// storage
	{"Synology", `Synology`},
	{"QNAP", `Qnap`},
	{"Western Digital", `Western Digital`},
	{"Seagate", `Seagate`},
	// routers, modems, mesh Wi-Fi
	{"Ubiquiti", `Ubiquiti`},
	{"TP-Link", `TP-Link`},
	{"Mercusys", `Mercusys`},
	{"Tenda", `Tenda|Shen Zhen Tenda`},
	{"Netgear", `Netgear`},
	{"D-Link", `D-Link`},
	{"Linksys", `Cisco-Linksys|The Linksys|Linksys`},
	{"Meraki", `Cisco Meraki`},
	{"Cisco", `Cisco Systems|Cisco Spvtg`},
	{"eero", `eero`},
	{"FRITZ!Box", `AVM`},
	{"MikroTik", `Routerboard`},
	{"Zyxel", `Zyxel`},
	{"MitraStar", `MitraStar`},
	{"Sagemcom", `Sagemcom`},
	{"Technicolor", `Technicolor|Vantiva`},
	{"Arris", `Arris|Commscope`},
	{"Arcadyan", `Arcadyan`},
	{"Sercomm", `Sercomm`},
	{"Askey", `Askey`},
	{"Gemtek", `Gemtek`},
	{"Humax", `Humax`},
	{"Sky", `SKY UK`},
	{"Freebox", `Freebox`},
	{"Calix", `Calix`},
	{"Adtran", `Adtran`},
	{"Nokia", `Nokia`},
	{"Plume", `Plume Design`},
	{"Starlink", `Space Exploration`},
	{"DrayTek", `DrayTek`},
	{"Teltonika", `UAB Teltonika`},
	{"Peplink", `Peplink`},
	{"GL.iNet", `GL Technologies`},
	{"Intelbras", `Intelbras`},
	{"Kaon", `Kaon`},
	{"Skyworth", `Shenzhen Skyworth|Skyworth`},
	{"Verizon", `Verizon`},
	{"Comcast", `Comcast`},
	{"AT&T", `AT&T`},
	{"Vodafone", `Vodafone`},
	{"Fortinet", `Fortinet`},
	{"SonicWall", `SonicWall`},
	{"Sophos", `Sophos`},
	{"WatchGuard", `WatchGuard Technologies`},
	// TV, audio, streaming
	{"Sonos", `Sonos`},
	{"Roku", `Roku`},
	{"Vizio", `Vizio`},
	{"Hisense", `Hisense|Qingdao Hisense`},
	{"Bose", `Bose`},
	{"Harman", `Harman`},
	{"Yamaha", `Yamaha`},
	{"Denon/Marantz", `D&M Holdings`},
	{"Pioneer", `Pioneer`},
	{"Onkyo", `Onkyo`},
	{"Logitech", `Logitech`},
	// smart home
	{"Amazon", `Amazon|Blink by Amazon`},
	{"Ring", `Ring$`},
	{"Philips Hue", `Signify|Philips Lighting`},
	{"Philips", `Philips`},
	{"Tuya", `Tuya`},
	{"Belkin", `Belkin`},
	{"Hikvision", `Hangzhou Hikvision`},
	{"EZVIZ", `Hangzhou Ezviz`},
	{"Dahua", `Zhejiang Dahua`},
	{"Reolink", `Reolink`},
	{"Arlo", `Arlo`},
	{"Wyze", `Wyze`},
	{"WiZ", `WiZ`},
	{"Nanoleaf", `Nanoleaf`},
	{"Govee", `Shenzhen Intellirocks`},
	{"Aqara", `Lumi United`},
	{"Yeelight", `Yeelink`},
	{"Meross", `Chengdu Meross`},
	{"tado", `tado`},
	{"Netatmo", `Netatmo`},
	{"Withings", `Withings`},
	{"ecobee", `ecobee`},
	{"Chamberlain", `The Chamberlain`},
	{"iRobot", `iRobot`},
	{"Roborock", `Beijing Roborock`},
	{"Dreame", `Dreame`},
	{"Dyson", `Dyson`},
	{"Tesla", `Tesla`},
	{"Bosch", `Robert Bosch|BSH Hausger`},
	{"Miele", `Miele`},
	{"Whirlpool", `Whirlpool`},
	{"Haier", `Qingdao Haier|Qing DAO Haier`},
	{"Midea", `GD Midea`},
	{"Mitsubishi", `Mitsubishi Electric`},
	// printers
	{"Brother", `Brother`},
	{"Canon", `Canon`},
	{"Epson", `Seiko Epson`},
	{"Xerox", `Xerox`},
	{"Ricoh", `Ricoh`},
	{"Lexmark", `Lexmark`},
	{"Kyocera", `Kyocera`},
	{"VTech", `VTech`},
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run mkoui.go nmap-mac-prefixes > oui.txt")
		os.Exit(2)
	}
	res := make([]*regexp.Regexp, len(vendors))
	for i, v := range vendors {
		res[i] = regexp.MustCompile(`(?i)^(` + v.re + `)`)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	got := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		hex, org, ok := strings.Cut(sc.Text(), " ")
		if !ok || len(hex) != 6 || strings.HasPrefix(hex, "#") {
			continue
		}
		for i, re := range res {
			if re.MatchString(org) {
				got[vendors[i].name] = append(got[vendors[i].name], strings.ToUpper(hex))
				break
			}
		}
	}
	fmt.Println("# MAC prefix (OUI) -> vendor, generated by mkoui.go from the IEEE MA-L registry.")
	fmt.Println("# Format: vendor<TAB>prefixes. Only vendors common on home networks.")
	n := 0
	for _, v := range vendors {
		p := got[v.name]
		if len(p) == 0 {
			fmt.Fprintln(os.Stderr, "no prefixes:", v.name)
			continue
		}
		sort.Strings(p)
		fmt.Printf("%s\t%s\n", v.name, strings.Join(p, " "))
		n += len(p)
	}
	fmt.Fprintf(os.Stderr, "%d vendors, %d prefixes\n", len(got), n)
}
