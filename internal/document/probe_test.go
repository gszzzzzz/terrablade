package document

import (
	"slices"
	"strconv"
	"testing"
)

func TestProbeBorrowsContinuation(t *testing.T) {
	for _, size := range []int{5000, 10000, 20000} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			continuation := make([]command, size)
			for i := range continuation {
				continuation[i] = command{doc: Text("unvisited")}
			}
			continuation[size-1] = command{doc: Line()}
			before := slices.Clone(continuation)
			candidate := command{doc: Group(Concat(Text("a"), Line(), Text("b"))), flat: true}
			options := Options{}.normalized()
			ok, scratch := fits(candidate, continuation, lineWidth{}, options, nil)
			if !ok || cap(scratch) > 8 {
				t.Fatalf("short probe fit=%v used scratch capacity %d for continuation %d", ok, cap(scratch), size)
			}
			allocs := testing.AllocsPerRun(100, func() {
				ok, scratch = fits(candidate, continuation, lineWidth{}, options, scratch)
			})
			// Stack reuse must be independent of the unvisited continuation.
			if !ok || !slices.Equal(before, continuation) || allocs != 0 {
				t.Fatalf("probe changed continuation or did not reuse scratch: fit=%v allocations=%g", ok, allocs)
			}
		})
	}
}
