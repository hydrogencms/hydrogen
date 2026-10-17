package media

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
)

// JPEGOrientation returns the EXIF orientation (1–8) of a JPEG stream, 1 when it has none or r is not a JPEG
// with a readable EXIF segment. It reads only the segments before the image data.
func JPEGOrientation(r io.Reader) int {
	br := bufio.NewReader(io.LimitReader(r, 1<<20))
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi != [2]byte{0xff, 0xd8} {
		return 1
	}
	for {
		b, err := br.ReadByte()
		if err != nil {
			return 1
		}
		if b != 0xff {
			continue
		}
		m, err := br.ReadByte()
		for err == nil && m == 0xff {
			m, err = br.ReadByte()
		}
		if err != nil {
			return 1
		}
		switch {
		case m == 0 || m == 0x01 || (m >= 0xd0 && m <= 0xd7):
			continue
		case m == 0xda || m == 0xd9:
			return 1
		}
		var l [2]byte
		if _, err := io.ReadFull(br, l[:]); err != nil {
			return 1
		}
		n := int(binary.BigEndian.Uint16(l[:])) - 2
		if n < 0 {
			return 1
		}
		if m != 0xe1 {
			if _, err := br.Discard(n); err != nil {
				return 1
			}
			continue
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(br, seg); err != nil {
			return 1
		}
		if bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
	}
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(t[2:]) != 42 {
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for i := range n {
		e := off + 2 + i*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) != 0x0112 {
			continue
		}
		if bo.Uint16(t[e+2:]) != 3 || bo.Uint32(t[e+4:]) != 1 {
			return 1
		}
		if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}
