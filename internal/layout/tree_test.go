package layout

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

var vp = Size{W: 1200, H: 800}

func ids(n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = fmt.Sprintf("b%d", i+1)
	}
	return out
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

func TestAutoMatchesCompute(t *testing.T) {
	t.Parallel()
	const gap = 8
	for n := 1; n <= 9; n++ {
		auto := Compute(n, vp, Size{}, gap)
		// Rows that are not full give their cells the whole row, which
		// Compute does not; compare only full grids.
		if n%auto.Cols != 0 {
			continue
		}
		p := Auto(ids(n), vp, gap).Place(vp, gap)
		for i, c := range p.Cells {
			want := auto.Cells[i]
			if !near(c.Rect.X, want.X) || !near(c.Rect.Y, want.Y) || !near(c.Rect.W, want.W) || !near(c.Rect.H, want.H) {
				t.Fatalf("n=%d cell %d = %+v, grid says %+v", n, i, c.Rect, want)
			}
		}
	}
}

func TestPresets(t *testing.T) {
	t.Parallel()
	const gap = 8
	row := Row(ids(3), vp, gap).Place(vp, gap)
	for _, c := range row.Cells {
		if !near(c.Rect.W, (vp.W-2*gap)/3) || !near(c.Rect.H, vp.H) {
			t.Fatalf("row cell %+v", c)
		}
	}
	col := Column(ids(4), vp, gap).Place(vp, gap)
	for _, c := range col.Cells {
		if !near(c.Rect.H, (vp.H-3*gap)/4) || !near(c.Rect.W, vp.W) {
			t.Fatalf("column cell %+v", c)
		}
	}
	if got := Auto(nil, vp, gap); !got.Empty() {
		t.Fatal("no blocks should give an empty tree")
	}
}

func TestInsertRemoveSwapMove(t *testing.T) {
	t.Parallel()
	tr := Leaf("a").Insert("b", "a", Right).Insert("c", "b", Bottom)
	if got := tr.IDs(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("ids = %v", got)
	}
	left := Leaf("a").Insert("b", "a", Left)
	if got := left.IDs(); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("left insert = %v", got)
	}
	if got := tr.Swap("a", "c").IDs(); !slices.Equal(got, []string{"c", "b", "a"}) {
		t.Fatalf("swap = %v", got)
	}
	if got := tr.Move("a", "c", Top).IDs(); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatalf("move = %v", got)
	}
	if got := tr.Remove("b").IDs(); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("remove = %v", got)
	}
	if !tr.Remove("a").Remove("b").Remove("c").Empty() {
		t.Fatal("removing everything should empty the tree")
	}
	// No-ops leave the tree alone and the original is never modified.
	if got := tr.Insert("a", "b", Left).IDs(); len(got) != 3 {
		t.Fatalf("duplicate insert changed the tree: %v", got)
	}
	if got := tr.Insert("z", "nope", Left).IDs(); len(got) != 3 {
		t.Fatalf("insert beside a missing block changed the tree: %v", got)
	}
	if !slices.Equal(tr.IDs(), []string{"a", "b", "c"}) {
		t.Fatal("the original tree was modified")
	}
}

func TestAddBesideUsesTheLongerSide(t *testing.T) {
	t.Parallel()
	wide := Leaf("a").AddBeside("b", "a", Size{W: 1000, H: 400}, 8)
	if r, _ := wide.Place(Size{W: 1000, H: 400}, 8).Rect("b"); r.X == 0 {
		t.Fatalf("a wide cell should split side by side, b at %+v", r)
	}
	tall := Leaf("a").AddBeside("b", "a", Size{W: 400, H: 1000}, 8)
	if r, _ := tall.Place(Size{W: 400, H: 1000}, 8).Rect("b"); r.Y == 0 {
		t.Fatalf("a tall cell should split top to bottom, b at %+v", r)
	}
}

func TestSnapRatio(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		ratio  float32
		want   float32
		snaps  bool
		usable float32
	}{
		{"near half", 0.505, 0.5, true, 1000},
		{"near a third", 0.34, 1.0 / 3, true, 1000},
		{"near two thirds", 0.66, 2.0 / 3, true, 1000},
		{"far from all", 0.42, 0.42, false, 1000},
		{"threshold is in pixels, far", 0.52, 0.52, false, 1000},
		{"threshold is in pixels, near", 0.52, 0.5, true, 100},
		{"nothing to split", 0.5, 0.5, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, hit := SnapRatio(tc.ratio, tc.usable, 8)
			if hit != tc.snaps || !near(got, tc.want) {
				t.Fatalf("SnapRatio(%v) = %v, %v; want %v, %v", tc.ratio, got, hit, tc.want, tc.snaps)
			}
			if again, _ := SnapRatio(got, tc.usable, 8); !near(again, got) {
				t.Fatalf("snapping is not idempotent: %v then %v", got, again)
			}
		})
	}
}

