package lowering

// visit is a node for postOrder to lower and the context it inherits.
type visit[N, C any] struct {
	node    N
	context C
}

// walker supplies the two steps of postOrder. expand appends the children of
// a node to visit, in order, each with the context it inherits. lower then
// receives a node, its context, and its children's results in that order; the
// results slice is reused after lower returns.
type walker[N, C, R any] interface {
	expand(node N, context C, children []visit[N, C]) []visit[N, C]
	lower(node N, context C, children []R) R
}

// postOrder maps the tree under root bottom-up without recursion, so deep
// expression chains and nested bodies do not grow the Go stack. The walker is
// a type parameter rather than a pair of closures so that starting a walk,
// which happens once per attribute, allocates nothing for it.
func postOrder[N, C, R any, W walker[N, C, R]](w W, root N, context C) R {
	type frame struct {
		visit[N, C]
		expanded bool
		children int
	}
	// Most walks cover one attribute value of a few nodes. Starting with room
	// for those avoids regrowing each slice from empty.
	const initial = 4
	stack := append(make([]frame, 0, initial), frame{visit: visit[N, C]{root, context}})
	children := make([]visit[N, C], 0, initial)
	results := make([]R, 0, initial)

	for len(stack) != 0 {
		top := len(stack) - 1
		if !stack[top].expanded {
			stack[top].expanded = true
			children = w.expand(stack[top].node, stack[top].context, children[:0])
			stack[top].children = len(children)
			// The first child must be lowered first, so it goes on top.
			for i := len(children) - 1; i >= 0; i-- {
				stack = append(stack, frame{visit: children[i]})
			}
			continue
		}

		current := stack[top]
		stack = stack[:top]
		start := len(results) - current.children
		lowered := w.lower(current.node, current.context, results[start:])
		results = append(results[:start], lowered)
	}
	return results[0]
}
