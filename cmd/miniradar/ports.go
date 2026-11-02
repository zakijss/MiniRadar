package main

import (
	"strconv"
	"strings"
)

// Service is a TCP port worth checking on a home network.
type Service struct {
	Port  int
	Short string // in the device list
	Name  string // on the device's page
}

// services are the ports each device found is checked for, in port order.
var services = []Service{
	{21, "FTP", "File transfer (FTP)"},
	{22, "SSH", "SSH"},
	{23, "Telnet", "Telnet"},
	{53, "DNS", "DNS"},
	{80, "HTTP", "Web (HTTP)"},
	{443, "HTTPS", "Secure web (HTTPS)"},
	{445, "SMB", "File sharing (SMB)"},
	{548, "AFP", "Apple file sharing (AFP)"},
	{631, "IPP", "Printing (IPP)"},
	{1883, "MQTT", "MQTT"},
	{2049, "NFS", "NFS"},
	{3000, ":3000", "Web app"},
	{3389, "RDP", "Remote Desktop"},
	{4533, "Navidrome", "Navidrome"},
	{5000, ":5000", "Web app or AirPlay"},
	{5900, "VNC", "Screen sharing (VNC)"},
	{7000, "AirPlay", "AirPlay"},
	{8006, "Proxmox", "Proxmox"},
	{8009, "Cast", "Chromecast"},
	{8080, ":8080", "Web (8080)"},
	{8090, "Beszel", "Beszel"},
	{8096, "Jellyfin", "Jellyfin"},
	{8123, "HA", "Home Assistant"},
	{8443, ":8443", "Secure web (8443)"},
	{9000, "Portainer", "Portainer"},
	{9090, ":9090", "Web app (Cockpit, Prometheus)"},
	{9100, ":9100", "Raw printing or node exporter"},
	{32400, "Plex", "Plex"},
	{61208, "Glances", "Glances"},
	{62078, "iOS sync", "iPhone or iPad sync"},
}

func service(port int) Service {
	for _, s := range services {
		if s.Port == port {
			return s
		}
	}
	p := strconv.Itoa(port)
	return Service{port, ":" + p, "Port " + p}
}

// formatPorts lists open ports by short name, at most n of them, with a
// count of the rest: "SSH, HTTP, SMB +2". ports must be sorted.
func formatPorts(ports []int, n int) string {
	if len(ports) == 0 {
		return ""
	}
	var names []string
	for i, p := range ports {
		if i == n {
			break
		}
		names = append(names, service(p).Short)
	}
	s := strings.Join(names, ", ")
	if len(ports) > n {
		s += " +" + strconv.Itoa(len(ports)-n)
	}
	return s
}
