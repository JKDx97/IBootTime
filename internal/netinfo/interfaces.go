package netinfo

import (
	"fmt"
	"net"
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
