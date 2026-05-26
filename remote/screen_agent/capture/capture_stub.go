//go:build !windows

package capture

import "fmt"

type Capturer struct{}

func New(quality int) (*Capturer, error) {
	return nil, fmt.Errorf("screen capture is only implemented on Windows")
}

func (c *Capturer) Size() (int, int) {
	return 0, 0
}

func (c *Capturer) CapturePackets() ([][]byte, error) {
	return nil, fmt.Errorf("screen capture is only implemented on Windows")
}
