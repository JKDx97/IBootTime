//go:build windows

package input

import (
	"fmt"
	"log"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"iboottime/screen_agent/desktop"
	"iboottime/screen_agent/protocol"
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseeventfMove       = 0x0001
	mouseeventfLeftDown   = 0x0002
	mouseeventfLeftUp     = 0x0004
	mouseeventfRightDown  = 0x0008
	mouseeventfRightUp    = 0x0010
	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040
	mouseeventfWheel      = 0x0800
	mouseeventfAbsolute   = 0x8000

	keyeventfKeyUp   = 0x0002
	keyeventfUnicode = 0x0004

	smCXScreen = 0
	smCYScreen = 1
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSendInput        = user32.NewProc("SendInput")
	procMouseEvent       = user32.NewProc("mouse_event")
	procKeybdEvent       = user32.NewProc("keybd_event")
	procMapVirtualKey    = user32.NewProc("MapVirtualKeyW")
	procSetCursorPos     = user32.NewProc("SetCursorPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

var (
	eventCount int
)

type queuedPacket struct {
	data []byte
	done chan error
}

type mouseInput struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type keyboardInput struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type mousePacket struct {
	Type    uint32
	Padding uint32
	MI      mouseInput
}

type keyboardPacket struct {
	Type    uint32
	Padding uint32
	KI      keyboardInput
	Pad     [8]byte
}

type Controller struct {
	width  int
	height int
	queue  chan queuedPacket
}

func NewController() *Controller {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	c := &Controller{
		width:  int(w),
		height: int(h),
		queue:  make(chan queuedPacket, 256),
	}
	go c.inputLoop()
	return c
}

func (c *Controller) HandlePacket(pkt []byte) error {
	if len(pkt) == 0 {
		return nil
	}
	cp := append([]byte(nil), pkt...)
	done := make(chan error, 1)
	select {
	case c.queue <- queuedPacket{data: cp, done: done}:
	case <-time.After(500 * time.Millisecond):
		return fmt.Errorf("input queue timeout")
	}
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return fmt.Errorf("input execution timeout")
	}
}

func (c *Controller) inputLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	desktop.AttachInputDesktop()
	log.Printf("input thread ready on locked OS thread")
	for item := range c.queue {
		err := c.handleDirect(item.data)
		item.done <- err
	}
}

func (c *Controller) handleDirect(pkt []byte) error {
	if len(pkt) == 0 {
		return nil
	}
	eventCount++
	if eventCount <= 5 || eventCount%100 == 0 {
		log.Printf("input event #%d type=0x%02x bytes=%d", eventCount, pkt[0], len(pkt))
	}
	switch pkt[0] {
	case protocol.MsgMouseMove:
		m, err := protocol.ParseMouseMove(pkt)
		if err != nil {
			return err
		}
		return c.Move(int(m.X), int(m.Y))
	case protocol.MsgMouseClick:
		m, err := protocol.ParseMouseClick(pkt)
		if err != nil {
			return err
		}
		return c.ClickAt(int(m.X), int(m.Y), m.Button, m.Down != 0)
	case protocol.MsgMouseWheel:
		m, err := protocol.ParseMouseWheel(pkt)
		if err != nil {
			return err
		}
		return c.WheelAt(int(m.X), int(m.Y), int(m.Delta))
	case protocol.MsgKeyEvent:
		k, err := protocol.ParseKeyEvent(pkt)
		if err != nil {
			return err
		}
		return c.Key(k.KeyCode, k.Down != 0)
	case protocol.MsgTextInput:
		t, err := protocol.ParseTextInput(pkt)
		if err != nil {
			return err
		}
		return c.Text(t.Text)
	default:
		return fmt.Errorf("unsupported input packet 0x%02x", pkt[0])
	}
}

func (c *Controller) Move(x, y int) error {
	desktop.AttachInputDesktop()
	if ok, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y)); ok != 0 {
		return nil
	} else {
		log.Printf("input warning: SetCursorPos(%d,%d) failed: %v", x, y, err)
	}
	ax, ay := c.absolute(x, y)
	return sendMouse(mouseInput{Dx: ax, Dy: ay, DwFlags: mouseeventfMove | mouseeventfAbsolute})
}

func (c *Controller) Click(button byte, down bool) error {
	return c.ClickAt(-1, -1, button, down)
}

