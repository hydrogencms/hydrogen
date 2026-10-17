package images

import (
	"image"
	"math"
)

// Focal is a point of interest in displayed coordinates, each 0–1.
type Focal struct{ X, Y float64 }

// Box is the planned output size and the source rectangle it is made from.
type Box struct {
	W, H int
	Crop image.Rectangle // in displayed source pixels
}

// Plan sizes the variant of width w of a srcW×srcH image (orientation applied). The box is w wide and
// round(Height·w/Width) high. contain scales the whole image to fit inside it; cover crops the image to the
// box's ratio around the focal point (centre by default) and scales that. Without upscale a box larger than
// the source shrinks, keeping its ratio. Outputs are at least 1×1.
func Plan(srcW, srcH int, focal *Focal, p Preset, w int) Box {
	if srcW < 1 || srcH < 1 {
		return Box{W: 1, H: 1, Crop: image.Rect(0, 0, 1, 1)}
	}
	bw := float64(w)
	bh := 0.0
	if p.Height > 0 {
		bh = math.Round(float64(p.Height) * float64(w) / float64(p.Width))
		bh = math.Max(bh, 1)
	}
	if p.fit() == Cover && bh > 0 {
		if !p.Upscale && (bw > float64(srcW) || bh > float64(srcH)) {
			s := math.Min(float64(srcW)/bw, float64(srcH)/bh)
			bw, bh = math.Round(bw*s), math.Round(bh*s)
		}
		ow, oh := max(int(bw), 1), max(int(bh), 1)
		if !p.Upscale {
			ow, oh = min(ow, srcW), min(oh, srcH)
		}
		cw, ch := srcW, srcH
		if srcW*oh >= srcH*ow {
			cw = min(max(int(math.Round(float64(srcH)*float64(ow)/float64(oh))), 1), srcW)
		} else {
			ch = min(max(int(math.Round(float64(srcW)*float64(oh)/float64(ow))), 1), srcH)
		}
		fx, fy := 0.5, 0.5
		if focal != nil {
			fx, fy = clamp01(focal.X), clamp01(focal.Y)
		}
		x0 := clampInt(int(math.Round(fx*float64(srcW)-float64(cw)/2)), 0, srcW-cw)
		y0 := clampInt(int(math.Round(fy*float64(srcH)-float64(ch)/2)), 0, srcH-ch)
		return Box{W: ow, H: oh, Crop: image.Rect(x0, y0, x0+cw, y0+ch)}
	}
	s := bw / float64(srcW)
	if bh > 0 {
		s = math.Min(s, bh/float64(srcH))
	}
	if !p.Upscale {
		s = math.Min(s, 1)
	}
	ow := min(max(int(math.Round(float64(srcW)*s)), 1), max(int(bw), 1))
	oh := max(int(math.Round(float64(srcH)*s)), 1)
	if !p.Upscale {
		ow, oh = min(ow, srcW), min(oh, srcH)
	}
	return Box{W: ow, H: oh, Crop: image.Rect(0, 0, srcW, srcH)}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0.5
	}
	return math.Min(math.Max(v, 0), 1)
}

func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }
