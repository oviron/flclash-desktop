package main

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestIcons(t *testing.T) {
	dir := os.Getenv("FLCLASH_ICON_DIR")
	for _, state := range []string{"off", "on", "blocked", "warning", "busy"} {
		for _, size := range []int{16, 20, 24, 32, 64} {
			img := drawIcon(size, state, color.NRGBA{255, 255, 255, 255})
			opaque := 0
			for i := 3; i < len(img.Pix); i += 4 {
				if img.Pix[i] > 128 {
					opaque++
				}
			}
			if opaque < size*size/12 {
				t.Errorf("%s at %d: only %d pixels drawn", state, size, opaque)
			}
			if dir != "" {
				f, _ := os.Create(filepath.Join(dir, state+"-"+itoa(size)+".png"))
				png.Encode(f, img)
				f.Close()
			}
		}
	}
}

func itoa(n int) string { return string(rune('0'+n/10)) + string(rune('0'+n%10)) }
