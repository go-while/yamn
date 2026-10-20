package crandom

import (
	"fmt"
	"slices"
	"testing"
)

// makeRange creates a slice of integers in numeric order between min and max (inclusive)
func makeRange(min, max int) []int {
	a := make([]int, max-min+1)
	for i := range a {
		a[i] = min + i
	}
	return a
}

// contains tests if an integer is a member of a slice
func contains(s []int, e int) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// sum totals all the integers in a slice
func sum(s []int) (t int) {
	for _, i := range s {
		t += i
	}
	return
}

func TestDice(t *testing.T) {
	i := Dice()
	if i < 0 || i > 255 {
		t.Fatalf("Dice should return 0-255.  Got: %d", i)
	}
}

func TestInt(t *testing.T) {
	for range 100 {
		n := RandomInt(3)
		if n < 0 || n > 2 {
			t.Fatalf("Random integer out of range: %d", n)
		}
	}
}

func TestRandInts(t *testing.T) {
	testLen := 50
	is := RandInts(testLen)
	seq := makeRange(0, testLen-1)
	if len(is) != len(seq) {
		t.Fatalf("Incorrect sequence length.  Expected=%d, Got=%d", len(seq), len(is))
	}
	if sum(is) != sum(seq) {
		t.Fatalf("Sum of sequence not equal to sum of randomized slice.  Expected=%d, Got=%d", sum(seq), sum(is))
	}
	inSeq := 0
	for i := range seq {
		if !contains(is, i) {
			t.Fatalf("Random range does not include element: %d", i)
		}
		if is[i] == i {
			inSeq++
		}
	}
	// A random permutation leaves about one element in place on average.
	// More than 10 of 50 has a probability of roughly 1 in 100 million.
	if inSeq > testLen/5 {
		t.Errorf("Suspiciously high number of sequence n in slice n. Collisions=%d", inSeq)
	}
}

func TestShuffle(t *testing.T) {
	start := 48
	end := 122
	shuffleText := make([]string, end-start+1)
	for n := 0; n <= end-start; n++ {
		shuffleText[n] = string(rune(n + start))
	}
	original := slices.Clone(shuffleText)
	Shuffle(shuffleText)
	// The shuffled slice must contain exactly the original elements.
	sorted := slices.Clone(shuffleText)
	slices.Sort(sorted)
	if !slices.Equal(sorted, original) {
		t.Fatal("Shuffle changed the elements of the slice")
	}
	// With 75 elements, the chance of a shuffle keeping the original order
	// is 1 in 75! (effectively zero).
	if slices.Equal(shuffleText, original) {
		t.Fatal("Bad shuffle. Slice order unchanged.")
	}
}

func TestRandBytes(t *testing.T) {
	sampleLength := 4096
	rb := Randbytes(sampleLength)
	if len(rb) != sampleLength {
		t.Fatalf("Mismatched byte count.  Expected=%d, Got=%d", sampleLength, len(rb))
	}
	// Each byte value is expected about 16 times.  Fail if any value occurs
	// 64 times or more, which random data will practically never do.
	threshold := 64
	m := make(map[byte]int)
	for _, b := range rb {
		m[b]++
	}
	for b, count := range m {
		if count >= threshold {
			t.Errorf("Suspiciously high number of matching bytes in random data. Byte=%d, Count=%d", b, count)
		}
	}
}

// permutationCounts runs perm n times and counts how often each ordering of
// three elements comes up.
func permutationCounts(n int, perm func() string) map[string]int {
	counts := make(map[string]int)
	for range n {
		counts[perm()]++
	}
	return counts
}

// checkUniform fails if any of the 6 orderings of three elements is more
// than 5% away from its expected share.  With 60000 runs that's about 5.5
// standard deviations, so a uniform shuffle practically never fails, while
// the naive shuffle (some orderings 4/27, others 5/27) always does.
func checkUniform(t *testing.T, name string, counts map[string]int, runs int) {
	t.Helper()
	if len(counts) != 6 {
		t.Errorf("%s: expected 6 different orderings, got %d: %v", name, len(counts), counts)
		return
	}
	expected := runs / 6
	for order, c := range counts {
		if c < expected*95/100 || c > expected*105/100 {
			t.Errorf("%s: ordering %s came up %d times, expected about %d: %v", name, order, c, expected, counts)
		}
	}
}

func TestShuffleUniform(t *testing.T) {
	const runs = 60000
	counts := permutationCounts(runs, func() string {
		s := []string{"a", "b", "c"}
		Shuffle(s)
		return s[0] + s[1] + s[2]
	})
	checkUniform(t, "Shuffle", counts, runs)
}

func TestRandIntsUniform(t *testing.T) {
	const runs = 60000
	counts := permutationCounts(runs, func() string {
		m := RandInts(3)
		return fmt.Sprint(m)
	})
	checkUniform(t, "RandInts", counts, runs)
}
