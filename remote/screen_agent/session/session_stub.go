//go:build !windows

package session

func LogSessionInfo() {}

func CurrentSessionID() (uint32, error) { return 0, nil }

func ActiveConsoleSessionID() uint32 { return 0 }

func InActiveConsoleSession() bool { return true }

func SuperviseInteractive(server string, fps, quality int, logf func(string, ...any)) {}
