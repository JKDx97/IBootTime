package httpboot

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kdomanski/iso9660"
)

func isoOpen(f *os.File) (*iso9660.Image, error) {
	img, err := iso9660.OpenImage(f)
	if err != nil {
		return nil, fmt.Errorf("opening ISO image: %w", err)
	}
	return img, nil
}

// isoGetFileReader navigates the ISO9660 filesystem and returns a ReadSeeker
// for the requested file, along with its size. This avoids loading the entire
// file into memory at once and supports HTTP Range requests via http.ServeContent.
func isoGetFileReader(img *iso9660.Image, path string) (io.ReadSeeker, int64, error) {
	root, err := img.RootDir()
	if err != nil {
		return nil, 0, fmt.Errorf("reading root dir: %w", err)
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	current := root

	for i, part := range parts {
		children, err := current.GetChildren()
		if err != nil {
			return nil, 0, fmt.Errorf("listing children at %s: %w", part, err)
		}

		found := false
		for _, child := range children {
			name := cleanISOName(child.Name())
			if strings.EqualFold(name, part) {
				if i == len(parts)-1 {
					// Read the file content and wrap in a ReadSeeker
					reader := child.Reader()
					data, err := io.ReadAll(reader)
					if err != nil {
						return nil, 0, fmt.Errorf("reading file %s: %w", path, err)
					}
					return bytes.NewReader(data), int64(len(data)), nil
				}
				current = child
				found = true
				break
			}
		}

		if !found {
			return nil, 0, fmt.Errorf("path component %q not found in ISO", part)
		}
	}

	return nil, 0, fmt.Errorf("file %q not found in ISO", path)
}

// isoGetWindowsBootFileReader tries direct path lookup first and then falls back
// to common Windows paths and a full recursive tree search by basename.
func isoGetWindowsBootFileReader(img *iso9660.Image, requestPath string) (io.ReadSeeker, int64, string, error) {
	normalized := strings.ToLower(strings.Trim(requestPath, "/"))

	// 1. Direct path
	if r, size, err := isoGetFileReader(img, normalized); err == nil {
		return r, size, normalized, nil
	}

	// 2. Known Windows fallback paths
	var candidates []string
	var targetBase string
	switch normalized {
	case "boot/bcd":
		candidates = []string{
			"boot/bcd", "Boot/BCD", "BOOT/BCD",
			"efi/microsoft/boot/bcd", "EFI/Microsoft/Boot/BCD",
			"efi/boot/bcd", "EFI/Boot/BCD",
		}
		targetBase = "bcd"
	case "efi/microsoft/boot/bcd":
		candidates = []string{
			"efi/microsoft/boot/bcd", "EFI/Microsoft/Boot/BCD",
			"boot/bcd", "Boot/BCD", "BOOT/BCD",
		}
		targetBase = "bcd"
	case "boot/boot.sdi":
		candidates = []string{
			"boot/boot.sdi", "Boot/boot.sdi", "BOOT/BOOT.SDI",
			"efi/microsoft/boot/boot.sdi",
		}
		targetBase = "boot.sdi"
	case "sources/boot.wim":
		candidates = []string{
			"sources/boot.wim", "Sources/boot.wim", "SOURCES/BOOT.WIM",
			"source/boot.wim",
		}
		targetBase = "boot.wim"
	default:
		// Generic: just use basename
		parts := strings.Split(normalized, "/")
		targetBase = parts[len(parts)-1]
	}

	for _, candidate := range candidates {
		if r, size, err := isoGetFileReader(img, candidate); err == nil {
			return r, size, candidate, nil
		}
	}

	// 3. Recursive basename search across entire ISO tree
	root, err := img.RootDir()
	if err != nil {
		return nil, 0, "", fmt.Errorf("reading root dir for fallback search: %w", err)
	}

	var found *iso9660.File
	var foundPath string
	isoWalkTree(root, "", func(f *iso9660.File, path string) bool {
		if !f.IsDir() && strings.EqualFold(cleanISOName(f.Name()), targetBase) {
			found = f
			foundPath = path
			return false // stop walking
		}
		return true // continue
	})

	if found != nil {
		data, err := io.ReadAll(found.Reader())
		if err != nil {
			return nil, 0, "", fmt.Errorf("reading fallback file %s: %w", foundPath, err)
		}
		return bytes.NewReader(data), int64(len(data)), foundPath, nil
	}

	return nil, 0, "", fmt.Errorf("windows boot file %q not found in ISO tree", targetBase)
}

// isoWalkTree recursively walks the ISO directory tree.
// The callback returns false to stop walking.
func isoWalkTree(dir *iso9660.File, prefix string, fn func(f *iso9660.File, path string) bool) bool {
	children, err := dir.GetChildren()
	if err != nil {
		return true
	}
	for _, child := range children {
		name := cleanISOName(child.Name())
		if name == "" || name == "." || name == ".." || name == "\x00" {
			continue
		}
		path := prefix + "/" + name
		if !fn(child, path) {
			return false
		}
		if child.IsDir() {
			if !isoWalkTree(child, path, fn) {
				return false
			}
		}
	}
	return true
}

// isoListTree returns a flat list of all files/dirs in the ISO for debugging.
func isoListTree(img *iso9660.Image) []string {
	root, err := img.RootDir()
	if err != nil {
		return []string{"error: " + err.Error()}
	}
	var paths []string
	isoWalkTree(root, "", func(f *iso9660.File, path string) bool {
		if f.IsDir() {
			paths = append(paths, path+"/")
		} else {
			paths = append(paths, path)
		}
		return len(paths) < 2000 // safety limit
	})
	return paths
}

// cleanISOName strips the ISO9660 version suffix (";1") and trailing dots
func cleanISOName(name string) string {
	if idx := strings.Index(name, ";"); idx >= 0 {
		name = name[:idx]
	}
	return strings.TrimRight(name, ".")
}
