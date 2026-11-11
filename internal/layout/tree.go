package layout

import (
	"errors"
	"fmt"
	"slices"
)

// Axis tells how a split places its two sides.
type Axis string

// The two axes.
const (
	// Cols puts the sides next to each other: A on the left, B on the right.
	Cols Axis = "cols"
	// Rows stacks the sides: A on top, B below.
	Rows Axis = "rows"
)

// Edge is a side of a leaf, where another leaf can be inserted.
type Edge int

// The four edges.
const (
	Left Edge = iota
	Right
	Top
	Bottom
)

// Node is a leaf (a block ID) or a split of two nodes. A split gives side A
// the fraction Ratio of the room that is left once the gap between the sides
// is taken out.
type Node struct {
	ID    string  `json:"id,omitempty"`
	Axis  Axis    `json:"axis,omitempty"`
	Ratio float32 `json:"ratio,omitzero"`
	A     *Node   `json:"a,omitzero"`
	B     *Node   `json:"b,omitzero"`
}

func (n *Node) leaf() bool { return n.A == nil }

// Tree is a manual layout: a binary tree of splits whose leaves are blocks.
// A Tree is a value; every operation returns a new Tree and leaves the
// receiver alone, so a drag can preview a change without committing it.
type Tree struct {
	Root *Node `json:"root,omitzero"`
}

// Leaf returns a tree of one block.
func Leaf(id string) Tree { return Tree{Root: &Node{ID: id}} }

// Empty reports whether the tree has no blocks.
func (t Tree) Empty() bool { return t.Root == nil }

// Clone returns a deep copy.
func (t Tree) Clone() Tree { return Tree{Root: cloneNode(t.Root)} }

func cloneNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	c := *n
	c.A, c.B = cloneNode(n.A), cloneNode(n.B)
	return &c
}

// IDs returns the blocks from left to right and top to bottom.
func (t Tree) IDs() []string {
	var out []string
	var walk func(*Node)
	walk = func(n *Node) {
		switch {
		case n == nil:
		case n.leaf():
			out = append(out, n.ID)
		default:
			walk(n.A)
			walk(n.B)
		}
	}
	walk(t.Root)
	return out
}

// Has reports whether the tree holds the block.
func (t Tree) Has(id string) bool { return slices.Contains(t.IDs(), id) }

// ErrInvalidTree means a saved tree is malformed.
var ErrInvalidTree = errors.New("invalid layout tree")

// Validate checks a tree that came from outside (a saved file): every split
// has two sides and a ratio strictly between 0 and 1, every leaf an ID, and no
// block appears twice.
func (t Tree) Validate() error {
	seen := map[string]bool{}
	var check func(*Node, int) error
	check = func(n *Node, depth int) error {
		switch {
		case n == nil:
			return fmt.Errorf("%w: missing node", ErrInvalidTree)
		case depth > 64:
			return fmt.Errorf("%w: too deep", ErrInvalidTree)
		case n.leaf():
			if n.ID == "" || n.B != nil {
				return fmt.Errorf("%w: bad leaf", ErrInvalidTree)
			}
			if seen[n.ID] {
				return fmt.Errorf("%w: %s appears twice", ErrInvalidTree, n.ID)
			}
			seen[n.ID] = true
			return nil
		}
		if n.ID != "" || n.B == nil || (n.Axis != Cols && n.Axis != Rows) || !(n.Ratio > 0 && n.Ratio < 1) {
			return fmt.Errorf("%w: bad split", ErrInvalidTree)
		}
		if err := check(n.A, depth+1); err != nil {
			return err
		}
		return check(n.B, depth+1)
	}
	if t.Root == nil {
		return nil
	}
	return check(t.Root, 0)
}

// Cell is where one block goes.
type Cell struct {
	ID   string
	Rect Rect
}

// Divider is the gap between the two sides of a split, which the user drags.
type Divider struct {
	// Path names the split: "" is the root, then "A" or "B" for each step.
	Path string
	Axis Axis
	// Rect is the gap itself.
	Rect Rect
	// Span is the whole area of the split, both sides and the gap.
	Span Rect
}

// Placement is the geometry of a tree in a viewport.
type Placement struct {
	Cells    []Cell
	Dividers []Divider
}

// Rect returns the rectangle of a block, and whether it is in the placement.
func (p Placement) Rect(id string) (Rect, bool) {
	for _, c := range p.Cells {
		if c.ID == id {
			return c.Rect, true
		}
	}
	return Rect{}, false
}

// Place lays the tree out in a viewport whose origin is (0, 0). gap is the
// space between the two sides of every split.
func (t Tree) Place(viewport Size, gap float32) Placement {
	var p Placement
	place(&p, t.Root, Rect{W: viewport.W, H: viewport.H}, gap, "")
	return p
}

