package httpboot

import (
	"strings"
	"testing"
)

func TestScreenAgentInjectedLaunchersUseHighPerformanceProfile(t *testing.T) {
	s := &Server{}
	wsURL := "ws://10.0.0.5:8080/ws/remote"

	scripts := map[string]string{
		"winpe start":    s.buildScreenAgentStartCMD(wsURL),
		"post install":   s.buildScreenAgentPostInstallLauncherCMD(wsURL),
		"setup complete": s.buildScreenAgentSetupCompleteCMD(wsURL),
		"post watcher":   s.buildScreenAgentPostInstallWatcherCMD(wsURL),
	}

	for name, script := range scripts {
		if !strings.Contains(script, "-fps 60") {
			t.Fatalf("%s launcher does not request 60 fps:\n%s", name, script)
		}
		if !strings.Contains(script, "-quality 88") {
			t.Fatalf("%s launcher does not request high-quality realtime JPEG profile:\n%s", name, script)
		}
	}
}
