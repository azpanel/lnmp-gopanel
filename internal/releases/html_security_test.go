package releases

import (
	"strings"
	"testing"
)

// GO-2026-6860: ignored closing tags used to copy the accumulated text on
// every token. A response well below maxBody could allocate gigabytes.
func TestHTMLNotesAllocationBound(t *testing.T) {
	input := `<div id="docs-body">` + strings.Repeat("a</x>", 16000) + `</div>`
	if got := mysqlNotes(input); got != strings.Repeat("a", 16000) {
		t.Fatal("adversarial HTML changed the extracted release notes")
	}
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			mysqlNotes(input)
		}
	})
	if allocated := result.AllocedBytesPerOp(); allocated > 8*1024*1024 {
		t.Fatalf("80 KB HTML allocated %d bytes; limit is 8 MiB", allocated)
	}
}

func BenchmarkAdversarialHTMLNotes(b *testing.B) {
	for _, size := range []int{2000, 16000} {
		input := `<div id="docs-body">` + strings.Repeat("a</x>", size) + `</div>`
		b.Run(stringSize(size*5), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for b.Loop() {
				mysqlNotes(input)
			}
		})
	}
}

func stringSize(size int) string {
	if size == 10000 {
		return "10KB"
	}
	return "80KB"
}
