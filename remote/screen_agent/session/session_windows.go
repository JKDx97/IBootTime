//go:build windows

package session

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Ensure syscall.GetCurrentProcess and syscall.OpenProcessToken are available.
// These are in the standard library for windows builds.

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	wtsapi32 = syscall.NewLazyDLL("wtsapi32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	userenv  = syscall.NewLazyDLL("userenv.dll")

	procGetCurrentProcessId        = kernel32.NewProc("GetCurrentProcessId")
	procProcessIdToSessionId       = kernel32.NewProc("ProcessIdToSessionId")
	procWTSGetActiveConsoleSession = kernel32.NewProc("WTSGetActiveConsoleSessionId")
	procWaitForSingleObject        = kernel32.NewProc("WaitForSingleObject")
	procCloseHandle                = kernel32.NewProc("CloseHandle")

	procWTSQueryUserToken       = wtsapi32.NewProc("WTSQueryUserToken")
	procDuplicateTokenEx        = advapi32.NewProc("DuplicateTokenEx")
	procSetTokenInformation     = advapi32.NewProc("SetTokenInformation")
	procCreateProcessAsUser     = advapi32.NewProc("CreateProcessAsUserW")
	procCreateProcessWithToken  = advapi32.NewProc("CreateProcessWithTokenW")
	procCreateEnvironmentBlock  = userenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock = userenv.NewProc("DestroyEnvironmentBlock")
)

const (
	activeSessionUnavailable = 0xFFFFFFFF

	tokenAssignPrimary    = 0x0001
	tokenDuplicate        = 0x0002
	tokenImpersonate      = 0x0004
	tokenQuery            = 0x0008
	tokenAdjustDefault    = 0x0080
	tokenAdjustSessionID  = 0x0100
	tokenPrimary          = 1
	tokenSessionID        = 12
	securityImpersonation = 2

	createNoWindow            = 0x08000000
	createUnicodeEnvironment  = 0x00000400
	logonWithProfile          = 0x00000001
	waitTimeout               = 0x00000102
	waitObject0               = 0x00000000
	supervisorPollInterval    = 5 * time.Second
	supervisorRelaunchBackoff = 3 * time.Second
)

type startupInfo struct {
	Cb            uint32
	Reserved      *uint16
	Desktop       *uint16
	Title         *uint16
	X             uint32
	Y             uint32
	XSize         uint32
	YSize         uint32
	XCountChars   uint32
	YCountChars   uint32
	FillAttribute uint32
	Flags         uint32
	ShowWindow    uint16
	Reserved2     uint16
	Reserved2Ptr  uintptr
	StdInput      uintptr
	StdOutput     uintptr
	StdErr        uintptr
}

type processInformation struct {
	Process   uintptr
	Thread    uintptr
	ProcessID uint32
	ThreadID  uint32
}

func LogSessionInfo() {
	pid := currentProcessID()
	sid, err := CurrentSessionID()
	active := ActiveConsoleSessionID()
	if err != nil {
		log.Printf("session: pid=%d session=unknown err=%v activeConsole=%d", pid, err, active)
		return
	}
	log.Printf("session: pid=%d session=%d activeConsole=%d", pid, sid, active)
	if active != activeSessionUnavailable && sid != active {
		log.Printf("session warning: agent is not in the active console session; Windows may reject input injection")
	}
}

func CurrentSessionID() (uint32, error) {
	pid := currentProcessID()
	var sid uint32
	ok, _, err := procProcessIdToSessionId.Call(pid, uintptr(unsafe.Pointer(&sid)))
	if ok == 0 {
		return 0, err
	}
	return sid, nil
}

func ActiveConsoleSessionID() uint32 {
	active, _, _ := procWTSGetActiveConsoleSession.Call()
	return uint32(active)
}

