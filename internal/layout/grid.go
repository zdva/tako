// Package layout computes Tako's adaptive terminal grid and the minimap
// geometry (hit testing, rubber-band selection, reordering). It is pure
// arithmetic with no UI dependency, so it is unit-tested without a window.
package layout

// Rect is an axis-aligned rectangle in DIPs.
type Rect struct {
	X, Y, W, H float32
}

// Right returns the x coordinate of the right edge.
func (r Rect) Right() float32 { return r.X + r.W }

// Bottom returns the y coordinate of the bottom edge.
func (r Rect) Bottom() float32 { return r.Y + r.H }

// Contains reports whether the point lies inside r.
func (r Rect) Contains(x, y float32) bool {
	return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom()
}

// Intersects reports whether r and o overlap.
func (r Rect) Intersects(o Rect) bool {
	return r.X < o.Right() && o.X < r.Right() && r.Y < o.Bottom() && o.Y < r.Bottom()
}

// Normalize returns r with a non-negative width and height, for rectangles
// dragged from bottom-right to top-left.
func (r Rect) Normalize() Rect {
	if r.W < 0 {
		r.X, r.W = r.X+r.W, -r.W
	}
	if r.H < 0 {
		r.Y, r.H = r.Y+r.H, -r.H
	}
	return r
}

// Size is a width and a height in DIPs.
type Size struct {
	W, H float32
}

// Dims returns the number of columns and rows of the grid for n blocks:
// 1 fills the area, 2 are halves, 3-4 are 2x2, 5-6 are 3x2, 7-9 are 3x3,
// and beyond that the grid keeps 3 columns and grows downward.
func Dims(n int) (cols, rows int) {
	switch {
	case n <= 1:
		return 1, 1
	case n == 2:
		return 2, 1
	case n <= 4:
		return 2, 2
	case n <= 6:
		return 3, 2
	}
	return 3, (n + 2) / 3
}

// Grid is the placement of n blocks inside a viewport.
type Grid struct {
	// Cells holds one rectangle per block, in block order, in the
	// coordinates of the scrolling content.
	Cells []Rect
	// Content is the size of the scrolling content: wider or taller than
	// the viewport only when the cells hit their minimum size.
	Content Size
	// Cols and Rows are the grid dimensions.
	Cols, Rows int
}

// Compute places n blocks in a viewport. Cells share the viewport equally
// but never shrink below min: when the minimum height does not fit, the
// content grows taller than the viewport and the caller scrolls it
// vertically. gap is the space between cells.
func Compute(n int, viewport, minCell Size, gap float32) Grid {
	cols, _ := Dims(n)
	// Never overflow horizontally: drop columns until the minimum width
	// fits, and let the rows grow (vertical scrolling) instead.
	fit := max(int((viewport.W+gap)/(minCell.W+gap)), 1)
	cols = min(cols, fit)
	rows := max((n+cols-1)/cols, 1)
	g := Grid{Cols: cols, Rows: rows, Cells: make([]Rect, 0, n)}
	if n == 0 {
		g.Content = viewport
		return g
	}
	w := max((viewport.W-gap*float32(cols-1))/float32(cols), minCell.W)
	h := max((viewport.H-gap*float32(rows-1))/float32(rows), minCell.H)
	for i := range n {
		col, row := i%cols, i/cols
		g.Cells = append(g.Cells, Rect{
			X: float32(col) * (w + gap),
			Y: float32(row) * (h + gap),
			W: w,
			H: h,
		})
	}
	g.Content = Size{
		W: float32(cols)*w + gap*float32(cols-1),
		H: float32(rows)*h + gap*float32(rows-1),
	}
	return g
}
