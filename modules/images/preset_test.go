package images

import (
	"reflect"
	"testing"
)

func TestPresetValidate(t *testing.T) {
	ok := Preset{Name: "card", Width: 640, Height: 360, Fit: "cover", Widths: []int{320, 480}, Format: "jpeg", Quality: 80}
	if err := ok.Validate("builtin"); err != nil {
		t.Fatal(err)
	}
	if err := (Preset{Name: "a", Width: 1}).Validate("builtin"); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	bad := map[string]Preset{
		"name dash":        {Name: "a-b", Width: 10},
		"name upper":       {Name: "Card", Width: 10},
		"name empty":       {Width: 10},
		"name long":        {Name: "abcdefghijklmnopqrstuvwxyzabcdefg", Width: 10},
		"no width":         {Name: "a"},
		"width big":        {Name: "a", Width: 8193},
		"height big":       {Name: "a", Width: 10, Height: 8193},
		"cover no height":  {Name: "a", Width: 100, Fit: "cover"},
		"fit":              {Name: "a", Width: 100, Fit: "stretch"},
		"extra too small":  {Name: "a", Width: 100, Widths: []int{15}},
		"extra equal":      {Name: "a", Width: 100, Widths: []int{100}},
		"extra unsorted":   {Name: "a", Width: 100, Widths: []int{50, 40}},
		"extra duplicated": {Name: "a", Width: 100, Widths: []int{50, 50}},
		"format":           {Name: "a", Width: 100, Format: "bmp"},
		"webp builtin":     {Name: "a", Width: 100, Format: "webp"},
		"avif builtin":     {Name: "a", Width: 100, Format: "avif"},
		"quality":          {Name: "a", Width: 100, Quality: 101},
	}
	for name, p := range bad {
		if p.Validate("builtin") == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := (Preset{Name: "a", Width: 100, Format: "avif"}).Validate("imgproxy"); err != nil {
		t.Fatal(err)
	}
	if ok.Validate("magick") == nil {
		t.Fatal("unknown driver accepted")
	}
}

func TestPresetVersion(t *testing.T) {
	p := Preset{Name: "card", Width: 640, Height: 360, Fit: "cover", Widths: []int{320}}
	v := p.Version("builtin")
	if len(v) != 8 || v != p.Version("builtin") {
		t.Fatalf("version %q", v)
	}
	if v != (Preset{Name: "other", Width: 640, Height: 360, Fit: "cover", Widths: []int{320}, Format: "original", Quality: 82}).Version("builtin") {
		t.Error("defaults and the name must not matter")
	}
	changed := []Preset{
		{Width: 641, Height: 360, Fit: "cover", Widths: []int{320}},
		{Width: 640, Height: 361, Fit: "cover", Widths: []int{320}},
		{Width: 640, Height: 360, Fit: "contain", Widths: []int{320}},
		{Width: 640, Height: 360, Fit: "cover", Widths: []int{321}},
		{Width: 640, Height: 360, Fit: "cover", Widths: []int{320}, Format: "png"},
		{Width: 640, Height: 360, Fit: "cover", Widths: []int{320}, Quality: 70},
		{Width: 640, Height: 360, Fit: "cover", Widths: []int{320}, Upscale: true},
	}
	seen := map[string]bool{v: true, p.Version("imgproxy"): true}
	if len(seen) != 2 {
		t.Error("driver does not change the version")
	}
	for i, c := range changed {
		cv := c.Version("builtin")
		if seen[cv] {
			t.Errorf("change %d keeps the version", i)
		}
		seen[cv] = true
	}
}

func TestAllowed(t *testing.T) {
	got := Preset{Width: 640, Widths: []int{320, 480}}.Allowed()
	if !reflect.DeepEqual(got, []int{320, 480, 640}) {
		t.Fatal(got)
	}
	if got := (Preset{Width: 100}).Allowed(); !reflect.DeepEqual(got, []int{100}) {
		t.Fatal(got)
	}
}

func TestOutputFormatAndName(t *testing.T) {
	tests := []struct {
		format, src, ext, mime string
	}{
		{"", "image/jpeg", "jpg", "image/jpeg"},
		{"original", "image/png", "png", "image/png"},
		{"original", "image/gif", "png", "image/png"},
		{"original", "image/webp", "png", "image/png"},
		{"jpeg", "image/png", "jpg", "image/jpeg"},
		{"png", "image/jpeg", "png", "image/png"},
		{"webp", "image/png", "webp", "image/webp"},
		{"avif", "image/png", "avif", "image/avif"},
	}
	for _, tc := range tests {
		ext, mime := OutputFormat(Preset{Format: tc.format}, tc.src)
		if ext != tc.ext || mime != tc.mime {
			t.Errorf("%s/%s: %s %s", tc.format, tc.src, ext, mime)
		}
	}
	for in, want := range map[string]string{"cat.png": "cat.jpg", "cat.tar.gz": "cat.tar.jpg", "cat": "cat.jpg", "cat.JPEG": "cat.jpg", ".png": "image.jpg", "": "image.jpg"} {
		if got := OutputName(in, "jpg"); got != want {
			t.Errorf("OutputName(%q) = %q, want %q", in, got, want)
		}
	}
}
