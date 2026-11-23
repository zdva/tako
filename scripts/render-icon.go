//go:build ignore

// Command render-icon draws the Tako app icon from one geometric master and
// writes the platform files DESIGN.md §4.3 lists. Run from the module root:
//
//	go run scripts/render-icon.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	cream = "#FFF5F0"
	eye   = "#7A1E18"
	hi    = "#FFE8E0"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	full := masterSVG(true)
	plain := masterSVG(false)
	if err := os.MkdirAll("resources/icons", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile("resources/icon.svg", []byte(full), 0o644); err != nil {
		return err
	}
	png1024, err := raster(full, 1024)
	if err != nil {
		return err
	}
	png256, err := raster(full, 256)
	if err != nil {
		return err
	}
	png512, err := raster(full, 512)
	if err != nil {
		return err
	}
	png128, err := raster(plain, 128)
	if err != nil {
		return err
	}
	png64, err := raster(plain, 64)
	if err != nil {
		return err
	}
	png48, err := raster(plain, 48)
	if err != nil {
		return err
	}
	png32, err := raster(plain, 32)
	if err != nil {
		return err
	}
	// 16 and 24 are drawn on the pixel grid. 32 and up are the master,
	// which still reads once the arms are this thick.
	png16 := pixel16()
	png24 := scaleBox(png16, 24)
	if err := writePNG("resources/icon.png", png1024); err != nil {
		return err
	}
	if err := writePNG("resources/icons/icon-16.png", png16); err != nil {
		return err
	}
	if err := writePNG("resources/icons/icon-32.png", png32); err != nil {
		return err
	}
	if err := writePNG("resources/icons/icon-256.png", png256); err != nil {
		return err
	}
	if err := writePNG("resources/icons/icon-512.png", png512); err != nil {
		return err
	}
	if err := writePNG("internal/ui/assets/brand.png", png256); err != nil {
		return err
	}
	icns, err := buildICNS(map[string]image.Image{
		"ic11": scaleNN(png16, 2), // 16 pt @2x, the simplified masses, crisp
		"ic12": png64,             // 32 pt @2x, the master
		"ic07": png128,
		"ic08": png256,
		"ic13": png256,
		"ic09": png512,
		"ic14": png512,
		"ic10": png1024,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile("resources/icons/tako.icns", icns, 0o644); err != nil {
		return err
	}
	ico, err := buildICO([]icoImg{
		{16, png16},
		{24, png24},
		{32, png32},
		{48, png48},
		{256, png256},
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile("resources/icons/tako.ico", ico, 0o644); err != nil {
		return err
	}
	fmt.Println("wrote resources/icon.svg, resources/icon.png, resources/icons/*, internal/ui/assets/brand.png")
	return nil
}

// masterSVG is the 1024 master. highlight is the tiny eye glint, drawn only
// at 256 px and above.
func masterSVG(highlight bool) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">`)
	b.WriteString(`<defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1">`)
	b.WriteString(`<stop offset="0" stop-color="#FF8A7A"/><stop offset="1" stop-color="#E34A40"/>`)
	b.WriteString(`</linearGradient><clipPath id="sq"><path d="`)
	b.WriteString(squircle(1024, 5))
	b.WriteString(`"/></clipPath></defs>`)
	b.WriteString(`<path d="`)
	b.WriteString(squircle(1024, 5))
	b.WriteString(`" fill="url(#g)"/>`)
	b.WriteString(`<g clip-path="url(#sq)" fill="` + cream + `" stroke="` + cream + `" stroke-linecap="round" stroke-linejoin="round">`)
	for _, t := range tentacles() {
		fmt.Fprintf(&b, `<path d="%s" fill="none" stroke-width="%g"/>`, t.d, t.w)
	}
	b.WriteString(`<circle cx="512" cy="392" r="176"/>`)
	b.WriteString(`</g>`)
	b.WriteString(`<g clip-path="url(#sq)">`)
	b.WriteString(`<circle cx="446" cy="378" r="34" fill="` + eye + `"/>`)
	b.WriteString(`<circle cx="578" cy="378" r="34" fill="` + eye + `"/>`)
	if highlight {
		b.WriteString(`<circle cx="434" cy="364" r="8" fill="` + hi + `"/>`)
		b.WriteString(`<circle cx="566" cy="364" r="8" fill="` + hi + `"/>`)
	}
	b.WriteString(`</g></svg>`)
	return b.String()
}

type tentacle struct {
	d string
	w float64
}

// tentacles are eight symmetric rounded arms. They are stroked under the
// head so the body reads as one solid shape.
func tentacles() []tentacle {
	type arm struct {
		x0, y0, c1x, c1y, c2x, c2y, x1, y1, w float64
	}
	// Left half, from the center outward. The right half mirrors on x=512.
	left := []arm{
		{478, 530, 468, 680, 470, 800, 462, 900, 86},
		{430, 510, 390, 660, 400, 820, 388, 910, 82},
		{372, 478, 300, 600, 250, 740, 268, 860, 78},
		{328, 440, 200, 500, 150, 660, 210, 790, 74},
	}
	var out []tentacle
	add := func(a arm, x func(float64) float64) {
		out = append(out, tentacle{
			d: fmt.Sprintf("M%.1f %.1f C%.1f %.1f %.1f %.1f %.1f %.1f",
				x(a.x0), a.y0, x(a.c1x), a.c1y, x(a.c2x), a.c2y, x(a.x1), a.y1),
			w: a.w,
		})
	}
	for _, a := range left {
		add(a, func(x float64) float64 { return x })
		add(a, func(x float64) float64 { return 1024 - x })
	}
	return out
}

// squircle is a superellipse of exponent n filling a size×size box, as a
// closed SVG path. n≈5 is the continuous corner of a squircle, not a circle
// and not a rounded square.
func squircle(size, n float64) string {
	const steps = 48
	var b strings.Builder
	p := 2 / n
	for i := range steps * 4 {
		ang := float64(i) / float64(steps*4) * 2 * math.Pi
		c, s := math.Cos(ang), math.Sin(ang)
		x := math.Copysign(math.Pow(math.Abs(c), p), c)
		y := math.Copysign(math.Pow(math.Abs(s), p), s)
		px, py := (x+1)*0.5*size, (y+1)*0.5*size
		if i == 0 {
			fmt.Fprintf(&b, "M%.2f %.2f", px, py)
			continue
		}
		fmt.Fprintf(&b, "L%.2f %.2f", px, py)
	}
	b.WriteByte('Z')
	return b.String()
}

// pixel16 is the 16 px octopus: head, body and four tentacle masses, on the
// coral squircle. It is the same figure as the master, with the arms collapsed
// into the masses that still read at this size.
func pixel16() *image.NRGBA {
	// 16 rows. C coral, W cream, E eye, . outside the squircle.
	// Traced from the master: a round head, two eyes, and the arm masses
	// pulled apart so the coral shows between them. A plain downsample of
	// the master closes those gaps and becomes one blob.
	rows := []string{
		"..CCCCCCCCCCCC..",
		".CCCCCCCCCCCCCC.",
		"CCCCCCWWWWCCCCCC",
		"CCCWWWWWWWWWWCCC",
		"CCWWWEEWWEEWWWCC",
		"CCWWWWWWWWWWWWCC",
		"CCCWWWWWWWWWWCCC",
		"CCCCWWWWWWWWCCCC",
		"CCWWCWWWWWWCWWCC",
		"CWWCCWWWWWWCCWWC",
		"CWWCCWWCCWWCCWWC",
		"CWWCCWWCCWWCCWWC",
		"CCWCCWWCCWWCCWCC",
		"CCCCCWWCCWWCCCCC",
		"....CCCCCCCC....",
		"......CCCC......",
	}
	// The grid above is a sketch; the renderer below uses an explicit mask
	// so the arms stay separated. Rebuild from a cleaner drawing.
	return draw16(rows)
}

func draw16(rows []string) *image.NRGBA {
	const n = 16
	for i, row := range rows {
		if len(row) != n {
			panic(fmt.Sprintf("pixel16 row %d has %d columns", i, len(row)))
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	coralTop := color.NRGBA{R: 0xFF, G: 0x8A, B: 0x7A, A: 0xFF}
	coralBot := color.NRGBA{R: 0xE3, G: 0x4A, B: 0x40, A: 0xFF}
	creamC := color.NRGBA{R: 0xFF, G: 0xF5, B: 0xF0, A: 0xFF}
	eyeC := color.NRGBA{R: 0x7A, G: 0x1E, B: 0x18, A: 0xFF}
	for y := range n {
		t := float64(y) / float64(n-1)
		bg := mix(coralTop, coralBot, t)
		row := rows[y]
		for x := range n {
			switch row[x] {
			case '.':
				// outside
			case 'C', 'c':
				img.SetNRGBA(x, y, bg)
			case 'W':
				img.SetNRGBA(x, y, creamC)
			case 'E':
				img.SetNRGBA(x, y, eyeC)
			default:
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	return img
}

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	lerp := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x)*(1-t) + float64(y)*t))
	}
	return color.NRGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: 0xFF}
}

func raster(svg string, size int) (*image.RGBA, error) {
	s, err := ui.ParseSVG([]byte(svg))
	if err != nil {
		return nil, err
	}
	return s.Image(size, size, nil), nil
}

// downNN samples src down to size×size by nearest neighbor, so a thick
// master stays crisp instead of blurring into one mass.
func downNN(src image.Image, size int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			sx := b.Min.X + (x*b.Dx()+b.Dx()/2)/size
			sy := b.Min.Y + (y*b.Dy()+b.Dy()/2)/size
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			if sy >= b.Max.Y {
				sy = b.Max.Y - 1
			}
			dst.SetNRGBA(x, y, color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA))
		}
	}
	return dst
}

