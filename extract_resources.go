package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// extractEmbeddedResources extracts embedded resources next to the executable
// so the single .exe is fully portable. Skips files that already exist on disk.
func extractEmbeddedResources(exeDir string) {
	// Skip extraction in dev mode (source tree has everything already)
	if _, err := os.Stat(filepath.Join(exeDir, "wails.json")); err == nil {
		return
	}

	type bundle struct {
		marker string   // if this file exists, skip extraction
		fsys   embed.FS // embedded filesystem
		label  string   // for logging
	}

	bundles := []bundle{
		{filepath.Join("remote", "winvnc", "winvnc.exe"), embeddedVNC, "UltraVNC"},
		{filepath.Join("noVNC-master", "core", "rfb.js"), embeddedNoVNC, "noVNC"},
		{filepath.Join("drivers", "drivers_universal"), embeddedDrivers, "Drivers"},
	}

	for _, b := range bundles {
		markerPath := filepath.Join(exeDir, b.marker)
		if _, err := os.Stat(markerPath); err == nil {
			continue // already extracted
		}
		fmt.Printf("[IBootTime] Extracting %s resources...\n", b.label)
		extractFS(b.fsys, exeDir)
	}

	// Create empty resource directories the user can populate
	for _, dir := range []string{
		filepath.Join(exeDir, "isos"),
	} {
		os.MkdirAll(dir, 0755)
	}
}

// extractFS walks an embedded FS and writes every file to destBase, preserving
// the directory structure. Existing files are NOT overwritten.
func extractFS(fsys embed.FS, destBase string) {
	fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}

		destPath := filepath.Join(destBase, path)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Skip if file already exists on disk
		if _, err := os.Stat(destPath); err == nil {
			return nil
		}

		data, err := fsys.ReadFile(path)
		if err != nil {
			return err
		}

		os.MkdirAll(filepath.Dir(destPath), 0755)
		return os.WriteFile(destPath, data, 0755)
	})
}