func (c *Controller) ClickAt(x, y int, button byte, down bool) error {
	desktop.AttachInputDesktop()
	var flags uint32
	switch button {
	case 1:
		if down {
			flags = mouseeventfLeftDown
		} else {
			flags = mouseeventfLeftUp
		}
	case 2:
		if down {
			flags = mouseeventfMiddleDown
		} else {
			flags = mouseeventfMiddleUp
		}
	case 3:
		if down {
			flags = mouseeventfRightDown
		} else {
			flags = mouseeventfRightUp
		}
	default:
		return nil
	}
	if x >= 0 && y >= 0 {
		if ok, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y)); ok == 0 {
			log.Printf("input warning: SetCursorPos before click (%d,%d) failed: %v", x, y, err)
			ax, ay := c.absolute(x, y)
			procMouseEvent.Call(uintptr(mouseeventfMove|mouseeventfAbsolute), uintptr(uint32(ax)), uintptr(uint32(ay)), 0, 0)
		}
		time.Sleep(8 * time.Millisecond)
	}
	log.Printf("input click button=%d down=%t x=%d y=%d flags=0x%x", button, down, x, y, flags)
	return sendMouse(mouseInput{DwFlags: flags})
}

func (c *Controller) WheelAt(x, y int, delta int) error {
	if delta == 0 {
		return nil
	}
	desktop.AttachInputDesktop()
	if x >= 0 && y >= 0 {
		if ok, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y)); ok == 0 {
			log.Printf("input warning: SetCursorPos before wheel (%d,%d) failed: %v", x, y, err)
		}
		time.Sleep(4 * time.Millisecond)
	}
	log.Printf("input wheel delta=%d x=%d y=%d", delta, x, y)
	return sendMouse(mouseInput{MouseData: uint32(int32(delta)), DwFlags: mouseeventfWheel})
}

func (c *Controller) Key(keycode uint32, down bool) error {
	var flags uint32
	if !down {
		flags = keyeventfKeyUp
	}
	scan, _, _ := procMapVirtualKey.Call(uintptr(keycode), 0)
	log.Printf("input key vk=0x%x scan=0x%x down=%t", keycode, scan, down)
	return sendKeyboard(keyboardInput{WVk: uint16(keycode), WScan: uint16(scan), DwFlags: flags})
}

func (c *Controller) Text(text string) error {
	desktop.AttachInputDesktop()
	for _, unit := range utf16.Encode([]rune(text)) {
		if err := sendKeyboard(keyboardInput{WScan: unit, DwFlags: keyeventfUnicode}); err != nil {
			return err
		}
		if err := sendKeyboard(keyboardInput{WScan: unit, DwFlags: keyeventfUnicode | keyeventfKeyUp}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) absolute(x, y int) (int32, int32) {
	if c.width <= 1 || c.height <= 1 {
		return 0, 0
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= c.width {
		x = c.width - 1
	}
	if y >= c.height {
		y = c.height - 1
	}
	return int32(x * 65535 / (c.width - 1)), int32(y * 65535 / (c.height - 1))
}

func sendMouse(mi mouseInput) error {
	desktop.AttachInputDesktop()
	in := mousePacket{Type: inputMouse, MI: mi}
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if r != 1 {
		log.Printf("input warning: SendInput(mouse flags=0x%x dx=%d dy=%d) returned %d: %v", mi.DwFlags, mi.Dx, mi.Dy, r, err)
		procMouseEvent.Call(
			uintptr(mi.DwFlags),
			uintptr(uint32(mi.Dx)),
			uintptr(uint32(mi.Dy)),
			uintptr(mi.MouseData),
			uintptr(mi.DwExtraInfo),
		)
		return nil
	}
	return nil
}

func sendKeyboard(ki keyboardInput) error {
	desktop.AttachInputDesktop()
	in := keyboardPacket{Type: inputKeyboard, KI: ki}
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if r != 1 {
		log.Printf("input warning: SendInput(key vk=0x%x scan=0x%x flags=0x%x) returned %d: %v", ki.WVk, ki.WScan, ki.DwFlags, r, err)
		procKeybdEvent.Call(uintptr(ki.WVk), uintptr(ki.WScan), uintptr(ki.DwFlags), uintptr(ki.DwExtraInfo))
		time.Sleep(2 * time.Millisecond)
	}
	return nil
}
