package images

import (
	"bytes"
	"image"

	"github.com/hydrogencms/hydrogen/features/media"
)

// orientation reads the EXIF orientation of a JPEG; 1 for anything else.
func orientation(data []byte) int { return media.JPEGOrientation(bytes.NewReader(data)) }

// swapsAxes reports whether orientation o (5–8) turns the stored image on its side.
func swapsAxes(o int) bool { return o >= 5 && o <= 8 }

// displayedRect maps a rectangle in displayed pixels back to the stored image of size sw×sh.
func displayedRect(o int, r image.Rectangle, sw, sh int) image.Rectangle {
	x0, y0 := storedPoint(o, r.Min.X, r.Min.Y, sw, sh)
	x1, y1 := storedPoint(o, r.Max.X, r.Max.Y, sw, sh)
	return image.Rect(x0, y0, x1, y1)
}

// storedPoint maps a corner point of the displayed image to the stored image's.
func storedPoint(o, dx, dy, sw, sh int) (x, y int) {
	switch o {
	case 2:
		return sw - dx, dy
	case 3:
		return sw - dx, sh - dy
	case 4:
		return dx, sh - dy
	case 5:
		return dy, dx
	case 6:
		return dy, sh - dx
	case 7:
		return sw - dy, sh - dx
	case 8:
		return sw - dy, dx
	}
	return dx, dy
}

// orient rotates or flips a stored image so that it shows as displayed.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := sw, sh
	if swapsAxes(o) {
		dw, dh = sh, sw
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range sh {
		srow := src.Pix[y*src.Stride : y*src.Stride+sw*4]
		for x := range sw {
			var tx, ty int
			switch o {
			case 2:
				tx, ty = sw-1-x, y
			case 3:
				tx, ty = sw-1-x, sh-1-y
			case 4:
				tx, ty = x, sh-1-y
			case 5:
				tx, ty = y, x
			case 6:
				tx, ty = sh-1-y, x
			case 7:
				tx, ty = sh-1-y, sw-1-x
			case 8:
				tx, ty = y, sw-1-x
			}
			copy(dst.Pix[ty*dst.Stride+tx*4:ty*dst.Stride+tx*4+4], srow[x*4:x*4+4])
		}
	}
	return dst
}
