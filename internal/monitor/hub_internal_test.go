package monitor

import "testing"

func TestShown_DiffersOnlyByVisibleSteps(t *testing.T) {
	base := shown{cpu: 40, mem: 50, disk: 60, gpu: 0, known: true}
	for _, tc := range []struct {
		name string
		to   shown
		want bool
	}{
		{"same", base, false},
		{"a point of cpu", shown{cpu: 41, mem: 50, disk: 60, known: true}, false},
		{"two points down", shown{cpu: 38, mem: 50, disk: 60, known: true}, false},
		{"three points up", shown{cpu: 43, mem: 50, disk: 60, known: true}, true},
		{"three points down", shown{cpu: 40, mem: 47, disk: 60, known: true}, true},
		{"disk", shown{cpu: 40, mem: 50, disk: 64, known: true}, true},
		{"a gpu appears at 5%", shown{cpu: 40, mem: 50, disk: 60, gpu: 5, known: true}, true},
		{"fails", shown{cpu: 40, mem: 50, disk: 60, known: true, failing: true}, true},
		{"first reading", shown{}, true},
	} {
		if got := tc.to.differs(base, 3); got != tc.want {
			t.Errorf("%s: differs = %v, want %v", tc.name, got, tc.want)
		}
	}
	if !(shown{cpu: 41, known: true}).differs(shown{cpu: 40, known: true}, 1) {
		t.Error("a step of 1 sees every point")
	}
}