func TestDragDivider(t *testing.T) {
	t.Parallel()
	const gap = 8
	tr := Leaf("a").Insert("b", "a", Right)
	o := DragOptions{Viewport: vp, Gap: gap, Min: Size{W: 200, H: 150}, Snap: true, Threshold: 8}

	got, guide := tr.DragDivider("", 600+3, o)
	if !guide.Valid || guide.Axis != Cols || !near(got.Root.Ratio, 0.5) {
		t.Fatalf("near the middle should snap to 1/2: ratio %v guide %+v", got.Root.Ratio, guide)
	}
	if !near(guide.Pos, 600) {
		t.Fatalf("guide at %v, want the divider center at 600", guide.Pos)
	}

	o.Snap = false
	free, guide := tr.DragDivider("", 603, o)
	if guide.Valid || math.Abs(float64(free.Root.Ratio)-0.5) < 0.001 {
		t.Fatalf("with snapping off the ratio stays free: %v %+v", free.Root.Ratio, guide)
	}

	// The left block never gets narrower than its minimum.
	tight, _ := tr.DragDivider("", 5, o)
	if w := tight.Place(vp, gap).Cells[0].Rect.W; w < 199.9 {
		t.Fatalf("left width %v is below the minimum", w)
	}
	tight, _ = tr.DragDivider("", 5000, o)
	if w := tight.Place(vp, gap).Cells[1].Rect.W; w < 199.9 {
		t.Fatalf("right width %v is below the minimum", w)
	}
	if same, _ := tr.DragDivider("AB", 100, o); same.Root.Ratio != tr.Root.Ratio {
		t.Fatal("a path that is not a split must change nothing")
	}

	// A horizontal divider in a stack uses y.
	stack := Leaf("a").Insert("b", "a", Bottom)
	o.Snap = true
	got, guide = stack.DragDivider("", 400, o)
	if !guide.Valid || guide.Axis != Rows || !near(got.Root.Ratio, 0.5) {
		t.Fatalf("stack: ratio %v guide %+v", got.Root.Ratio, guide)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	good := Leaf("a").Insert("b", "a", Right)
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]Tree{
		"dup":       {Root: &Node{Axis: Cols, Ratio: .5, A: &Node{ID: "a"}, B: &Node{ID: "a"}}},
		"ratio":     {Root: &Node{Axis: Cols, Ratio: 1.5, A: &Node{ID: "a"}, B: &Node{ID: "b"}}},
		"axis":      {Root: &Node{Axis: "diag", Ratio: .5, A: &Node{ID: "a"}, B: &Node{ID: "b"}}},
		"one side":  {Root: &Node{Axis: Cols, Ratio: .5, A: &Node{ID: "a"}}},
		"empty id":  {Root: &Node{}},
		"id+splits": {Root: &Node{ID: "x", Axis: Cols, Ratio: .5, A: &Node{ID: "a"}, B: &Node{ID: "b"}}},
	}
	for name, tr := range bad {
		if err := tr.Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if err := (Tree{}).Validate(); err != nil {
		t.Fatalf("an empty tree is valid: %v", err)
	}
}

func TestReconcile(t *testing.T) {
	t.Parallel()
	tr := Leaf("a").Insert("b", "a", Right).Insert("c", "b", Right)
	got := tr.Reconcile([]string{"a", "c", "d"}, vp, 8)
	if want := []string{"a", "c", "d"}; !slices.Equal(slices.Sorted(slices.Values(got.IDs())), want) {
		t.Fatalf("ids = %v", got.IDs())
	}
	if got := (Tree{}).Reconcile([]string{"x", "y"}, vp, 8).IDs(); len(got) != 2 {
		t.Fatalf("from empty: %v", got)
	}
}

// checkInvariants is the property every sequence of operations must keep:
// the tree holds exactly the expected blocks, is valid, no two cells overlap,
// every cell is inside the viewport, and cells plus gaps fill the viewport.
func checkInvariants(t *testing.T, tr Tree, want map[string]bool, step string) {
	t.Helper()
	got := tr.IDs()
	if len(got) != len(want) {
		t.Fatalf("%s: %d blocks, want %d (%v)", step, len(got), len(want), got)
	}
	seen := map[string]bool{}
	for _, id := range got {
		if !want[id] || seen[id] {
			t.Fatalf("%s: unexpected or duplicate block %s in %v", step, id, got)
		}
		seen[id] = true
	}
	if err := tr.Validate(); err != nil {
		t.Fatalf("%s: %v", step, err)
	}
	if tr.Empty() {
		return
	}
	p := tr.Place(vp, 8)
	var area float64
	for i, a := range p.Cells {
		r := a.Rect
		if r.X < -0.01 || r.Y < -0.01 || r.Right() > vp.W+0.01 || r.Bottom() > vp.H+0.01 || r.W < 0 || r.H < 0 {
			t.Fatalf("%s: cell %s %+v leaves the viewport", step, a.ID, r)
		}
		area += float64(r.W) * float64(r.H)
		for _, b := range p.Cells[i+1:] {
			if overlap(r, b.Rect) > 0.5 {
				t.Fatalf("%s: %s and %s overlap by %.2f", step, a.ID, b.ID, overlap(r, b.Rect))
			}
		}
	}
	for _, d := range p.Dividers {
		area += float64(d.Rect.W) * float64(d.Rect.H)
	}
	total := float64(vp.W) * float64(vp.H)
	if math.Abs(area-total) > total*1e-4 {
		t.Fatalf("%s: cells and gaps cover %.1f, viewport is %.1f", step, area, total)
	}
}

func overlap(a, b Rect) float32 {
	w := min(a.Right(), b.Right()) - max(a.X, b.X)
	h := min(a.Bottom(), b.Bottom()) - max(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// TestRandomOperationSequences applies thousands of random operations and
// checks the invariants after each (TECH-PLAN M2 acceptance 3).
func TestRandomOperationSequences(t *testing.T) {
	t.Parallel()
	for seed := range uint64(40) {
		rng := rand.New(rand.NewPCG(seed, seed*7919+1))
		var tr Tree
		want := map[string]bool{}
		next := 0
		pick := func() string {
			all := tr.IDs()
			return all[rng.IntN(len(all))]
		}
		for step := range 200 {
			label := fmt.Sprintf("seed %d step %d", seed, step)
			switch op := rng.IntN(10); {
			case op < 2 || tr.Empty():
				next++
				id := fmt.Sprintf("n%d", next)
				if tr.Empty() || rng.IntN(2) == 0 {
					if tr.Empty() {
						tr = Leaf(id)
					} else {
						tr = tr.Insert(id, pick(), Edge(rng.IntN(4)))
					}
				} else {
					tr = tr.AddBeside(id, pick(), vp, 8)
				}
				want[id] = true
			case op == 2 && len(want) > 1:
				id := pick()
				tr = tr.Remove(id)
				delete(want, id)
			case op == 3 && len(want) > 1:
				tr = tr.Swap(pick(), pick())
			case op == 4 && len(want) > 1:
				tr = tr.Move(pick(), pick(), Edge(rng.IntN(4)))
			case op <= 7:
				p := tr.Place(vp, 8)
				if len(p.Dividers) == 0 {
					continue
				}
				d := p.Dividers[rng.IntN(len(p.Dividers))]
				pos := rng.Float32() * max(vp.W, vp.H) * 1.2
				tr, _ = tr.DragDivider(d.Path, pos, DragOptions{
					Viewport: vp, Gap: 8, Min: Size{W: 120, H: 90}, Snap: rng.IntN(2) == 0, Threshold: 8,
				})
			case op == 8:
				all := tr.IDs()
				switch rng.IntN(3) {
				case 0:
					tr = Auto(all, vp, 8)
				case 1:
					tr = Row(all, vp, 8)
				default:
					tr = Column(all, vp, 8)
				}
			default:
				keep := tr.IDs()
				if len(keep) > 1 {
					keep = keep[:len(keep)-1]
				}
				next++
				id := fmt.Sprintf("r%d", next)
				keep = append(keep, id)
				tr = tr.Reconcile(keep, vp, 8)
				want = map[string]bool{}
				for _, k := range keep {
					want[k] = true
				}
			}
			checkInvariants(t, tr, want, label)
		}
	}
}

func TestDragRespectsMinimumsAndIsStable(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 2))
	tr := Auto(ids(6), vp, 8)
	floor := Size{W: 150, H: 100}
	for range 500 {
		p := tr.Place(vp, 8)
		d := p.Dividers[rng.IntN(len(p.Dividers))]
		o := DragOptions{Viewport: vp, Gap: 8, Min: floor, Snap: true, Threshold: 8}
		next, _ := tr.DragDivider(d.Path, rng.Float32()*1400, o)
		// Dragging to where the divider already is changes nothing.
		// (with snapping off, since a divider near a snap point is pulled in).
		free := o
		free.Snap = false
		again, _ := next.DragDivider(d.Path, centerAlong(next.Place(vp, 8), d), free)
		if !slices.EqualFunc(next.Place(vp, 8).Cells, again.Place(vp, 8).Cells, func(a, b Cell) bool {
			return a.ID == b.ID && near(a.Rect.X, b.Rect.X) && near(a.Rect.Y, b.Rect.Y) && near(a.Rect.W, b.Rect.W) && near(a.Rect.H, b.Rect.H)
		}) {
			t.Fatal("dragging a divider to where it is moved something")
		}
		for _, c := range next.Place(vp, 8).Cells {
			if c.Rect.W < floor.W-0.1 || c.Rect.H < floor.H-0.1 {
				t.Fatalf("%s shrank to %+v, below the minimum", c.ID, c.Rect)
			}
		}
		tr = next
	}
}

func centerAlong(p Placement, d Divider) float32 {
	for _, x := range p.Dividers {
		if x.Path == d.Path {
			if x.Axis == Cols {
				return x.Rect.X + x.Rect.W/2
			}
			return x.Rect.Y + x.Rect.H/2
		}
	}
	return 0
}
