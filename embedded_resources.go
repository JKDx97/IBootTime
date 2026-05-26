package main

import "embed"

// Embed all resource directories into the binary for portable distribution.
// These are extracted next to the executable on first run.

//go:embed all:drivers
var embeddedDrivers embed.FS
