package main

import (
	"image"
	"image/color"
	"math"
)

// The icon in the notification area is the shield the macOS menu bar shows,
// drawn here: one color on a clear background, like the system's own.
//
//	off      the outline
//	on       filled, with a check cut out
//	blocked  filled, with a lock cut out
//	warning  the outline with "!": no helper, or on but not capturing
//	busy     the outline with three dots: a change is on its way
func drawIcon(size int, state string, ink color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const samples = 4
	stroke := math.Max(0.085, 1.25/float64(size))
	for py := range size {
		for px := range size {
			hits := 0
			for sy := range samples {
				for sx := range samples {
					x := (float64(px) + (float64(sx)+0.5)/samples) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/samples) / float64(size)
					if painted(state, x, y, stroke) {
						hits++
					}
				}
			}
			if hits > 0 {
				c := ink
				c.A = uint8(int(ink.A) * hits / (samples * samples))
				img.SetNRGBA(px, py, c)
			}
		}
	}
	return img
}

func painted(state string, x, y, stroke float64) bool {
	switch state {
	case "on":
		return shield(x, y) && !check(x, y)
	case "blocked":
		return shield(x, y) && !lock(x, y)
	case "warning":
		return outline(x, y, stroke) || exclamation(x, y)
	case "busy":
		return outline(x, y, stroke) || dots(x, y)
	default:
		return outline(x, y, stroke)
	}
}

// shield: a gently arched top, straight sides, a bottom that narrows to a point.
func shield(x, y float64) bool {
	const half, mid, bottom = 0.37, 0.5, 0.96
	dx := math.Abs(x - 0.5)
	if dx > half || y > bottom {
		return false
	}
	if y < 0.06+0.11*(dx/half)*(dx/half) {
		return false
	}
	if y <= mid {
		return true
	}
	t := (y - mid) / (bottom - mid)
	return dx <= half*math.Pow(math.Cos(t*math.Pi/2), 0.75)
}

// outline: the edge of the shield, a stroke wide.
func outline(x, y, stroke float64) bool {
	if !shield(x, y) {
		return false
	}
	for a := 0.0; a < 2*math.Pi; a += math.Pi / 6 {
		if !shield(x+stroke*math.Cos(a), y+stroke*math.Sin(a)) {
			return true
		}
	}
	return false
}

func check(x, y float64) bool {
	return near(x, y, 0.31, 0.5, 0.45, 0.65, 0.06) || near(x, y, 0.45, 0.65, 0.7, 0.36, 0.06)
}

func lock(x, y float64) bool {
	if x >= 0.34 && x <= 0.66 && y >= 0.46 && y <= 0.72 {
		return true
	}
	r := math.Hypot(x-0.5, y-0.46)
	return y <= 0.46 && r >= 0.085 && r <= 0.145
}

func exclamation(x, y float64) bool {
	return (math.Abs(x-0.5) <= 0.05 && y >= 0.27 && y <= 0.56) || math.Hypot(x-0.5, y-0.68) <= 0.058
}

func dots(x, y float64) bool {
	for _, cx := range []float64{0.34, 0.5, 0.66} {
		if math.Hypot(x-cx, y-0.5) <= 0.055 {
			return true
		}
	}
	return false
}

// near says whether (x, y) is within r of the segment from a to b.
func near(x, y, ax, ay, bx, by, r float64) bool {
	vx, vy := bx-ax, by-ay
	t := ((x-ax)*vx + (y-ay)*vy) / (vx*vx + vy*vy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(x-(ax+t*vx), y-(ay+t*vy)) <= r
}
