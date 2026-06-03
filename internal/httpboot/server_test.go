package httpboot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSetupExeCommandOmitsUnattendForCleanISO(t *testing.T) {
	got := buildSetupExeCommand("Z", false, "", false)
	want := `cmd /c "cd /d Z:\sources && setup.exe"`
	if got != want {
		t.Fatalf("clean ISO setup command = %q, want %q", got, want)
	}
}

func TestBuildSetupExeCommandUsesUserUnattendWhenConfigured(t *testing.T) {
	got := buildSetupExeCommand("Z", true, "autounattend.xml", true)
	want := `cmd /c "cd /d Z:\sources && setup.exe /unattend:X:\IBootTime\autounattend.xml"`
	if got != want {
		t.Fatalf("user autounattend setup command = %q, want %q", got, want)
	}
}

func TestBuildSetupExeCommandUsesRootAutounattendWhenPresent(t *testing.T) {
	got := buildSetupExeCommand("Z", false, "autounattend.xml", true)
	want := `cmd /c "cd /d Z:\sources && setup.exe /unattend:Z:\autounattend.xml"`
	if got != want {
		t.Fatalf("root-autounattend ISO setup command = %q, want %q", got, want)
	}
}

func TestBuildSetupExeCommandUsesSafeUnattendForNonRootEmbeddedUnattendISO(t *testing.T) {
	got := buildSetupExeCommand("Z", false, "", true)
	want := `cmd /c "cd /d Z:\sources && setup.exe /unattend:X:\IBootTime\safe-unattend.xml"`
	if got != want {
		t.Fatalf("embedded-unattend ISO setup command = %q, want %q", got, want)
	}
}

func TestBuildUserUnattendDownloadScriptEscapesISOName(t *testing.T) {
	got := buildUserUnattendDownloadScript("192.168.1.10", 8080, "Win 11 Pro.iso")
	want := "http://192.168.1.10:8080/api/iso-unattend?iso=Win+11+Pro.iso"
	if !strings.Contains(got, want) {
		t.Fatalf("download script does not contain escaped endpoint %q:\n%s", want, got)
	}
	if !strings.Contains(got, `X:\IBootTime\autounattend.xml`) {
		t.Fatalf("download script does not target WinPE autounattend path:\n%s", got)
	}
}

func TestHasEmbeddedUnattend(t *testing.T) {
	root := t.TempDir()
	if hasEmbeddedUnattend(root) {
		t.Fatal("clean root unexpectedly reported embedded unattend")
	}

	if err := os.WriteFile(filepath.Join(root, "autounattend.xml"), []byte("<unattend/>"), 0644); err != nil {
		t.Fatal(err)
	}
	if !hasEmbeddedUnattend(root) {
		t.Fatal("root autounattend.xml was not detected")
	}
	if got := findRootAutounattend(root); got != "autounattend.xml" {
		t.Fatalf("root autounattend = %q, want autounattend.xml", got)
	}
}

func TestHasEmbeddedUnattendDetectsPantherOEM(t *testing.T) {
	root := t.TempDir()
	panther := filepath.Join(root, "sources", "$OEM$", "$$", "Panther")
	if err := os.MkdirAll(panther, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(panther, "unattend.xml"), []byte("<unattend/>"), 0644); err != nil {
		t.Fatal(err)
	}
	if !hasEmbeddedUnattend(root) {
		t.Fatal("OEM Panther unattend.xml was not detected")
	}
	if got := findRootAutounattend(root); got != "" {
		t.Fatalf("OEM Panther should not be treated as root autounattend, got %q", got)
	}
}
