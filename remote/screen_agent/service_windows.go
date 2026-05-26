//go:build windows

package main

import (
	"iboottime/screen_agent/session"
	"log"
	"sync"
	"syscall"
	"unsafe"
)

var (
	modAdvapi32                       = syscall.NewLazyDLL("advapi32.dll")
	procStartServiceCtrlDispatcherW   = modAdvapi32.NewProc("StartServiceCtrlDispatcherW")
	procRegisterServiceCtrlHandlerExW = modAdvapi32.NewProc("RegisterServiceCtrlHandlerExW")
	procSetServiceStatus              = modAdvapi32.NewProc("SetServiceStatus")
)

const (
	svcTypeWin32OwnProcess = 0x10
	svcRunning             = 0x04
	svcStopped             = 0x01
	svcStartPending        = 0x02
	svcStopPending         = 0x03
	svcAcceptStop          = 0x01
	svcAcceptShutdown      = 0x04
	svcControlStop         = 0x01
	svcControlShutdown     = 0x05
)

type svcStatus struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

type svcTableEntry struct {
	ServiceName *uint16
	ServiceProc uintptr
}

var (
	svcHandle  uintptr
	svcStopCh  = make(chan struct{})
	svcOnce    sync.Once
	svcServer  string
	svcFps     int
	svcQuality int
)

// runAsService registers with the Windows Service Control Manager and runs
// the screen agent as a proper Windows service. This allows the agent to
// start at boot — before any user logs in — covering OOBE.
func runAsService(server string, fps, quality int) error {
	svcServer = server
	svcFps = fps
	svcQuality = quality

	serviceName, _ := syscall.UTF16PtrFromString("IBootTimeAgent")
	table := [2]svcTableEntry{
		{ServiceName: serviceName, ServiceProc: syscall.NewCallback(svcMain)},
		{ServiceName: nil, ServiceProc: 0},
	}

	ret, _, err := procStartServiceCtrlDispatcherW.Call(uintptr(unsafe.Pointer(&table[0])))
	if ret == 0 {
		return err
	}
	return nil
}

func svcMain(argc uint32, argv **uint16) uintptr {
	serviceName, _ := syscall.UTF16PtrFromString("IBootTimeAgent")

	handle, _, _ := procRegisterServiceCtrlHandlerExW.Call(
		uintptr(unsafe.Pointer(serviceName)),
		syscall.NewCallback(svcHandler),
		0,
	)
	if handle == 0 {
		return 1
	}
	svcHandle = handle

	setSvcStatus(svcStartPending, 0)
	setSvcStatus(svcRunning, svcAcceptStop|svcAcceptShutdown)

	log.Printf("service: IBootTimeAgent running, launching supervisor...")

	// Supervise: launch agent in the active console session (not session 0).
	// This ensures the agent can capture the OOBE/user desktop.
	go session.SuperviseInteractive(svcServer, svcFps, svcQuality, log.Printf)

	// Block until stop/shutdown signal
	<-svcStopCh

	log.Printf("service: stop signal received, shutting down")
	setSvcStatus(svcStopped, 0)
	return 0
}

func svcHandler(control, eventType uint32, eventData, context uintptr) uintptr {
	switch control {
	case svcControlStop, svcControlShutdown:
		setSvcStatus(svcStopPending, 0)
		svcOnce.Do(func() { close(svcStopCh) })
		return 0
	}
	return 0
}

func setSvcStatus(state, accepted uint32) {
	status := svcStatus{
		ServiceType:      svcTypeWin32OwnProcess,
		CurrentState:     state,
		ControlsAccepted: accepted,
	}
	procSetServiceStatus.Call(svcHandle, uintptr(unsafe.Pointer(&status)))
}
