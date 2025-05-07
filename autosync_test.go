//go:build cgo

package autosync

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

		doc1 := NewDoc()
		if doc1 == nil || doc1.yDoc == nil {
			t.Fatalf("Iteration %d: NewAutoSyncDoc returned nil for doc1", i)
		}

		testData := generateTestData(i)

		// Test UpdateToState
		_, err := doc1.UpdateToState(testData)
		if err != nil {
			t.Fatalf("Iteration %d: UpdateToState failed: %v", i, err)
		}

		jsonData1, err := doc1.ToJSON()
		if err != nil {
			t.Fatalf("Iteration %d: ToJSON failed for doc1: %v", i, err)
		}
		if !compareMaps(jsonData1, testData) {
			t.Fatalf("Iteration %d: ToJSON map content mismatch after UpdateToState. Expected %v, got %v", i, testData, jsonData1)
		}

		// Test GetStateVector
		stateVector, err := doc1.GetStateVector()
		if err != nil {
			t.Fatalf("Iteration %d: GetStateVector failed: %v", i, err)
		}
		if i > 0 && len(stateVector) == 0 { // Allow empty state vector only if testData was empty (iteration 0 with certain random outcomes)
			// A more robust check would be to see if testData itself would result in an empty doc.
			// For simplicity, we assume non-empty state for i > 0 unless specifically designed.
			// If testData can be legitimately empty, this check needs adjustment.
			initialState, _ := doc1.GetState()
			if len(initialState) > 0 { // only fail if the state was non-empty
				t.Fatalf("Iteration %d: GetStateVector returned empty state vector for non-empty document", i)
			}
		}

		// Test ApplyStateVector
		doc2 := NewDoc()
		if doc2 == nil || doc2.yDoc == nil {
			t.Fatalf("Iteration %d: NewAutoSyncDoc returned nil for doc2", i)
		}

		err = doc2.ApplyStateVector(stateVector)
		if err != nil {
			t.Fatalf("Iteration %d: ApplyStateVector failed: %v", i, err)
		}

		jsonData2, err := doc2.ToJSON()
		if err != nil {
			t.Fatalf("Iteration %d: ToJSON failed for doc2 after ApplyStateVector: %v", i, err)
		}

		if !compareMaps(jsonData2, testData) {
			t.Fatalf("Iteration %d: ToJSON map content mismatch for doc2 after ApplyStateVector. Expected %v, got %v", i, testData, jsonData2)
		}

		// CRITICAL: Ensure Destroy is called to free C memory
		doc1.Destroy()
		doc1 = nil // Help GC

		doc2.Destroy()
		doc2 = nil // Help GC
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

// compareMaps recursively compares two maps.
// Note: This is a basic comparison and might need to be more robust for complex cases
// (e.g., order of elements in slices if that matters, deeper type checks).
func compareMaps(m1, m2 map[string]interface{}) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v1 := range m1 {
		v2, ok := m2[k]
		if !ok {
			return false
		}
		if !deepCompare(v1, v2) {
			return false
		}
	}
	return true
}

func deepCompare(v1, v2 interface{}) bool {
	if v1 == nil && v2 == nil {
		return true
	}
	if v1 == nil || v2 == nil {
		return false
	}

	// Handle float64 specifically due to potential precision issues if converting from interface{}
	// If original values were int64 but CRDT stores as float64, comparison might fail.
	// This depends on how Yrs handles numbers internally and how ToJSON serializes them.
	// For this test, we'll assume numbers are either float64 or int64/int.

	switch val1 := v1.(type) {
	case map[string]interface{}:
		val2, ok := v2.(map[string]interface{})
		if !ok || !compareMaps(val1, val2) {
			return false
		}
	case []interface{}:
		val2, ok := v2.([]interface{})
		if !ok || len(val1) != len(val2) {
			return false
		}
		for i := range val1 {
			if !deepCompare(val1[i], val2[i]) {
				return false
			}
		}
	case float64:
		// Yrs might store all numbers as float64.
		// Handle comparison if v2 is an integer type that got converted.
		switch val2 := v2.(type) {
		case float64:
			if val1 != val2 {
				return false
			}
		case int:
			if val1 != float64(val2) {
				return false
			}
		case int64:
			if val1 != float64(val2) {
				return false
			}
		default:
			return false // Type mismatch or unhandled numeric type
		}
	case int64:
		// Handle comparison if v2 is float64
		switch val2 := v2.(type) {
		case int64:
			if val1 != val2 {
				return false
			}
		case float64:
			if float64(val1) != val2 {
				return false
			}
		case int: // json.Unmarshal might produce int for smaller numbers
			if val1 != int64(val2) {
				return false
			}
		default:
			return false // Type mismatch
		}
	case int: // json.Unmarshal might produce int for smaller numbers
		switch val2 := v2.(type) {
		case int:
			if val1 != val2 {
				return false
			}
		case int64:
			if int64(val1) != val2 {
				return false
			}
		case float64:
			if float64(val1) != val2 {
				return false
			}
		default:
			return false // Type mismatch
		}
	default:
		if v1 != v2 {
			return false
		}
	}
	return true
}