func InActiveConsoleSession() bool {
	active := ActiveConsoleSessionID()
	if active == activeSessionUnavailable {
		return true
	}
	current, err := CurrentSessionID()
	return err == nil && current == active
}

func SuperviseInteractive(server string, fps, quality int, logf func(string, ...any)) {
	var child uintptr
	var childSession uint32
	var childPID uint32
	var consecutiveFails int

	for {
		active := ActiveConsoleSessionID()
		if child != 0 {
			state, _, _ := procWaitForSingleObject.Call(child, 0)
			if uint32(state) == waitTimeout && childSession == active {
				time.Sleep(supervisorPollInterval)
				continue
			}
			if uint32(state) == waitObject0 {
				logf("interactive agent pid=%d exited; relaunching", childPID)
			} else {
				logf("interactive agent session changed from %d to %d; relaunching", childSession, active)
			}
			procCloseHandle.Call(child)
			child = 0
			childPID = 0
		}

		if active == activeSessionUnavailable {
			logf("interactive launch skipped: active console session unavailable")
			time.Sleep(supervisorPollInterval)
			continue
		}

		handle, pid, err := LaunchInteractive(server, fps, quality, active)
		if err != nil {
			consecutiveFails++
			logf("interactive launch warning (attempt %d): %v", consecutiveFails, err)
			// Exponential backoff capped at 15s to avoid spamming during OOBE
			wait := supervisorRelaunchBackoff * time.Duration(consecutiveFails)
			if wait > 15*time.Second {
				wait = 15 * time.Second
			}
			time.Sleep(wait)
			continue
		}
		consecutiveFails = 0
		child = handle
		childPID = pid
		childSession = active
		logf("interactive agent launched pid=%d session=%d", childPID, childSession)
		time.Sleep(supervisorPollInterval)
	}
}

func LaunchInteractive(server string, fps, quality int, sessionID uint32) (uintptr, uint32, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, 0, err
	}
	cmdLine := fmt.Sprintf(`"%s" -server "%s" -fps %d -quality %d -interactive`, exe, server, fps, quality)

	// Try to get the logged-in user's token for this session.
	// During OOBE no user is logged in, so WTSQueryUserToken will fail.
	var userToken uintptr
	ok, _, wtsErr := procWTSQueryUserToken.Call(uintptr(sessionID), uintptr(unsafe.Pointer(&userToken)))
	gotUserToken := ok != 0
	if gotUserToken {
		defer procCloseHandle.Call(userToken)
	}

	if gotUserToken {
		// Normal case: user is logged in, launch as that user.
		handle, pid, err := launchWithToken(userToken, cmdLine, exe)
		if err == nil {
			return handle, pid, nil
		}
		log.Printf("launch with user token failed (session %d): %v; trying SYSTEM fallback", sessionID, err)
	}

	// OOBE fallback: no user token available. Launch as SYSTEM directly
	// targeting winsta0\default so the agent can capture the OOBE desktop.
	// This works because SYSTEM has access to all window stations.
	log.Printf("no user token for session %d (WTSQueryUserToken: %v); launching as SYSTEM in active session on winsta0\\default", sessionID, wtsErr)
	return launchAsSYSTEM(cmdLine, exe, sessionID)
}

// launchWithToken creates a process using a duplicated user token.
func launchWithToken(userToken uintptr, cmdLine, exe string) (uintptr, uint32, error) {
	var primaryToken uintptr
	access := uint32(tokenAssignPrimary | tokenDuplicate | tokenImpersonate | tokenQuery | tokenAdjustDefault | tokenAdjustSessionID)
	ok, _, err := procDuplicateTokenEx.Call(userToken, uintptr(access), 0, securityImpersonation, tokenPrimary, uintptr(unsafe.Pointer(&primaryToken)))
	if ok == 0 {
		return 0, 0, fmt.Errorf("DuplicateTokenEx failed: %w", err)
	}
	defer procCloseHandle.Call(primaryToken)

	return createProcessInDesktop(primaryToken, cmdLine, exe)
}

