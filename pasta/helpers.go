package pasta

import "iter"

func multiSLiceIter[E any](slices ...[]E) iter.Seq[E] {
	return func(yield func(E) bool) {
		for _, slice := range slices {
			for _, e := range slice {
				if !yield(e) {
					return
				}
			}
		}
	}
}
