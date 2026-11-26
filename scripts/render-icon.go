//go:build ignore

// Command render-icon prepares the supplied icon artwork and writes the
// platform files listed in docs/DESIGN.md. Run from the module root:
//
//	go run scripts/render-icon.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/png"
	"math"
	"os"
	"strings"

	"golang.org/x/image/draw"
)

const sourcePath = "resources/icon-source.png"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	source, err := loadSource(sourcePath)
	if err != nil {
		return err
	}
	png1024 := resize(source, 1024)
	png512 := resize(source, 512)
	png256 := resize(source, 256)
	png128 := resize(source, 128)
	png64 := resize(source, 64)
	png48 := resize(source, 48)
	png32 := resize(source, 32)
	png24 := resize(source, 24)
	png16 := resize(source, 16)
	lightSource := recolorLight(source)
	lightBrand := resize(lightSource, 256)

	if err := os.MkdirAll("resources/icons", 0o755); err != nil {
		return err
	}
	svg, err := vectorSVG(source)
	if err != nil {
		return err
	}
	for _, path := range []string{"resources/icon.svg", "resources/icons/tako.svg"} {
		if err := os.WriteFile(path, []byte(svg), 0o644); err != nil {
			return err
		}
	}
	for path, img := range map[string]image.Image{
		"resources/icon.png":                 png1024,
		"resources/icons/icon-16.png":        png16,
		"resources/icons/icon-32.png":        png32,
		"resources/icons/icon-256.png":       png256,
		"resources/icons/icon-512.png":       png512,
		"internal/ui/assets/brand.png":       png256,
		"internal/ui/assets/brand-light.png": lightBrand,
	} {
		if err := writePNG(path, img); err != nil {
			return err
		}
	}

	icns, err := buildICNS(map[string]image.Image{
		"ic11": png32,
		"ic12": png64,
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
		{size: 16, img: png16},
		{size: 24, img: png24},
		{size: 32, img: png32},
		{size: 48, img: png48},
		{size: 256, img: png256},
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

func recolorLight(src image.Image) *image.NRGBA {
	bounds := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	lightPalette := [...]color.NRGBA{
		{R: 240, G: 241, B: 244, A: 255},
		{R: 32, G: 34, B: 40, A: 255},
		{R: 177, G: 181, B: 191, A: 255},
		{R: 17, G: 139, B: 108, A: 255},
	}
	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			c := color.NRGBAModel.Convert(src.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			if c.A == 0 {
				continue
			}
			out.SetNRGBA(x, y, lightPalette[nearestColor(c)-1])
		}
	}
	return out
}

func loadSource(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open icon source: %w", err)
	}
	defer f.Close()

	source, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode icon source: %w", err)
	}
	bounds := source.Bounds()
	img := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	imagedraw.Draw(img, img.Bounds(), source, bounds.Min, imagedraw.Src)
	removeEdgeBackground(img)
	return cropOpaque(img), nil
}

// removeEdgeBackground makes only the white background connected to an edge
// transparent, preserving the white octopus inside the black tile.
func removeEdgeBackground(img *image.NRGBA) {
	bounds := img.Bounds()
	visited := make([]bool, bounds.Dx()*bounds.Dy())
	queue := make([]image.Point, 0, bounds.Dx()+bounds.Dy())
	add := func(p image.Point) {
		if !p.In(bounds) {
			return
		}
		i := p.Y*bounds.Dx() + p.X
		if visited[i] || !isBackground(img.NRGBAAt(p.X, p.Y)) {
			return
		}
		visited[i] = true
		queue = append(queue, p)
	}
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		add(image.Point{X: x, Y: bounds.Min.Y})
		add(image.Point{X: x, Y: bounds.Max.Y - 1})
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		add(image.Point{X: bounds.Min.X, Y: y})
		add(image.Point{X: bounds.Max.X - 1, Y: y})
	}
	for head := 0; head < len(queue); head++ {
		p := queue[head]
		img.SetNRGBA(p.X, p.Y, color.NRGBA{})
		add(image.Point{X: p.X - 1, Y: p.Y})
		add(image.Point{X: p.X + 1, Y: p.Y})
		add(image.Point{X: p.X, Y: p.Y - 1})
		add(image.Point{X: p.X, Y: p.Y + 1})
	}
}

func isBackground(c color.NRGBA) bool {
	return c.A != 0 && c.R >= 220 && c.G >= 220 && c.B >= 220
}

func cropOpaque(img *image.NRGBA) *image.NRGBA {
	bounds := img.Bounds()
	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if img.NRGBAAt(x, y).A == 0 {
				continue
			}
			minX = min(minX, x)
			minY = min(minY, y)
			maxX = max(maxX, x+1)
			maxY = max(maxY, y+1)
		}
	}
	if minX >= maxX || minY >= maxY {
		return img
	}
	width, height := maxX-minX, maxY-minY
	size := max(width, height)
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	offsetX := (size - width) / 2
	offsetY := (size - height) / 2
	imagedraw.Draw(out, image.Rect(offsetX, offsetY, offsetX+width, offsetY+height), img, image.Point{X: minX, Y: minY}, imagedraw.Src)
	return out
}