// launchAsSYSTEM creates a process using the current process token (SYSTEM)
// targeted at winsta0\default. Used during OOBE when no user is logged in.
func launchAsSYSTEM(cmdLine, exe string, sessionID uint32) (uintptr, uint32, error) {
	// Open our own process token
	var processToken uintptr
	currentProcess, _ := syscall.GetCurrentProcess()
	err := syscall.OpenProcessToken(currentProcess,
		syscall.TOKEN_DUPLICATE|syscall.TOKEN_QUERY|syscall.TOKEN_ASSIGN_PRIMARY,
		(*syscall.Token)(unsafe.Pointer(&processToken)))
	if err != nil {
		return 0, 0, fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer procCloseHandle.Call(processToken)

	var primaryToken uintptr
	access := uint32(tokenAssignPrimary | tokenDuplicate | tokenImpersonate | tokenQuery | tokenAdjustDefault | tokenAdjustSessionID)
	ok, _, dupErr := procDuplicateTokenEx.Call(processToken, uintptr(access), 0, securityImpersonation, tokenPrimary, uintptr(unsafe.Pointer(&primaryToken)))
	if ok == 0 {
		return 0, 0, fmt.Errorf("DuplicateTokenEx (SYSTEM): %w", dupErr)
	}
	defer procCloseHandle.Call(primaryToken)

	if sessionID != activeSessionUnavailable {
		ok, _, setErr := procSetTokenInformation.Call(
			primaryToken,
			tokenSessionID,
			uintptr(unsafe.Pointer(&sessionID)),
			unsafe.Sizeof(sessionID),
		)
		if ok == 0 {
			return 0, 0, fmt.Errorf("SetTokenInformation(TokenSessionId=%d): %w", sessionID, setErr)
		}
	}

	return createProcessInDesktop(primaryToken, cmdLine, exe)
}

// createProcessInDesktop is the shared helper that creates a process in
// winsta0\default with the given token.
func createProcessInDesktop(token uintptr, cmdLine, exe string) (uintptr, uint32, error) {
	var env uintptr
	ok, _, _ := procCreateEnvironmentBlock.Call(uintptr(unsafe.Pointer(&env)), token, 0)
	flags := uint32(createNoWindow)
	if ok != 0 {
		flags |= createUnicodeEnvironment
		defer procDestroyEnvironmentBlock.Call(env)
	}

	desktop, _ := syscall.UTF16PtrFromString(`winsta0\default`)
	command, _ := syscall.UTF16PtrFromString(cmdLine)
	workdir, _ := syscall.UTF16PtrFromString(filepath.Dir(exe))
	si := startupInfo{Cb: uint32(unsafe.Sizeof(startupInfo{})), Desktop: desktop}
	var pi processInformation
	ok, _, err := procCreateProcessAsUser.Call(
		token,
		0,
		uintptr(unsafe.Pointer(command)),
		0,
		0,
		0,
		uintptr(flags),
		env,
		uintptr(unsafe.Pointer(workdir)),
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if ok == 0 {
		createAsUserErr := err
		ok, _, err = procCreateProcessWithToken.Call(
			token,
			logonWithProfile,
			0,
			uintptr(unsafe.Pointer(command)),
			uintptr(flags),
			env,
			uintptr(unsafe.Pointer(workdir)),
			uintptr(unsafe.Pointer(&si)),
			uintptr(unsafe.Pointer(&pi)),
		)
		if ok == 0 {
			return 0, 0, fmt.Errorf("CreateProcessAsUser failed: %w; CreateProcessWithTokenW failed: %w", createAsUserErr, err)
		}
	}
	procCloseHandle.Call(pi.Thread)
	return pi.Process, pi.ProcessID, nil
}

func currentProcessID() uintptr {
	pid, _, _ := procGetCurrentProcessId.Call()
	return pid
}
