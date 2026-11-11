package layout

import "testing"

func TestDims(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		n          int
		cols, rows int
	}{
		{name: "zero", n: 0, cols: 1, rows: 1},
		{name: "one fills the area", n: 1, cols: 1, rows: 1},
		{name: "two are halves", n: 2, cols: 2, rows: 1},
		{name: "three use 2x2", n: 3, cols: 2, rows: 2},
		{name: "four use 2x2", n: 4, cols: 2, rows: 2},
		{name: "five use 3x2", n: 5, cols: 3, rows: 2},
		{name: "six use 3x2", n: 6, cols: 3, rows: 2},
		{name: "seven use 3x3", n: 7, cols: 3, rows: 3},
		{name: "nine use 3x3", n: 9, cols: 3, rows: 3},
		{name: "ten keep 3 columns", n: 10, cols: 3, rows: 4},
		{name: "twelve", n: 12, cols: 3, rows: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cols, rows := Dims(tc.n)
			if cols != tc.cols || rows != tc.rows {
				t.Fatalf("Dims(%d) = %dx%d, want %dx%d", tc.n, cols, rows, tc.cols, tc.rows)
			}
		})
	}
}

func TestCompute(t *testing.T) {
	t.Parallel()
	viewport := Size{W: 1900, H: 1000}
	minCell := Size{W: 600, H: 400}

	t.Run("one block fills the viewport", func(t *testing.T) {
		t.Parallel()
		g := Compute(1, viewport, minCell, 8)
		if g.Cells[0] != (Rect{W: 1900, H: 1000}) {
			t.Fatalf("cell = %+v", g.Cells[0])
		}
		if g.Content != viewport {
			t.Fatalf("content = %+v, want the viewport", g.Content)
		}
	})

	t.Run("six blocks shrink to 3x2 and still fit", func(t *testing.T) {
		t.Parallel()
		g := Compute(6, viewport, minCell, 8)
		if g.Cols != 3 || g.Rows != 2 || len(g.Cells) != 6 {
			t.Fatalf("grid = %dx%d with %d cells", g.Cols, g.Rows, len(g.Cells))
		}
		if g.Content.H > viewport.H+0.01 || g.Content.W > viewport.W+0.01 {
			t.Fatalf("content %+v should fit the viewport %+v", g.Content, viewport)
		}
	})

	t.Run("narrow viewport drops columns instead of scrolling sideways", func(t *testing.T) {
		t.Parallel()
		g := Compute(6, Size{W: 1300, H: 1000}, minCell, 8)
		if g.Cols != 2 || g.Rows != 3 {
			t.Fatalf("grid = %dx%d, want 2x3", g.Cols, g.Rows)
		}
		if g.Content.W > 1300.01 {
			t.Fatalf("content width %v overflows the viewport", g.Content.W)
		}
	})

	t.Run("many blocks hit the minimum height and scroll vertically", func(t *testing.T) {
		t.Parallel()
		g := Compute(12, viewport, minCell, 8)
		for i, c := range g.Cells {
			if c.H < minCell.H || c.W < minCell.W {
				t.Fatalf("cell %d is %vx%v, below the minimum", i, c.W, c.H)
			}
		}
		if g.Content.H <= viewport.H {
			t.Fatalf("content height %v should exceed the viewport %v", g.Content.H, viewport.H)
		}
	})

	t.Run("cells do not overlap", func(t *testing.T) {
		t.Parallel()
		g := Compute(9, viewport, minCell, 8)
		for i := range g.Cells {
			for j := i + 1; j < len(g.Cells); j++ {
				if g.Cells[i].Intersects(g.Cells[j]) {
					t.Fatalf("cells %d and %d overlap", i, j)
				}
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		g := Compute(0, viewport, minCell, 8)
		if len(g.Cells) != 0 {
			t.Fatalf("cells = %d, want 0", len(g.Cells))
		}
	})
}