func place(p *Placement, n *Node, r Rect, gap float32, path string) {
	if n == nil {
		return
	}
	if n.leaf() {
		p.Cells = append(p.Cells, Cell{ID: n.ID, Rect: r})
		return
	}
	a, b, d := splitRect(r, n, gap)
	p.Dividers = append(p.Dividers, Divider{Path: path, Axis: n.Axis, Rect: d, Span: r})
	place(p, n.A, a, gap, path+"A")
	place(p, n.B, b, gap, path+"B")
}

// splitRect cuts r in two by n, with the gap between.
func splitRect(r Rect, n *Node, gap float32) (a, b, div Rect) {
	if n.Axis == Cols {
		gap = effGap(r.W, gap)
		usable := max(r.W-gap, 0)
		aw := usable * n.Ratio
		a = Rect{X: r.X, Y: r.Y, W: aw, H: r.H}
		div = Rect{X: r.X + aw, Y: r.Y, W: gap, H: r.H}
		b = Rect{X: r.X + aw + gap, Y: r.Y, W: usable - aw, H: r.H}
		return a, b, div
	}
	gap = effGap(r.H, gap)
	usable := max(r.H-gap, 0)
	ah := usable * n.Ratio
	a = Rect{X: r.X, Y: r.Y, W: r.W, H: ah}
	div = Rect{X: r.X, Y: r.Y + ah, W: r.W, H: gap}
	b = Rect{X: r.X, Y: r.Y + ah + gap, W: r.W, H: usable - ah}
	return a, b, div
}

// effGap is the gap a split really leaves: never more than a quarter of the
// room, so that a very small area still splits into two non-negative sides.
func effGap(length, gap float32) float32 { return min(gap, max(length, 0)/4) }

// node returns the node at a path, or nil.
func (t Tree) node(path string) *Node {
	n := t.Root
	for _, step := range path {
		if n == nil || n.leaf() {
			return nil
		}
		if step == 'A' {
			n = n.A
		} else {
			n = n.B
		}
	}
	return n
}

// span returns the rectangle the node at path occupies.
func (t Tree) span(path string, viewport Size, gap float32) (Rect, bool) {
	r := Rect{W: viewport.W, H: viewport.H}
	n := t.Root
	for _, step := range path {
		if n == nil || n.leaf() {
			return Rect{}, false
		}
		a, b, _ := splitRect(r, n, gap)
		if step == 'A' {
			n, r = n.A, a
		} else {
			n, r = n.B, b
		}
	}
	return r, n != nil
}

// minSize is the smallest room a subtree needs when each block needs min.
func minSize(n *Node, floor Size, gap float32) Size {
	if n == nil || n.leaf() {
		return floor
	}
	a, b := minSize(n.A, floor, gap), minSize(n.B, floor, gap)
	if n.Axis == Cols {
		return Size{W: a.W + gap + b.W, H: max(a.H, b.H)}
	}
	return Size{W: max(a.W, b.W), H: a.H + gap + b.H}
}

// snapRatios are the fractions a divider snaps to.
var snapRatios = [...]float32{1.0 / 3, 1.0 / 2, 2.0 / 3}

// SnapRatio pulls a split ratio onto 1/3, 1/2 or 2/3 when it is within
// threshold pixels of one, where usable is the length of the room being
// split. It reports whether it snapped.
func SnapRatio(ratio, usable, threshold float32) (float32, bool) {
	if usable <= 0 {
		return ratio, false
	}
	best, bestDist, hit := ratio, threshold, false
	for _, s := range snapRatios {
		d := (s - ratio) * usable
		if d < 0 {
			d = -d
		}
		if d <= bestDist {
			best, bestDist, hit = s, d, true
		}
	}
	return best, hit
}

// DragOptions tune [Tree.DragDivider].
type DragOptions struct {
	Viewport Size
	Gap      float32
	// Min is the smallest size of one block; a divider stops where either
	// side would get smaller.
	Min Size
	// Snap turns on snapping (the Alt key turns it off while dragging).
	Snap bool
	// Threshold is the snapping distance in pixels.
	Threshold float32
}

// Guide is the line shown while a divider snaps: its position along the
// divider's axis, in viewport coordinates. Valid is false when not snapped.
type Guide struct {
	Valid bool
	Axis  Axis
	// Pos is the x (Cols) or y (Rows) of the line.
	Pos float32
	// Span is the area of the split, to draw the line across.
	Span Rect
}

