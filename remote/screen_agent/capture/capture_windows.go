//go:build windows

package capture

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"syscall"
	"time"
	"unsafe"

	"iboottime/screen_agent/desktop"
	"iboottime/screen_agent/protocol"
)

const (
	tileSize = 256

	srccopy      = 0x00CC0020
	captureblt   = 0x40000000
	biRGB        = 0
	dibRGBColors = 0
	smCXScreen   = 0
	smCYScreen   = 1
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC              = user32.NewProc("GetDC")
	procGetDesktopWindow   = user32.NewProc("GetDesktopWindow")
	procGetWindowDC        = user32.NewProc("GetWindowDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [3]uint32
}

type Capturer struct {
	width    int
	height   int
	quality  int
	hashes   map[int][16]byte
	fullNext bool
	seq      int
	warned   bool
}

func New(quality int) (*Capturer, error) {
	if quality < 35 {
		quality = 35
	}
	if quality > 95 {
		quality = 95
	}
	procSetProcessDPIAware.Call()
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("unable to read screen metrics")
	}
	return &Capturer{
		width:    int(w),
		height:   int(h),
		quality:  quality,
		hashes:   make(map[int][16]byte),
		fullNext: true,
	}, nil
}

func (c *Capturer) Size() (int, int) {
	return c.width, c.height
}

func (c *Capturer) CapturePackets() ([][]byte, error) {
	img, err := c.captureImage()
	if err != nil {
		return nil, err
	}
	c.seq++
	// Send a keyframe at session start and periodically afterward. At 60 FPS,
	// repeated full JPEG frames create visible latency spikes; tile deltas keep
	// realtime control fluid while the periodic full frame self-heals any missed
	// tile updates.
	if c.seq == 1 || c.seq%120 == 0 {
		c.fullNext = true
	}

	if c.fullNext {
		data, err := encodeJPEG(img, c.quality)
		if err != nil {
			return nil, err
		}
		c.refreshHashes(img)
		c.fullNext = false
		return [][]byte{protocol.MarshalFrame(protocol.Frame{
			Width:   uint16(c.width),
			Height:  uint16(c.height),
			Quality: byte(c.quality),
			Data:    data,
		})}, nil
	}

	var packets [][]byte
	tilesX := (c.width + tileSize - 1) / tileSize
	tilesY := (c.height + tileSize - 1) / tileSize
	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			r := image.Rect(tx*tileSize, ty*tileSize, min((tx+1)*tileSize, c.width), min((ty+1)*tileSize, c.height))
			hash := hashTile(img, r)
			idx := ty*tilesX + tx
			if old, ok := c.hashes[idx]; ok && old == hash {
				continue
			}
			c.hashes[idx] = hash
			tile := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
			for y := 0; y < r.Dy(); y++ {
				src := img.PixOffset(r.Min.X, r.Min.Y+y)
				dst := tile.PixOffset(0, y)
				copy(tile.Pix[dst:dst+r.Dx()*4], img.Pix[src:src+r.Dx()*4])
			}
			data, err := encodeJPEG(tile, c.quality)
			if err != nil {
				return nil, err
			}
			packets = append(packets, protocol.MarshalTileFrame(protocol.TileFrame{
				X:       uint16(r.Min.X),
				Y:       uint16(r.Min.Y),
				Width:   uint16(r.Dx()),
				Height:  uint16(r.Dy()),
				Quality: byte(c.quality),
				Data:    data,
			}))
		}
	}
	return packets, nil
}

func (c *Capturer) captureImage() (*image.RGBA, error) {
	desktop.AttachInputDesktop()

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, screenDC)

	img, err := c.captureFromDC(screenDC)
	if err != nil {
		return nil, err
	}
	if !mostlyBlack(img) {
		return img, nil
	}

	desktop, _, _ := procGetDesktopWindow.Call()
	if desktop != 0 {
		windowDC, _, _ := procGetWindowDC.Call(desktop)
		if windowDC != 0 {
			defer procReleaseDC.Call(desktop, windowDC)
			if fallback, err := c.captureFromDC(windowDC); err == nil && !mostlyBlack(fallback) {
				return fallback, nil
			}
		}
	}

	if !c.warned {
		log.Printf("capture warning: GDI returned an almost black frame; agent may be running outside the interactive desktop/session")
		c.warned = true
	}
	return img, nil
}

func (c *Capturer) captureFromDC(sourceDC uintptr) (*image.RGBA, error) {
	memDC, _, _ := procCreateCompatibleDC.Call(sourceDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	var bits uintptr
	bi := bitmapInfo{}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(c.width)
	bi.Header.Height = -int32(c.height)
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB

	hbmp, _, _ := procCreateDIBSection.Call(sourceDC, uintptr(unsafe.Pointer(&bi)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbmp == 0 || bits == 0 {
		return nil, fmt.Errorf("CreateDIBSection failed")
	}
	defer procDeleteObject.Call(hbmp)

	old, _, _ := procSelectObject.Call(memDC, hbmp)
	if old != 0 {
		defer procSelectObject.Call(memDC, old)
	}

	ok, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(c.width), uintptr(c.height), sourceDC, 0, 0, srccopy|captureblt)
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}

	src := unsafe.Slice((*byte)(unsafe.Pointer(bits)), c.width*c.height*4)
	img := image.NewRGBA(image.Rect(0, 0, c.width, c.height))
	for i := 0; i < c.width*c.height; i++ {
		b := src[i*4]
		g := src[i*4+1]
		r := src[i*4+2]
		img.Pix[i*4] = r
		img.Pix[i*4+1] = g
		img.Pix[i*4+2] = b
		img.Pix[i*4+3] = 255
	}
	return img, nil
}

func mostlyBlack(img *image.RGBA) bool {
	if len(img.Pix) == 0 {
		return true
	}
	step := max(4, len(img.Pix)/4096)
	samples := 0
	bright := 0
	for i := 0; i+2 < len(img.Pix); i += step {
		if img.Pix[i] > 12 || img.Pix[i+1] > 12 || img.Pix[i+2] > 12 {
			bright++
		}
		samples++
	}
	return samples > 0 && bright < samples/200
}

func (c *Capturer) refreshHashes(img *image.RGBA) {
	tilesX := (c.width + tileSize - 1) / tileSize
	tilesY := (c.height + tileSize - 1) / tileSize
	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			r := image.Rect(tx*tileSize, ty*tileSize, min((tx+1)*tileSize, c.width), min((ty+1)*tileSize, c.height))
			c.hashes[ty*tilesX+tx] = hashTile(img, r)
		}
	}
}

func hashTile(img *image.RGBA, r image.Rectangle) [16]byte {
	h := md5.New()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		off := img.PixOffset(r.Min.X, y)
		h.Write(img.Pix[off : off+r.Dx()*4])
	}
	var out [16]byte
	copy(out[:], h.Sum(nil))
	return out
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func SleepFrame(rate time.Duration) {
	time.Sleep(rate)
}
