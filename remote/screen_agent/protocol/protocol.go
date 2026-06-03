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
	MsgTextInput  byte = 0x13
	MsgMouseWheel byte = 0x14
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

type MouseWheel struct {
	X     uint16
	Y     uint16
	Delta int16
}

type KeyEvent struct {
	KeyCode uint32
	Down    byte
}

type TextInput struct {
	Text string
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

func MarshalMouseWheel(m MouseWheel) []byte {
	out := make([]byte, 7)
	out[0] = MsgMouseWheel
	binary.BigEndian.PutUint16(out[1:3], m.X)
	binary.BigEndian.PutUint16(out[3:5], m.Y)
	binary.BigEndian.PutUint16(out[5:7], uint16(m.Delta))
	return out
}

func ParseMouseWheel(pkt []byte) (MouseWheel, error) {
	if len(pkt) != 7 || pkt[0] != MsgMouseWheel {
		return MouseWheel{}, errors.New("invalid mouse wheel packet")
	}
	return MouseWheel{
		X:     binary.BigEndian.Uint16(pkt[1:3]),
		Y:     binary.BigEndian.Uint16(pkt[3:5]),
		Delta: int16(binary.BigEndian.Uint16(pkt[5:7])),
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

func MarshalTextInput(t TextInput) []byte {
	data := []byte(t.Text)
	out := make([]byte, 3+len(data))
	out[0] = MsgTextInput
	binary.BigEndian.PutUint16(out[1:3], uint16(len(data)))
	copy(out[3:], data)
	return out
}

func ParseTextInput(pkt []byte) (TextInput, error) {
	if len(pkt) < 3 || pkt[0] != MsgTextInput {
		return TextInput{}, errors.New("invalid text input packet")
	}
	n := int(binary.BigEndian.Uint16(pkt[1:3]))
	if len(pkt) != 3+n {
		return TextInput{}, errors.New("invalid text input length")
	}
	return TextInput{Text: string(pkt[3:])}, nil
}

func PacketType(pkt []byte) (byte, error) {
	if len(pkt) == 0 {
		return 0, errors.New("empty packet")
	}
	switch pkt[0] {
	case MsgFrame, MsgCursor, MsgTileFrame, MsgMouseMove, MsgMouseClick, MsgKeyEvent, MsgTextInput, MsgMouseWheel:
		return pkt[0], nil
	default:
		return 0, fmt.Errorf("unknown packet type 0x%02x", pkt[0])
	}
}
