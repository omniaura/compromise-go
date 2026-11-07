package compromise

import "testing"

// BenchmarkLoadModel is the one-time cost of decoding the embedded model
// (paid once per process, on first use).
func BenchmarkLoadModel(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := loadModel(modelGz); err != nil {
			b.Fatal(err)
		}
	}
}
