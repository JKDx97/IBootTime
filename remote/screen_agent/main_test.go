package main

import "testing"

func TestDefaultRemotePerformanceProfileTargetsRealtime1080p(t *testing.T) {
	if defaultFPS != 60 {
		t.Fatalf("defaultFPS = %d, want 60 for realtime remote control on fast networks", defaultFPS)
	}
	if defaultQuality != 88 {
		t.Fatalf("defaultQuality = %d, want 88 to preserve 1080p detail without excessive JPEG encode cost", defaultQuality)
	}
}
