// Package multibuffer contains the operators that read their input in chunks
// of several blocks, and the shared functions that decide how many buffers
// they use.
package multibuffer

import "math"

// reservedBuffers is the number of available buffers that an operator does
// not use. Other scans in the same plan, such as the scan of the operator's
// input, pin blocks too. If an operator took every free buffer, those scans
// would have to wait for one.
const reservedBuffers = 2

// BestRoot returns the number of buffers that an operator over size blocks
// should use when available buffers are free. Without the reserved buffers,
// the candidates are size, its square root, its cube root and so on, each
// rounded up. The result is the first candidate that fits.
//
// A sort uses it to split its input into runs of that many blocks. The runs
// are then merged that many at a time, in as few passes as the buffers allow.
// If size itself fits, it is returned, so that the whole input is sorted in
// one pass.
//
// The result is at least 1, because an operator cannot run without a buffer.
func BestRoot(available, size int) int {
	avail := available - reservedBuffers
	if avail <= 1 || size <= 1 {
		return 1
	}

	// The roots decrease towards 2 as i grows, and avail is at least 2 here, so
	// one of them fits.
	for i := 1; ; i++ {
		if k := ceilRoot(size, i); k <= avail {
			return k
		}
	}
}

// ceilRoot returns the i-th root of size, rounded up: the smallest k for
// which k^i is at least size.
//
// math.Pow only provides a first estimate, which is then corrected with
// integer arithmetic. Its result can be slightly above an exact integer root,
// such as 5.000000000000001 for the cube root of 125. Rounding that up would
// give a result that is too large by one.
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

// powReaches reports whether k^i is at least size. It stops multiplying as
// soon as the product reaches size, so that the product does not overflow.
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

// BestFactor returns the number of buffers that an operator over size blocks
// should use when available buffers are free. Without the reserved buffers,
// the candidates are size, size/2, size/3 and so on, each rounded up. The
// result is the first candidate that fits.
//
// A product uses it to read its right side in chunks of that many blocks. Its
// left side is then scanned once per chunk, which is as few times as the
// buffers allow.
//
// The result is at least 1, because an operator cannot run without a buffer.
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
