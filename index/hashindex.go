package index

import (
	"hash/fnv"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// numBuckets is how many buckets a hash index spreads its entries over.
const numBuckets = 100

// bucketOf is the number of the bucket the entries filed under value go in.
//
// The bucket a value goes in is written to disk as part of a file name, so the
// hash has to come out the same in every process that opens the index. That
// rules out hash/maphash, whose seed is picked afresh each time; an int is
// taken as it is and a varchar through FNV-1a, neither of which has a seed.
//
// The remainder is kept non-negative. Go's % takes the sign of the dividend,
// and a negative int would otherwise land in a bucket that does not exist.
func bucketOf(value query.Constant) int {
	if value.Type() == recordmanager.FieldTypeInt {
		// AsInt cannot fail here: the type has just been checked.
		n, _ := value.AsInt()

		return ((int(n) % numBuckets) + numBuckets) % numBuckets
	}

	// AsString cannot fail either, a constant being one of the two kinds.
	s, _ := value.AsString()
	h := fnv.New32a()
	// Writing to a hash never fails.
	_, _ = h.Write([]byte(s))

	return int(h.Sum32() % numBuckets)
}
