// Package multibuffer holds the operators that read their input a chunk of
// several blocks at a time, and the arithmetic they share for deciding how many
// buffers to take.
package multibuffer

import "math"

// reservedBuffers is how many of the available buffers an operator leaves
// alone. Other scans in the same plan, such as the one reading the operator's
// input, pin blocks of their own, and taking every free buffer would leave
// them waiting for one.
const reservedBuffers = 2

// BestRoot returns how many buffers an operator over size blocks should take
// when available buffers are free: the smallest of size, its square root, its
// cube root and so on, rounded up, that fits in what is left once
// reservedBuffers are set aside.
//
// A sort uses it to split its input into runs of that many blocks, so that the
// runs are merged that many at a time in as few passes as the buffers allow.
// When size fits as it is, it is returned as it is, so that the whole input is
// sorted in one go.
//
// It never returns less than 1, since an operator cannot run with no buffer.
func BestRoot(available, size int) int {
	avail := available - reservedBuffers
	if avail <= 1 || size <= 1 {
		return 1
	}

	// The roots shrink towards 2 as i grows, and avail is at least 2 here, so
	// one of them fits.
	for i := 1; ; i++ {
		if k := ceilRoot(size, i); k <= avail {
			return k
		}
	}
}

// ceilRoot returns the i-th root of size rounded up: the smallest k with k^i
// at least size.
//
// math.Pow only gives a first guess, which is then corrected in integers. Its
// result can land just above a whole number that is the exact root, such as
// 5.000000000000001 for the cube root of 125, and rounding that up would give
// one too many.
func ceilRoot(size, i int) int {
	k := max(int(math.Ceil(math.Pow(float64(size), 1/float64(i)))), 1)
	for k > 1 && powReaches(k-1, i, size) {
		k--
	}
	for !powReaches(k, i, size) {
		k++
	}
	return k
}

// powReaches reports whether k^i is at least size. It stops multiplying as soon
// as the product gets there, so that it does not overflow.
func powReaches(k, i, size int) bool {
	p := 1
	for range i {
		p *= k
		if p >= size {
			return true
		}
	}
	return p >= size
}

// BestFactor returns how many buffers an operator over size blocks should take
// when available buffers are free: the largest of size, size/2, size/3 and so
// on, rounded up, that fits in what is left once reservedBuffers are set
// aside.
//
// A product uses it to read its right side in chunks of that many blocks, so
// that its left side is scanned once a chunk, as few times as the buffers
// allow.
//
// It never returns less than 1, since an operator cannot run with no buffer.
func BestFactor(available, size int) int {
	avail := available - reservedBuffers
	if avail <= 1 || size <= 1 {
		return 1
	}

	// size/size rounds up to 1, which always fits, so the loop ends.
	for i := 1; ; i++ {
		if k := (size + i - 1) / i; k <= avail {
			return k
		}
	}
}