func resize(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

type point struct{ x, y int }

type edge struct {
	from  point
	to    point
	dir   int
	class uint8
}

var iconPalette = []color.NRGBA{
	{R: 16, G: 17, B: 21, A: 255},
	{R: 255, G: 253, B: 249, A: 255},
	{R: 83, G: 85, B: 96, A: 255},
	{R: 66, G: 230, B: 184, A: 255},
}

func vectorSVG(img image.Image) (string, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	labels := make([]uint8, width*height)
	for y := range height {
		for x := range width {
			c := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			if c.A != 0 {
				labels[y*width+x] = nearestColor(c)
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d">`, width, height)
	for class, fill := range iconPalette {
		loops := contours(labels, width, height, uint8(class+1))
		if len(loops) == 0 {
			continue
		}
		fmt.Fprintf(&b, `<path fill="#%02X%02X%02X" fill-rule="evenodd" d="`, fill.R, fill.G, fill.B)
		for _, loop := range loops {
			loop = simplifyClosed(loop, 1.2)
			if len(loop) < 3 {
				continue
			}
			fmt.Fprintf(&b, "M%d %d", loop[0].x, loop[0].y)
			for _, p := range loop[1:] {
				fmt.Fprintf(&b, "L%d %d", p.x, p.y)
			}
			b.WriteByte('Z')
		}
		b.WriteString(`"/>`)
	}
	b.WriteString(`</svg>`)
	return b.String(), nil
}

func nearestColor(c color.NRGBA) uint8 {
	best, bestDistance := uint8(1), math.MaxFloat64
	for i, candidate := range iconPalette {
		dr := float64(c.R) - float64(candidate.R)
		dg := float64(c.G) - float64(candidate.G)
		db := float64(c.B) - float64(candidate.B)
		distance := dr*dr + dg*dg + db*db
		if distance < bestDistance {
			best, bestDistance = uint8(i+1), distance
		}
	}
	return best
}

func contours(labels []uint8, width, height int, target uint8) [][]point {
	labelAt := func(x, y int) uint8 {
		if x < 0 || y < 0 || x >= width || y >= height {
			return 0
		}
		return labels[y*width+x]
	}
	edges := make([]edge, 0, width*2)
	for y := range height {
		for x := range width {
			if labelAt(x, y) != target {
				continue
			}
			if labelAt(x, y-1) != target {
				edges = append(edges, edge{point{x, y}, point{x + 1, y}, 0, target})
			}
			if labelAt(x+1, y) != target {
				edges = append(edges, edge{point{x + 1, y}, point{x + 1, y + 1}, 1, target})
			}
			if labelAt(x, y+1) != target {
				edges = append(edges, edge{point{x + 1, y + 1}, point{x, y + 1}, 2, target})
			}
			if labelAt(x-1, y) != target {
				edges = append(edges, edge{point{x, y + 1}, point{x, y}, 3, target})
			}
		}
	}
	starts := make(map[point][]int, len(edges))
	for i, e := range edges {
		starts[e.from] = append(starts[e.from], i)
	}
	used := make([]bool, len(edges))
	loops := make([][]point, 0)
	for start := range edges {
		if used[start] {
			continue
		}
		loop := []point{edges[start].from}
		current := start
		for {
			if used[current] {
				break
			}
			used[current] = true
			e := edges[current]
			loop = append(loop, e.to)
			if e.to == loop[0] {
				loops = append(loops, loop[:len(loop)-1])
				break
			}
			current = nextEdge(starts[e.to], edges, used, e.dir)
			if current < 0 {
				break
			}
		}
	}
	return loops
}

func nextEdge(candidates []int, edges []edge, used []bool, dir int) int {
	for _, turn := range [...]int{1, 0, 3, 2} {
		for _, candidate := range candidates {
			if !used[candidate] && (edges[candidate].dir-dir+4)%4 == turn {
				return candidate
			}
		}
	}
	return -1
}

func simplifyClosed(points []point, tolerance float64) []point {
	if len(points) < 4 {
		return points
	}
	open := append(append([]point{}, points...), points[0])
	keep := make([]bool, len(open))
	keep[0], keep[len(keep)-1] = true, true
	simplifyRange(open, 0, len(open)-1, tolerance*tolerance, keep)
	out := make([]point, 0, len(open))
	for i, p := range open[:len(open)-1] {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

func simplifyRange(points []point, first, last int, tolerance2 float64, keep []bool) {
	if last-first < 2 {
		return
	}
	maxDistance, index := 0.0, 0
	for i := first + 1; i < last; i++ {
		distance := segmentDistanceSquared(points[i], points[first], points[last])
		if distance > maxDistance {
			maxDistance, index = distance, i
		}
	}
	if maxDistance <= tolerance2 {
		return
	}
	keep[index] = true
	simplifyRange(points, first, index, tolerance2, keep)
	simplifyRange(points, index, last, tolerance2, keep)
}

func segmentDistanceSquared(p, a, b point) float64 {
	dx, dy := float64(b.x-a.x), float64(b.y-a.y)
	if dx == 0 && dy == 0 {
		dx, dy = float64(p.x-a.x), float64(p.y-a.y)
		return dx*dx + dy*dy
	}
	t := (float64(p.x-a.x)*dx + float64(p.y-a.y)*dy) / (dx*dx + dy*dy)
	t = max(0, min(1, t))
	dx = float64(p.x-a.x) - t*dx
	dy = float64(p.y-a.y) - t*dy
	return dx*dx + dy*dy
}

func writePNG(path string, img image.Image) error {
	data, err := pngBytes(img)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func pngBytes(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildICNS(byType map[string]image.Image) ([]byte, error) {
	order := []string{"ic11", "ic12", "ic07", "ic08", "ic13", "ic09", "ic14", "ic10"}
	var body bytes.Buffer
	for _, kind := range order {
		img := byType[kind]
		if img == nil {
			return nil, fmt.Errorf("icns missing %s", kind)
		}
		data, err := pngBytes(img)
		if err != nil {
			return nil, err
		}
		body.WriteString(kind)
		_ = binary.Write(&body, binary.BigEndian, uint32(len(data)+8))
		body.Write(data)
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
		data, err := pngBytes(im.img)
		if err != nil {
			return nil, err
		}
		payloads[i] = data
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
	for _, data := range payloads {
		out.Write(data)
	}
	return out.Bytes(), nil
}
