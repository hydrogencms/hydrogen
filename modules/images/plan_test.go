package images

import (
	"image"
	"testing"
)

func TestPlan(t *testing.T) {
	card := Preset{Name: "card", Width: 640, Height: 360, Fit: Cover}
	cardUp := card
	cardUp.Upscale = true
	hero := Preset{Name: "hero", Width: 1600}
	boxed := Preset{Name: "boxed", Width: 400, Height: 400}
	tests := []struct {
		name         string
		srcW, srcH   int
		focal        *Focal
		p            Preset
		w            int
		wantW, wantH int
		crop         image.Rectangle
	}{
		{"cover centred", 1200, 800, nil, card, 640, 640, 360, image.Rect(0, 63, 1200, 738)},
		{"cover smaller width keeps ratio", 1200, 800, nil, card, 320, 320, 180, image.Rect(0, 63, 1200, 738)},
		{"cover focal top", 1200, 800, &Focal{0.5, 0}, card, 640, 640, 360, image.Rect(0, 0, 1200, 675)},
		{"cover focal bottom", 1200, 800, &Focal{0.5, 1}, card, 640, 640, 360, image.Rect(0, 125, 1200, 800)},
		{"cover focal beyond range", 1200, 800, &Focal{-3, 9}, card, 640, 640, 360, image.Rect(0, 125, 1200, 800)},
		{"cover portrait source picks left half", 400, 1000, &Focal{0, 0.5}, Preset{Name: "sq", Width: 200, Height: 200, Fit: Cover}, 200, 200, 200, image.Rect(0, 300, 400, 700)},
		{"cover wide source focal left", 2000, 500, &Focal{0, 0.5}, Preset{Name: "sq", Width: 200, Height: 200, Fit: Cover}, 200, 200, 200, image.Rect(0, 0, 500, 500)},
		{"cover wide source focal right", 2000, 500, &Focal{1, 0.5}, Preset{Name: "sq", Width: 200, Height: 200, Fit: Cover}, 200, 200, 200, image.Rect(1500, 0, 2000, 500)},
		{"cover shrinks without upscale", 320, 300, nil, card, 640, 320, 180, image.Rect(0, 60, 320, 240)},
		{"cover shrinks by the tighter side", 1000, 200, nil, card, 640, 356, 200, image.Rect(322, 0, 678, 200)},
		{"cover upscale", 320, 300, nil, cardUp, 640, 640, 360, image.Rect(0, 60, 320, 240)},
		{"contain width only", 1200, 800, nil, hero, 800, 800, 533, image.Rect(0, 0, 1200, 800)},
		{"contain no upscale", 1200, 800, nil, hero, 1600, 1200, 800, image.Rect(0, 0, 1200, 800)},
		{"contain box", 1200, 800, nil, boxed, 400, 400, 267, image.Rect(0, 0, 1200, 800)},
		{"contain tall", 800, 1200, nil, boxed, 400, 267, 400, image.Rect(0, 0, 800, 1200)},
		{"contain small source stays", 100, 50, nil, boxed, 400, 100, 50, image.Rect(0, 0, 100, 50)},
		{"contain upscale", 100, 50, nil, Preset{Name: "u", Width: 400, Upscale: true}, 400, 400, 200, image.Rect(0, 0, 100, 50)},
		{"1x1 floor", 1000, 1, nil, Preset{Name: "t", Width: 16}, 16, 16, 1, image.Rect(0, 0, 1000, 1)},
		{"no upscale of a thin source", 1, 1000, nil, Preset{Name: "t", Width: 16}, 16, 1, 1000, image.Rect(0, 0, 1, 1000)},
	}
	for _, tc := range tests {
		got := Plan(tc.srcW, tc.srcH, tc.focal, tc.p, tc.w)
		if got.W != tc.wantW || got.H != tc.wantH || (tc.crop != image.Rectangle{} && got.Crop != tc.crop) {
			t.Errorf("%s: got %dx%d crop %v; want %dx%d crop %v", tc.name, got.W, got.H, got.Crop, tc.wantW, tc.wantH, tc.crop)
		}
		if !got.Crop.In(image.Rect(0, 0, tc.srcW, tc.srcH)) || got.W < 1 || got.H < 1 {
			t.Errorf("%s: box out of range: %+v", tc.name, got)
		}
	}
}

func TestPlanDegenerate(t *testing.T) {
	if b := Plan(0, 0, nil, Preset{Width: 100}, 100); b.W != 1 || b.H != 1 {
		t.Fatalf("%+v", b)
	}
}
