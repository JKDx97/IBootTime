//go:build windows

package desktop

import (
	"log"
	"syscall"
	"unsafe"
)

const (
	desktopReadObjects   = 0x0001
	desktopCreateWindow  = 0x0002
	desktopCreateMenu    = 0x0004
	desktopHookControl   = 0x0008
	desktopJournalRecord = 0x0010
	desktopJournalPlay   = 0x0020
	desktopEnumerate     = 0x0040
	desktopWriteObjects  = 0x0080
	desktopSwitch        = 0x0100

	winstaEnumDesktops    = 0x0001
	winstaReadAttributes  = 0x0002
	winstaAccessClipboard = 0x0004
	winstaCreateDesktop   = 0x0008
	winstaWriteAttributes = 0x0010
	winstaAccessAtoms     = 0x0020
	winstaExitWindows     = 0x0040
	winstaEnumerate       = 0x0100
	winstaReadScreen      = 0x0200
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procOpenWindowStationW   = user32.NewProc("OpenWindowStationW")
	procSetProcessWindowSta  = user32.NewProc("SetProcessWindowStation")
	procOpenDesktopW         = user32.NewProc("OpenDesktopW")
	procOpenInputDesktop     = user32.NewProc("OpenInputDesktop")
	procSetThreadDesktop     = user32.NewProc("SetThreadDesktop")
	procCloseWindowStation   = user32.NewProc("CloseWindowStation")
	procCloseDesktop         = user32.NewProc("CloseDesktop")
	procGetUserObjectInfoW   = user32.NewProc("GetUserObjectInformationW")

	windowStationHandle uintptr
	desktopHandle       uintptr
	desktopNameCached   string
	warned              bool
	attached            bool
)

const uoiName = 2

// AttachInputDesktop attaches the calling thread to the current input desktop.
//
// IMPORTANT: This must be called on every capture iteration (not cached
// permanently) because Windows switches between desktops (Default <-> Winlogon)
// during OOBE, Microsoft account setup ("Getting things ready"), UAC prompts,
// lock screen, etc. Caching the first attachment causes the agent to capture a
// stale/black framebuffer after Windows switches the input desktop.
func AttachInputDesktop() {
	// First-time setup: attach the process to WinSta0. The window station
	// is process-wide and only needs to be set once.
	if windowStationHandle == 0 {
		name, _ := syscall.UTF16PtrFromString("WinSta0")
		access := uint32(winstaEnumDesktops | winstaReadAttributes | winstaAccessClipboard | winstaCreateDesktop | winstaWriteAttributes | winstaAccessAtoms | winstaExitWindows | winstaEnumerate | winstaReadScreen)
		hwinsta, _, err := procOpenWindowStationW.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(access))
		if hwinsta == 0 {
			warnOnce("OpenWindowStationW(WinSta0) failed: %v", err)
		} else {
			ok, _, err := procSetProcessWindowSta.Call(hwinsta)
			if ok == 0 {
				procCloseWindowStation.Call(hwinsta)
				warnOnce("SetProcessWindowStation(WinSta0) failed: %v", err)
			} else {
				windowStationHandle = hwinsta
				log.Printf("desktop: process window station attached to WinSta0")
			}
		}
	}

	// Re-evaluate the current input desktop on every call. Each call to
	// OpenInputDesktop returns a fresh handle, even when the active desktop
	// hasn't changed; we close the previous handle once the new one is
	// successfully attached to the thread.
	access := uint32(desktopReadObjects | desktopCreateWindow | desktopCreateMenu | desktopHookControl | desktopJournalRecord | desktopJournalPlay | desktopEnumerate | desktopWriteObjects | desktopSwitch)
	hdesk, _, openErr := procOpenInputDesktop.Call(0, 0, uintptr(access))
	if hdesk == 0 {
		// Couldn't open the input desktop. If we already have an attachment
		// keep it (better stale than nothing); otherwise fall back to "Default".
		if desktopHandle != 0 {
			return
		}
		name, _ := syscall.UTF16PtrFromString("Default")
		var fallbackErr error
		hdesk, _, fallbackErr = procOpenDesktopW.Call(uintptr(unsafe.Pointer(name)), 0, 0, uintptr(access))
		if hdesk == 0 {
			warnOnce("OpenInputDesktop failed (%v); OpenDesktopW(Default) failed: %v", openErr, fallbackErr)
			return
		}
	}

	ok, _, setErr := procSetThreadDesktop.Call(hdesk)
	if ok == 0 {
		// SetThreadDesktop fails when the thread already owns windows/hooks on
		// the current desktop. We keep the existing attachment so capture can
		// continue from whatever desktop the thread is on.
		procCloseDesktop.Call(hdesk)
		if desktopHandle == 0 {
			warnOnce("SetThreadDesktop failed: %v", setErr)
		}
		return
	}

	newName := desktopName(hdesk)
	if desktopHandle != 0 {
		procCloseDesktop.Call(desktopHandle)
	}
	if newName != desktopNameCached {
		if desktopNameCached == "" {
			log.Printf("desktop: input thread attached to %q", newName)
		} else {
			log.Printf("desktop: input desktop switched %q -> %q, re-attached", desktopNameCached, newName)
		}
		desktopNameCached = newName
	}
	desktopHandle = hdesk
	attached = true
}

// desktopName returns the desktop's name (e.g. "Default", "Winlogon"), or "" on failure.
func desktopName(hdesk uintptr) string {
	var buf [256]uint16
	var needed uint32
	ok, _, _ := procGetUserObjectInfoW.Call(
		hdesk,
		uoiName,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)*2),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

func warnOnce(format string, args ...any) {
	if warned {
		return
	}
	log.Printf("desktop warning: "+format, args...)
	warned = true
}