// DragDivider moves the divider of the split at path so that its center is at
// pos (a coordinate along the split's axis in viewport coordinates), snapping
// and clamping as o says. It returns the new tree and the guide to show.
func (t Tree) DragDivider(path string, pos float32, o DragOptions) (Tree, Guide) {
	n := t.node(path)
	span, ok := t.span(path, o.Viewport, o.Gap)
	if n == nil || n.leaf() || !ok {
		return t, Guide{}
	}
	start, length := span.X, span.W
	if n.Axis == Rows {
		start, length = span.Y, span.H
	}
	gap := effGap(length, o.Gap)
	usable := length - gap
	if usable <= 0 {
		return t, Guide{}
	}
	ratio := (pos - gap/2 - start) / usable
	snapped := false
	if o.Snap {
		ratio, snapped = SnapRatio(ratio, usable, o.Threshold)
	}
	lo, hi := ratioBounds(n, usable, o)
	if lo > hi { // not enough room for both minimums: keep the middle
		lo, hi = 0.5, 0.5
	}
	if clamped := min(max(ratio, lo), hi); clamped != ratio {
		ratio, snapped = clamped, false
	}
	out := t.Clone()
	cn := out.node(path)
	cn.Ratio = ratio
	out.fit(o)
	g := Guide{}
	if snapped {
		g = Guide{Valid: true, Axis: n.Axis, Pos: start + usable*ratio + gap/2, Span: span}
	}
	return out, g
}

// fit walks the tree from the top and pulls every ratio back inside what keeps
// both sides at their minimum, so that squeezing one split cannot leave a
// block of a nested split too small.
func (t Tree) fit(o DragOptions) {
	var walk func(n *Node, r Rect)
	walk = func(n *Node, r Rect) {
		if n == nil || n.leaf() {
			return
		}
		length := r.W
		if n.Axis == Rows {
			length = r.H
		}
		if usable := length - effGap(length, o.Gap); usable > 0 {
			lo, hi := ratioBounds(n, usable, o)
			if lo > hi && lo-hi < 1e-3 { // exactly enough room, minus rounding
				lo, hi = (lo+hi)/2, (lo+hi)/2
			}
			if lo <= hi {
				n.Ratio = min(max(n.Ratio, lo), hi)
			}
		}
		a, b, _ := splitRect(r, n, o.Gap)
		walk(n.A, a)
		walk(n.B, b)
	}
	walk(t.Root, Rect{W: o.Viewport.W, H: o.Viewport.H})
}

// ratioBounds returns the ratios that keep both sides at their minimum size.
func ratioBounds(n *Node, usable float32, o DragOptions) (lo, hi float32) {
	a, b := minSize(n.A, o.Min, o.Gap), minSize(n.B, o.Min, o.Gap)
	need := func(s Size) float32 {
		if n.Axis == Cols {
			return s.W
		}
		return s.H
	}
	lo = need(a) / usable
	hi = 1 - need(b)/usable
	return max(lo, 0.02), min(hi, 0.98)
}

// SetRatio returns a tree with the split at path set to ratio, clamped into
// (0, 1). It is for restoring and tests; dragging uses [Tree.DragDivider].
func (t Tree) SetRatio(path string, ratio float32) Tree {
	out := t.Clone()
	if n := out.node(path); n != nil && !n.leaf() {
		n.Ratio = min(max(ratio, 0.02), 0.98)
	}
	return out
}

// Insert returns a tree with block id placed at an edge of block target: the
// target's leaf becomes a split of the two, half and half. It does nothing
// when target is absent or id is already present.
func (t Tree) Insert(id, target string, edge Edge) Tree {
	if t.Root == nil {
		return Leaf(id)
	}
	if t.Has(id) || !t.Has(target) {
		return t
	}
	out := t.Clone()
	replaceLeaf(&out.Root, target, func(old *Node) *Node {
		fresh := &Node{ID: id}
		s := &Node{Ratio: 0.5}
		switch edge {
		case Left:
			s.Axis, s.A, s.B = Cols, fresh, old
		case Right:
			s.Axis, s.A, s.B = Cols, old, fresh
		case Top:
			s.Axis, s.A, s.B = Rows, fresh, old
		case Bottom:
			s.Axis, s.A, s.B = Rows, old, fresh
		}
		return s
	})
	return out
}

func replaceLeaf(slot **Node, id string, with func(*Node) *Node) bool {
	n := *slot
	if n == nil {
		return false
	}
	if n.leaf() {
		if n.ID != id {
			return false
		}
		*slot = with(n)
		return true
	}
	return replaceLeaf(&n.A, id, with) || replaceLeaf(&n.B, id, with)
}

// AddBeside returns a tree with id added next to block target, on the longer
// side of the target's rectangle (to its right in a wide cell, below it in a
// tall one).
func (t Tree) AddBeside(id, target string, viewport Size, gap float32) Tree {
	if t.Root == nil {
		return Leaf(id)
	}
	r, ok := t.Place(viewport, gap).Rect(target)
	if !ok {
		return t
	}
	if r.W >= r.H {
		return t.Insert(id, target, Right)
	}
	return t.Insert(id, target, Bottom)
}

