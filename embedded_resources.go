package main

import "embed"

// Embed all resource directories into the binary for portable distribution.
// These are extracted next to the executable on first run.

//go:embed all:drivers
var embeddedDrivers embed.FS

//go:embed all:agent_server
var embeddedAgentServer embed.FS

//go:embed all:agent_client
var embeddedAgentClient embed.FS

//go:embed all:tools/python-embed
var embeddedPythonEmbed embed.FS

//go:embed all:remote/winvnc
var embeddedWinVNC embed.FS

//go:embed portable_resources/remote/screen_agent/screen_agent.exe
var embeddedScreenAgent embed.FS

//go:embed all:noVNC-master
var embeddedNoVNC embed.FS
