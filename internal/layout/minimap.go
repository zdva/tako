package layout

import "slices"

// HitSquare returns the index of the cell under the point, or -1.
func HitSquare(cells []Rect, x, y float32) int {
	return slices.IndexFunc(cells, func(c Rect) bool { return c.Contains(x, y) })
}

// SelectBox returns the indexes of the cells that a rubber-band rectangle
// touches, in ascending order. The rectangle may have a negative size.
func SelectBox(cells []Rect, box Rect) []int {
	box = box.Normalize()
	picked := []int{}
	for i, c := range cells {
		if c.Intersects(box) {
			picked = append(picked, i)
		}
	}
	return picked
}

// NearestSlot returns the index of the cell whose center is closest to the
// point. A dragged square snaps to it. It returns -1 when there are no cells.
func NearestSlot(cells []Rect, x, y float32) int {
	best, bestDist := -1, float32(0)
	for i, c := range cells {
		dx, dy := c.X+c.W/2-x, c.Y+c.H/2-y
		d := dx*dx + dy*dy
		if best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// Move returns a copy of s with the element at from taken out and inserted
// so that it ends up at index to. Out-of-range indexes return a plain copy.
func Move[T any](s []T, from, to int) []T {
	out := slices.Clone(s)
	if from < 0 || from >= len(s) || to < 0 || to >= len(s) || from == to {
		return out
	}
	item := out[from]
	out = slices.Delete(out, from, from+1)
	return slices.Insert(out, to, item)
}

// Corner is one of the four corners a floating panel snaps to.
type Corner int

// The corners of the viewport.
const (
	TopLeft Corner = iota
	TopRight
	BottomLeft
	BottomRight
)

// CornerOrigins returns the top-left offsets of a panel of the given size
// sitting in each corner of the viewport, margin away from the edges.
func CornerOrigins(viewport, panel Size, margin float32) [4]Rect {
	left, top := margin, margin
	right := max(viewport.W-panel.W-margin, left)
	bottom := max(viewport.H-panel.H-margin, top)
	return [4]Rect{
		TopLeft:     {X: left, Y: top, W: panel.W, H: panel.H},
		TopRight:    {X: right, Y: top, W: panel.W, H: panel.H},
		BottomLeft:  {X: left, Y: bottom, W: panel.W, H: panel.H},
		BottomRight: {X: right, Y: bottom, W: panel.W, H: panel.H},
	}
}

// SnapEdges pulls v onto the nearest of targets when it is within
// threshold of one, and returns it unchanged otherwise.
func SnapEdges(v, threshold float32, targets ...float32) float32 {
	best, bestDist := v, threshold
	for _, t := range targets {
		d := t - v
		if d < 0 {
			d = -d
		}
		if d <= bestDist {
			best, bestDist = t, d
		}
	}
	return best
}
