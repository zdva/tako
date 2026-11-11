package layout

import (
	"slices"
	"testing"
)

func squares() []Rect {
	// A 3x2 minimap: 40x30 squares, 4 apart.
	return Compute(6, Size{W: 128, H: 64}, Size{}, 4).Cells
}

func TestHitSquare(t *testing.T) {
	t.Parallel()
	cells := squares()
	tests := []struct {
		name string
		x, y float32
		want int
	}{
		{name: "first square", x: 5, y: 5, want: 0},
		{name: "second row", x: 5, y: 40, want: 3},
		{name: "gap between squares", x: cells[0].Right() + 1, y: 5, want: -1},
		{name: "outside", x: 500, y: 500, want: -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := HitSquare(cells, tc.x, tc.y); got != tc.want {
				t.Fatalf("HitSquare(%v, %v) = %d, want %d", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestSelectBox(t *testing.T) {
	t.Parallel()
	cells := squares()
	tests := []struct {
		name string
		box  Rect
		want []int
	}{
		{name: "one square", box: Rect{X: 2, Y: 2, W: 5, H: 5}, want: []int{0}},
		{name: "first row", box: Rect{X: 0, Y: 0, W: 128, H: 10}, want: []int{0, 1, 2}},
		{name: "reversed drag", box: Rect{X: 60, Y: 40, W: -60, H: -40}, want: []int{0, 1, 3, 4}},
		{name: "nothing", box: Rect{X: 500, Y: 500, W: 5, H: 5}, want: []int{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := SelectBox(cells, tc.box); !slices.Equal(got, tc.want) {
				t.Fatalf("SelectBox = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNearestSlot(t *testing.T) {
	t.Parallel()
	cells := squares()
	if got := NearestSlot(cells, 1000, 1000); got != 5 {
		t.Fatalf("far bottom-right snaps to %d, want 5", got)
	}
	if got := NearestSlot(cells, -50, -50); got != 0 {
		t.Fatalf("far top-left snaps to %d, want 0", got)
	}
	if got := NearestSlot(nil, 0, 0); got != -1 {
		t.Fatalf("no cells gives %d, want -1", got)
	}
}

func TestMove(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		from, to int
		want     []string
	}{
		{name: "forward", from: 0, to: 2, want: []string{"b", "c", "a", "d"}},
		{name: "backward", from: 3, to: 1, want: []string{"a", "d", "b", "c"}},
		{name: "same", from: 1, to: 1, want: []string{"a", "b", "c", "d"}},
		{name: "out of range", from: 9, to: 1, want: []string{"a", "b", "c", "d"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := []string{"a", "b", "c", "d"}
			got := Move(in, tc.from, tc.to)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Move = %v, want %v", got, tc.want)
			}
			if !slices.Equal(in, []string{"a", "b", "c", "d"}) {
				t.Fatalf("Move mutated its input: %v", in)
			}
		})
	}
}

func TestCornerOrigins(t *testing.T) {
	t.Parallel()
	c := CornerOrigins(Size{W: 1000, H: 800}, Size{W: 200, H: 100}, 12)
	if c[TopLeft].X != 12 || c[TopLeft].Y != 12 {
		t.Fatalf("top-left = %+v", c[TopLeft])
	}
	if c[BottomRight].X != 788 || c[BottomRight].Y != 688 {
		t.Fatalf("bottom-right = %+v", c[BottomRight])
	}
}

func TestSnapEdges(t *testing.T) {
	t.Parallel()
	if got := SnapEdges(498, 8, 0, 500, 1000); got != 500 {
		t.Fatalf("498 snaps to %v, want 500", got)
	}
	if got := SnapEdges(480, 8, 0, 500, 1000); got != 480 {
		t.Fatalf("480 should stay, got %v", got)
	}
}