// Remove returns a tree without block id; the neighbor takes the room.
func (t Tree) Remove(id string) Tree {
	if !t.Has(id) {
		return t
	}
	out := t.Clone()
	out.Root = removeLeaf(out.Root, id)
	return out
}

func removeLeaf(n *Node, id string) *Node {
	if n == nil {
		return nil
	}
	if n.leaf() {
		if n.ID == id {
			return nil
		}
		return n
	}
	n.A, n.B = removeLeaf(n.A, id), removeLeaf(n.B, id)
	switch {
	case n.A == nil:
		return n.B
	case n.B == nil:
		return n.A
	}
	return n
}

// Swap returns a tree where blocks a and b trade places.
func (t Tree) Swap(a, b string) Tree {
	if a == b || !t.Has(a) || !t.Has(b) {
		return t
	}
	out := t.Clone()
	var walk func(*Node)
	walk = func(n *Node) {
		switch {
		case n == nil:
		case n.leaf():
			switch n.ID {
			case a:
				n.ID = b
			case b:
				n.ID = a
			}
		default:
			walk(n.A)
			walk(n.B)
		}
	}
	walk(out.Root)
	return out
}

// Move returns a tree where block id leaves its place and goes to an edge of
// block target.
func (t Tree) Move(id, target string, edge Edge) Tree {
	if id == target || !t.Has(id) || !t.Has(target) {
		return t
	}
	return t.Remove(id).Insert(id, target, edge)
}

// Reconcile returns a tree holding exactly ids: blocks that are gone leave,
// new ones are added beside the last block (or the first of them starts the
// tree). It is how a saved tree is made to match the blocks that restored.
func (t Tree) Reconcile(ids []string, viewport Size, gap float32) Tree {
	out := t
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for _, id := range t.IDs() {
		if !want[id] {
			out = out.Remove(id)
		}
	}
	for _, id := range ids {
		if out.Has(id) {
			continue
		}
		have := out.IDs()
		if len(have) == 0 {
			out = Leaf(id)
			continue
		}
		out = out.AddBeside(id, have[len(have)-1], viewport, gap)
	}
	return out
}

// Auto returns the tree of the automatic grid for ids (see [Dims]), with
// every cell the size [Compute] would give it, so switching from the grid to
// a manual layout moves nothing.
func Auto(ids []string, viewport Size, gap float32) Tree {
	n := len(ids)
	if n == 0 {
		return Tree{}
	}
	cols, _ := Dims(n)
	var rows []*Node
	for start := 0; start < n; start += cols {
		end := min(start+cols, n)
		leaves := make([]*Node, 0, end-start)
		for _, id := range ids[start:end] {
			leaves = append(leaves, &Node{ID: id})
		}
		rows = append(rows, equalNodes(Cols, leaves, viewport.W, gap))
	}
	return Tree{Root: equalNodes(Rows, rows, viewport.H, gap)}
}

// Row returns a tree with all blocks side by side, of equal width.
func Row(ids []string, viewport Size, gap float32) Tree {
	return line(Cols, ids, viewport.W, gap)
}

// Column returns a tree with all blocks stacked, of equal height.
func Column(ids []string, viewport Size, gap float32) Tree {
	return line(Rows, ids, viewport.H, gap)
}

func line(axis Axis, ids []string, length, gap float32) Tree {
	if len(ids) == 0 {
		return Tree{}
	}
	nodes := make([]*Node, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, &Node{ID: id})
	}
	return Tree{Root: equalNodes(axis, nodes, length, gap)}
}

// equalNodes chains nodes into splits along axis so that each gets the same
// share of span (the length along the axis) once the gaps are taken out.
func equalNodes(axis Axis, nodes []*Node, span, gap float32) *Node {
	if len(nodes) == 1 {
		return nodes[0]
	}
	k := float32(len(nodes))
	share := (span - gap*(k-1)) / k
	usable := span - gap
	ratio := float32(0.5)
	if usable > 0 {
		ratio = min(max(share/usable, 0.02), 0.98)
	}
	return &Node{
		Axis: axis, Ratio: ratio, A: nodes[0],
		B: equalNodes(axis, nodes[1:], span-share-gap, gap),
	}
}

// MapIDs returns a tree with every block ID replaced by f(ID). It is how a
// saved tree, which names blocks by position, is pointed at the blocks that
// were restored.
func (t Tree) MapIDs(f func(string) string) Tree {
	out := t.Clone()
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.leaf() {
			n.ID = f(n.ID)
			return
		}
		walk(n.A)
		walk(n.B)
	}
	walk(out.Root)
	return out
}
