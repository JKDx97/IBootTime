//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

type instanceLock struct {
	handle uintptr
}

var (
	kernel32SingleInstance = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW       = kernel32SingleInstance.NewProc("CreateMutexW")
	procReleaseMutex       = kernel32SingleInstance.NewProc("ReleaseMutex")
	procCloseHandleSingle  = kernel32SingleInstance.NewProc("CloseHandle")
)

const errorAlreadyExists syscall.Errno = 183

func acquireAgentLock() (*instanceLock, bool) {
	return acquireNamedMutex(`Global\IBootTimeScreenAgentInteractive`)
}

func acquireSupervisorLock() (*instanceLock, bool) {
	return acquireNamedMutex(`Global\IBootTimeScreenAgentSupervisor`)
}

func acquireNamedMutex(name string) (*instanceLock, bool) {
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return &instanceLock{}, true
	}
	handle, _, callErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(ptr)))
	if handle == 0 {
		return &instanceLock{}, true
	}
	if callErr == errorAlreadyExists {
		procCloseHandleSingle.Call(handle)
		return nil, false
	}
	return &instanceLock{handle: handle}, true
}

func (l *instanceLock) Release() {
	if l == nil || l.handle == 0 {
		return
	}
	procReleaseMutex.Call(l.handle)
	procCloseHandleSingle.Call(l.handle)
	l.handle = 0
}
