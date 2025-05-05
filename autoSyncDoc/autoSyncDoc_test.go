//go:build cgo

package autosyncdoc

import (
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"
)

// Helper function to generate somewhat complex nested data
func generateTestData(iteration int) map[string]interface{} {
	r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(iteration))) // Seed random generator

	nestedMap := make(map[string]interface{})
	for j := 0; j < r.Intn(5)+1; j++ { // 1 to 5 keys
		nestedMap[fmt.Sprintf("nested_key_%d_%d", iteration, j)] = r.Float64() * 1000
	}

	nestedSlice := make([]interface{}, 0, r.Intn(10)+1) // 1 to 10 elements
	for j := 0; j < cap(nestedSlice); j++ {
		switch r.Intn(4) {
		case 0:
			nestedSlice = append(nestedSlice, r.Int63())
		case 1:
			nestedSlice = append(nestedSlice, fmt.Sprintf("str_%d_%d", iteration, r.Intn(100)))
		case 2:
			nestedSlice = append(nestedSlice, r.Intn(2) == 1) // bool
		default:
			nestedSlice = append(nestedSlice, nil)
		}
	}

	return map[string]interface{}{
		fmt.Sprintf("message_%d", iteration):      fmt.Sprintf("hello %d", iteration),
		fmt.Sprintf("count_%d", iteration):        int64(iteration * r.Intn(100)),
		fmt.Sprintf("valid_%d", iteration):        iteration%2 == 0,
		fmt.Sprintf("float_%d", iteration):        r.Float64() * float64(iteration),
		fmt.Sprintf("nested_map_%d", iteration):   nestedMap,
		fmt.Sprintf("nested_slice_%d", iteration): nestedSlice,
		fmt.Sprintf("null_val_%d", iteration):     nil,
	}
}

func TestMemoryLeakStress(t *testing.T) {
	iterations := 10000 // Increase for more thorough testing, decrease for speed

	initialMemStats := new(runtime.MemStats)
	runtime.ReadMemStats(initialMemStats)

	for i := 0; i < iterations; i++ {
		if i > 0 && i%1000 == 0 {
			// Optional: Log progress and periodically force GC to see if Go heap grows unboundedly
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			t.Logf("Iteration %d: Alloc=%v MiB, TotalAlloc=%v MiB, Sys=%v MiB, NumGC=%v",
				i, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		}

		doc := NewAutoSyncDoc()
		if doc == nil || doc.yDoc == nil {
			t.Fatalf("Iteration %d: NewAutoSyncDoc returned nil", i)
		}

		testData := generateTestData(i)

		for key, value := range testData {
			err := doc.AddValue(key, value)
			if err != nil {
				// Using Fatalf here will stop the test immediately on error
				t.Fatalf("Iteration %d: AddValue failed for key '%s': %v", i, key, err)
			}
		}

		jsonData, err := doc.ToJSON()
		if err != nil {
			t.Fatalf("Iteration %d: ToJSON failed: %v", i, err)
		}
		if len(jsonData) != len(testData) {
			// Basic sanity check - might need deeper comparison depending on needs
			t.Fatalf("Iteration %d: ToJSON map length mismatch. Expected %d, got %d", i, len(testData), len(jsonData))
		}

		// CRITICAL: Ensure Destroy is called to free C memory
		doc.Destroy()
		// Setting doc to nil helps GC, though not strictly necessary for leak detection by external tools
		doc = nil
	}

	// Force final GC and check memory stats (mostly for Go heap, C leaks need external tools)
	runtime.GC()
	finalMemStats := new(runtime.MemStats)
	runtime.ReadMemStats(finalMemStats)
	t.Logf("Stress test finished.")
	t.Logf("Initial Alloc: %v MiB", initialMemStats.Alloc/1024/1024)
	t.Logf("Final Alloc:   %v MiB", finalMemStats.Alloc/1024/1024)
	t.Logf("Total Alloc: %v MiB", finalMemStats.TotalAlloc/1024/1024)
	t.Logf("Sys Memory:  %v MiB", finalMemStats.Sys/1024/1024)
	t.Logf("Num GC:      %v", finalMemStats.NumGC)

	// Note: A small increase in final Alloc vs initial Alloc is normal due to runtime overhead.
	// Significant growth could indicate a Go leak, but C leaks MUST be checked externally.
}
