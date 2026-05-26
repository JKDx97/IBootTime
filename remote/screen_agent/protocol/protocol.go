package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	MsgFrame     byte = 0x01
	MsgCursor    byte = 0x02
	MsgTileFrame byte = 0x03

	MsgMouseMove  byte = 0x10
	MsgMouseClick byte = 0x11
	MsgKeyEvent   byte = 0x12
)

type Frame struct {
	Width   uint16
	Height  uint16
	Quality byte
	Data    []byte
}

type TileFrame struct {
	X       uint16
	Y       uint16
	Width   uint16
	Height  uint16
	Quality byte
	Data    []byte
}

type Cursor struct {
	X     uint16
	Y     uint16
	Shape byte
}

type MouseMove struct {
	X uint16
	Y uint16
}

type MouseClick struct {
	X      uint16
	Y      uint16
	Button byte
	Down   byte
}

type KeyEvent struct {
	KeyCode uint32
	Down    byte
}

func MarshalFrame(f Frame) []byte {
	out := make([]byte, 6+len(f.Data))
	out[0] = MsgFrame
	binary.BigEndian.PutUint16(out[1:3], f.Width)
	binary.BigEndian.PutUint16(out[3:5], f.Height)
	out[5] = f.Quality
	copy(out[6:], f.Data)
	return out
}

func ParseFrame(pkt []byte) (Frame, error) {
	if len(pkt) < 6 || pkt[0] != MsgFrame {
		return Frame{}, errors.New("invalid frame packet")
	}
	return Frame{
		Width:   binary.BigEndian.Uint16(pkt[1:3]),
		Height:  binary.BigEndian.Uint16(pkt[3:5]),
		Quality: pkt[5],
		Data:    pkt[6:],
	}, nil
}

func MarshalTileFrame(f TileFrame) []byte {
	out := make([]byte, 10+len(f.Data))
	out[0] = MsgTileFrame
	binary.BigEndian.PutUint16(out[1:3], f.X)
	binary.BigEndian.PutUint16(out[3:5], f.Y)
	binary.BigEndian.PutUint16(out[5:7], f.Width)
	binary.BigEndian.PutUint16(out[7:9], f.Height)
	out[9] = f.Quality
	copy(out[10:], f.Data)
	return out
}

func ParseTileFrame(pkt []byte) (TileFrame, error) {
	if len(pkt) < 10 || pkt[0] != MsgTileFrame {
		return TileFrame{}, errors.New("invalid tile frame packet")
	}
	return TileFrame{
		X:       binary.BigEndian.Uint16(pkt[1:3]),
		Y:       binary.BigEndian.Uint16(pkt[3:5]),
		Width:   binary.BigEndian.Uint16(pkt[5:7]),
		Height:  binary.BigEndian.Uint16(pkt[7:9]),
		Quality: pkt[9],
		Data:    pkt[10:],
	}, nil
}

func MarshalCursor(c Cursor) []byte {
	out := make([]byte, 6)
	out[0] = MsgCursor
	binary.BigEndian.PutUint16(out[1:3], c.X)
	binary.BigEndian.PutUint16(out[3:5], c.Y)
	out[5] = c.Shape
	return out
}

func ParseCursor(pkt []byte) (Cursor, error) {
	if len(pkt) != 6 || pkt[0] != MsgCursor {
		return Cursor{}, errors.New("invalid cursor packet")
	}
	return Cursor{
		X:     binary.BigEndian.Uint16(pkt[1:3]),
		Y:     binary.BigEndian.Uint16(pkt[3:5]),
		Shape: pkt[5],
	}, nil
}

func MarshalMouseMove(m MouseMove) []byte {
	out := make([]byte, 5)
	out[0] = MsgMouseMove
	binary.BigEndian.PutUint16(out[1:3], m.X)
	binary.BigEndian.PutUint16(out[3:5], m.Y)
	return out
}

func ParseMouseMove(pkt []byte) (MouseMove, error) {
	if len(pkt) != 5 || pkt[0] != MsgMouseMove {
		return MouseMove{}, errors.New("invalid mouse move packet")
	}
	return MouseMove{
		X: binary.BigEndian.Uint16(pkt[1:3]),
		Y: binary.BigEndian.Uint16(pkt[3:5]),
	}, nil
}

func MarshalMouseClick(m MouseClick) []byte {
	out := make([]byte, 7)
	out[0] = MsgMouseClick
	binary.BigEndian.PutUint16(out[1:3], m.X)
	binary.BigEndian.PutUint16(out[3:5], m.Y)
	out[5] = m.Button
	out[6] = m.Down
	return out
}

func ParseMouseClick(pkt []byte) (MouseClick, error) {
	if len(pkt) != 7 || pkt[0] != MsgMouseClick {
		return MouseClick{}, errors.New("invalid mouse click packet")
	}
	return MouseClick{
		X:      binary.BigEndian.Uint16(pkt[1:3]),
		Y:      binary.BigEndian.Uint16(pkt[3:5]),
		Button: pkt[5],
		Down:   pkt[6],
	}, nil
}

func MarshalKeyEvent(k KeyEvent) []byte {
	out := make([]byte, 6)
	out[0] = MsgKeyEvent
	binary.BigEndian.PutUint32(out[1:5], k.KeyCode)
	out[5] = k.Down
	return out
}

func ParseKeyEvent(pkt []byte) (KeyEvent, error) {
	if len(pkt) != 6 || pkt[0] != MsgKeyEvent {
		return KeyEvent{}, errors.New("invalid key event packet")
	}
	return KeyEvent{
		KeyCode: binary.BigEndian.Uint32(pkt[1:5]),
		Down:    pkt[5],
	}, nil
}

func PacketType(pkt []byte) (byte, error) {
	if len(pkt) == 0 {
		return 0, errors.New("empty packet")
	}
	switch pkt[0] {
	case MsgFrame, MsgCursor, MsgTileFrame, MsgMouseMove, MsgMouseClick, MsgKeyEvent:
		return pkt[0], nil
	default:
		return 0, fmt.Errorf("unknown packet type 0x%02x", pkt[0])
	}
}