func scaleNN(src image.Image, factor int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx()*factor, b.Dy()*factor))
	for y := range b.Dy() {
		for x := range b.Dx() {
			c := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			for dy := range factor {
				for dx := range factor {
					dst.SetNRGBA(x*factor+dx, y*factor+dy, c)
				}
			}
		}
	}
	return dst
}

// scaleBox fits the 16 px design into size×size with nearest-neighbor, centered
// on a coral field so 24 px still shows the same masses.
func scaleBox(src image.Image, size int) *image.NRGBA {
	// 16 → 24 is 1.5. Draw each source pixel as a 1 or 2 device pixel.
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			sx := b.Min.X + x*b.Dx()/size
			sy := b.Min.Y + y*b.Dy()/size
			dst.SetNRGBA(x, y, color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA))
		}
	}
	return dst
}

func writePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func pngBytes(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildICNS writes an icns whose payloads are PNG, the sizes macOS reads as
// PNG (ic07 and up, including the @2x 16 pt and 32 pt slots). icp4 and icp5
// are omitted: IconServices treats those slots as legacy ARGB, so a PNG there
// draws as noise. The 16 px simplified art is what ic11 scales from.
func buildICNS(byType map[string]image.Image) ([]byte, error) {
	order := []string{"ic11", "ic12", "ic07", "ic08", "ic13", "ic09", "ic14", "ic10"}
	var body bytes.Buffer
	for _, kind := range order {
		img := byType[kind]
		if img == nil {
			return nil, fmt.Errorf("icns missing %s", kind)
		}
		p, err := pngBytes(img)
		if err != nil {
			return nil, err
		}
		body.WriteString(kind)
		_ = binary.Write(&body, binary.BigEndian, uint32(len(p)+8))
		body.Write(p)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(body.Len()+8))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

type icoImg struct {
	size int
	img  image.Image
}

func buildICO(images []icoImg) ([]byte, error) {
	payloads := make([][]byte, len(images))
	for i, im := range images {
		p, err := pngBytes(im.img)
		if err != nil {
			return nil, err
		}
		payloads[i] = p
	}
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(images))})
	offset := 6 + 16*len(images)
	for i, im := range images {
		dim := byte(im.size)
		if im.size >= 256 {
			dim = 0
		}
		out.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&out, binary.LittleEndian, [2]uint16{1, 32})
		_ = binary.Write(&out, binary.LittleEndian, [2]uint32{uint32(len(payloads[i])), uint32(offset)})
		offset += len(payloads[i])
	}
	for _, p := range payloads {
		out.Write(p)
	}
	return out.Bytes(), nil
}

func contact(imgs []image.Image) *image.NRGBA {
	gap := 16
	h := 0
	w := gap
	for _, im := range imgs {
		b := im.Bounds()
		w += b.Dx() + gap
		if b.Dy() > h {
			h = b.Dy()
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h+gap*2))
	for i := range dst.Pix {
		dst.Pix[i] = 0x22
		if i%4 == 3 {
			dst.Pix[i] = 0xFF
		}
	}
	x := gap
	for _, im := range imgs {
		b := im.Bounds()
		y := gap + (h-b.Dy())/2
		for yy := range b.Dy() {
			for xx := range b.Dx() {
				dst.Set(x+xx, y+yy, im.At(b.Min.X+xx, b.Min.Y+yy))
			}
		}
		x += b.Dx() + gap
	}
	return dst
}
