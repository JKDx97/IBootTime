package httpboot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSetupExeCommandOmitsUnattendForCleanISO(t *testing.T) {
	got := buildSetupExeCommand("Z", false)
	want := `Z:\setup.exe`
	if got != want {
		t.Fatalf("clean ISO setup command = %q, want %q", got, want)
	}
}

func TestBuildSetupExeCommandUsesSafeUnattendForEmbeddedUnattendISO(t *testing.T) {
	got := buildSetupExeCommand("Z", true)
	want := `Z:\setup.exe /unattend:X:\IBootTime\safe-unattend.xml`
	if got != want {
		t.Fatalf("embedded-unattend ISO setup command = %q, want %q", got, want)
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
}
