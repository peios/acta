// Command icongen generates Acta's home-screen / favicon assets: a white
// line-art "A" on a blue gradient tile. The mark is drawn from pure geometry
// (distance-to-segment strokes) and supersampled, so it stays crisp at every
// size with no font or external raster tooling (the box has neither ImageMagick
// nor cairo). It is not part of the server build — run it by hand from the
// module root when the mark changes:
//
//	go run ./internal/web/icongen
//
// It writes favicon-32 / icon-180 / icon-192 / icon-512 (PNG) and icon.svg into
// internal/web/static, which //go:embed picks up on the next build.
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

type pt struct{ x, y float64 }

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// distSeg is the distance from p to segment ab. Strokes built from it get
// rounded caps for free, which is the look we want.
func distSeg(p, a, b pt) float64 {
	vx, vy := b.x-a.x, b.y-a.y
	wx, wy := p.x-a.x, p.y-a.y
	c1 := vx*wx + vy*wy
	if c1 <= 0 {
		return math.Hypot(p.x-a.x, p.y-a.y)
	}
	c2 := vx*vx + vy*vy
	if c2 <= c1 {
		return math.Hypot(p.x-b.x, p.y-b.y)
	}
	t := c1 / c2
	return math.Hypot(p.x-(a.x+t*vx), p.y-(a.y+t*vy))
}

// The mark, in a unit square. An "A": two legs from the apex plus a crossbar.
var (
	apex   = pt{0.500, 0.205}
	legL   = pt{0.275, 0.800}
	legR   = pt{0.725, 0.800}
	stroke = 0.052 // half-width
)

func crossbar() (l, r pt) {
	const cy = 0.585
	t := (cy - apex.y) / (legL.y - apex.y)
	return pt{lerp(apex.x, legL.x, t), cy}, pt{lerp(apex.x, legR.x, t), cy}
}

func onMark(p pt) bool {
	cl, cr := crossbar()
	d := distSeg(p, apex, legL)
	if x := distSeg(p, apex, legR); x < d {
		d = x
	}
	if x := distSeg(p, cl, cr); x < d {
		d = x
	}
	return d <= stroke
}

// bg is the tile's vertical gradient: accent blue (#4d7cfe) down to a deeper
// blue (#3554d8), matching the app's --accent.
func bg(v float64) (r, g, b float64) {
	return lerp(77, 53, v), lerp(124, 84, v), lerp(254, 216, v)
}

func render(size int) *image.RGBA {
	const ss = 4 // supersample factor for clean edges
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	inv := 1.0 / float64(size*ss)
	for y := range size {
		for x := range size {
			var rs, gs, bs float64
			for sy := range ss {
				for sx := range ss {
					p := pt{(float64(x*ss+sx) + 0.5) * inv, (float64(y*ss+sy) + 0.5) * inv}
					r, g, b := bg(p.y)
					if onMark(p) {
						r, g, b = 255, 255, 255
					}
					rs, gs, bs = rs+r, gs+g, bs+b
				}
			}
			n := float64(ss * ss)
			img.Set(x, y, color.RGBA{
				uint8(rs/n + 0.5), uint8(gs/n + 0.5), uint8(bs/n + 0.5), 255,
			})
		}
	}
	return img
}

// A hand-written SVG twin of the same mark, for a tab favicon that stays sharp
// at any size. The geometry mirrors the constants above.
const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100" role="img" aria-label="Acta">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#4d7cfe"/>
      <stop offset="1" stop-color="#3554d8"/>
    </linearGradient>
  </defs>
  <rect width="100" height="100" fill="url(#g)"/>
  <path d="M50 20.5 L27.5 80 M50 20.5 L72.5 80 M35.6 58.5 L64.4 58.5"
        fill="none" stroke="#fff" stroke-width="5.2"
        stroke-linecap="round" stroke-linejoin="round"/>
</svg>
`

func main() {
	dir := "internal/web/static"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	for _, ic := range []struct {
		name string
		size int
	}{
		{"favicon-32.png", 32},
		{"icon-180.png", 180},
		{"icon-192.png", 192},
		{"icon-512.png", 512},
	} {
		f, err := os.Create(filepath.Join(dir, ic.name))
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, render(ic.size)); err != nil {
			panic(err)
		}
		if err := f.Close(); err != nil {
			panic(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.svg"), []byte(svg), 0o644); err != nil {
		panic(err)
	}
}
