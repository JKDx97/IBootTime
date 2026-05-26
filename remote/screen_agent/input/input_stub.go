//go:build !windows

package input

import "fmt"

type Controller struct{}

func NewController() *Controller {
	return &Controller{}
}

func (c *Controller) HandlePacket(pkt []byte) error {
	return fmt.Errorf("input injection is only implemented on Windows")
}
