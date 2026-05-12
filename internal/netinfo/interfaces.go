package netinfo

import (
	"fmt"
	"net"
	"strings"

	"IBootTime/internal/hidecmd"
)

type NetInterface struct {
	Name       string `json:"name"`
	IP         string `json:"ip"`
	MAC        string `json:"mac"`
	IsUp       bool   `json:"isUp"`
	IsLoopback bool   `json:"isLoopback"`
}

func ListInterfaces() ([]NetInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("listing interfaces: %w", err)
	}

	var result []NetInterface
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		ni := NetInterface{
			Name:       iface.Name,
			MAC:        iface.HardwareAddr.String(),
			IsUp:       iface.Flags&net.FlagUp != 0,
			IsLoopback: iface.Flags&net.FlagLoopback != 0,
		}

		addrs, err := iface.Addrs()
		if err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
					ni.IP = ipNet.IP.String()
					break
				}
			}
		}

		if ni.IP != "" {
			result = append(result, ni)
		}
	}

	return result, nil
}

func GetInterfaceIP(name string) (string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", fmt.Errorf("interface %q not found: %w", name, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("getting addrs for %q: %w", name, err)
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
	}

	return "", fmt.Errorf("no IPv4 address on interface %q", name)
}

// GetDefaultGateway detects the system's default IPv4 gateway.
// Falls back to serverIP's .1 if detection fails.
func GetDefaultGateway(serverIP string) string {
	// Try PowerShell (Windows 10+)
	out, err := hidecmd.Command("powershell", "-NoProfile", "-Command",
		"(Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Select-Object -First 1).NextHop",
	).Output()
	if err == nil {
		gw := strings.TrimSpace(string(out))
		if ip := net.ParseIP(gw); ip != nil && ip.To4() != nil {
			return gw
		}
	}

	// Fallback: .1 in the server's subnet
	sip := net.ParseIP(serverIP).To4()
	if sip != nil {
		return fmt.Sprintf("%d.%d.%d.1", sip[0], sip[1], sip[2])
	}
	return "0.0.0.0"
}
