package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ErrTooLarge is returned for a source with more pixels than allowed, before it is decoded.
var ErrTooLarge = errors.New("images: source too large")

// ErrUnsupported is returned for data that is not a decodable raster image, or a format the engine cannot write.
var ErrUnsupported = errors.New("images: unsupported image")

// maxSourceBytes caps how much of a source Transform reads.
const maxSourceBytes = 64 << 20

// Decodable reports whether the builtin engine can read images of this media type.
func Decodable(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}

// Transform decodes src (orientation applied), plans with the decoded displayed size, crops, resizes and
// encodes. ctx cancels between stages. It returns the bytes and their media type.
func Transform(ctx context.Context, src io.Reader, p Preset, w int, focal *Focal, maxPixels int64) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	lr := &io.LimitedReader{R: src, N: maxSourceBytes + 1}
	var head bytes.Buffer
	cfg, format, err := image.DecodeConfig(io.TeeReader(lr, &head))
	if lr.N <= 0 {
		return nil, "", ErrTooLarge
	}
	if err != nil || !Decodable("image/"+format) || cfg.Width < 1 || cfg.Height < 1 {
		return nil, "", ErrUnsupported
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, "", ErrTooLarge
	}
	srcMIME := "image/" + format
	ext, outMIME := OutputFormat(p, srcMIME)
	if ext != "jpg" && ext != "png" {
		return nil, "", fmt.Errorf("%w: output %s needs imgproxy", ErrUnsupported, ext)
	}
	o := 1
	if format == "jpeg" {
		o = orientation(head.Bytes())
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	img, _, err := image.Decode(io.MultiReader(&head, lr))
	if lr.N <= 0 {
		return nil, "", ErrTooLarge
	}
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	if format == "gif" && img.Bounds() != image.Rect(0, 0, cfg.Width, cfg.Height) {
		screen := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
		draw.Draw(screen, img.Bounds().Intersect(screen.Bounds()), img, img.Bounds().Min, draw.Src)
		img = screen
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := sw, sh
	if swapsAxes(o) {
		dw, dh = sh, sw
	}
	box := Plan(dw, dh, focal, p, w)
	crop := displayedRect(o, box.Crop, sw, sh).Add(b.Min)
	tw, th := box.W, box.H
	if swapsAxes(o) {
		tw, th = th, tw
	}

	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	scale(img, crop, out)
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	out = orient(out, o)

	var buf bytes.Buffer
	if ext == "jpg" {
		var enc image.Image = out
		if !opaque(img) {
			bg := image.NewRGBA(out.Bounds())
			draw.Draw(bg, bg.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
			draw.Draw(bg, bg.Bounds(), out, image.Point{}, draw.Over)
			enc = bg
		}
		err = jpeg.Encode(&buf, enc, &jpeg.Options{Quality: p.quality()})
	} else {
		err = (&png.Encoder{}).Encode(&buf, out)
	}
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), outMIME, nil
}

func opaque(img image.Image) bool {
	switch img.(type) {
	case *image.YCbCr, *image.Gray, *image.Gray16, *image.CMYK:
		return true
	}
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}

// scale draws the crop of src into dst, shrinking in two steps when the reduction is large.
func scale(src image.Image, crop image.Rectangle, dst *image.RGBA) {
	tw, th := dst.Bounds().Dx(), dst.Bounds().Dy()
	switch {
	case crop.Dx() == tw && crop.Dy() == th:
		draw.Draw(dst, dst.Bounds(), src, crop.Min, draw.Src)
		return
	case crop.Dx() > 4*tw || crop.Dy() > 4*th:
		mw, mh := min(2*tw, crop.Dx()), min(2*th, crop.Dy())
		mid := image.NewRGBA(image.Rect(0, 0, mw, mh))
		xdraw.ApproxBiLinear.Scale(mid, mid.Bounds(), src, crop, xdraw.Src, nil)
		src, crop = mid, mid.Bounds()
	}
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Src, nil)
}
