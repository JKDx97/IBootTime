package dhcpsrv

import (
	"testing"

	"IBootTime/internal/config"
	"IBootTime/internal/logger"
)

func TestDefaultIPXEProtocolServesIPXEEFIForUEFI(t *testing.T) {
	cfg := &config.Config{BootProtocol: config.BootProtocolIPXE, HTTPPort: 8080}
	s := New(cfg, "192.168.1.9", "192.168.1.1", logger.New(20, nil), nil)

	got := s.getBootFilename("UEFI-x64", false, false)
	if got != "ipxe.efi" {
		t.Fatalf("UEFI PXE boot file = %q, want ipxe.efi", got)
	}
}

func TestUndionlyProtocolServesSNPEFIForUEFI(t *testing.T) {
	cfg := &config.Config{BootProtocol: config.BootProtocolUndionly, HTTPPort: 8080}
	s := New(cfg, "192.168.1.9", "192.168.1.1", logger.New(20, nil), nil)

	got := s.getBootFilename("UEFI-x64", false, false)
	if got != "snp.efi" {
		t.Fatalf("UEFI undionly boot file = %q, want snp.efi", got)
	}
}
