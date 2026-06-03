//go:build !windows

package main

type instanceLock struct{}

func acquireAgentLock() (*instanceLock, bool) {
	return &instanceLock{}, true
}

func acquireSupervisorLock() (*instanceLock, bool) {
	return &instanceLock{}, true
}

func (l *instanceLock) Release() {}
