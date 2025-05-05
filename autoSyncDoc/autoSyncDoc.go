//go:build cgo

// Package autosyncdoc provides JSON-Patch-based synchronization with a yrs-backed CRDT.
package autosyncdoc

/*
#cgo CFLAGS: -I../yrs
#cgo LDFLAGS: -L../yrs -lyrs
#include <libyrs.h>
#include <stdlib.h> // Required for C.free
#include <string.h> // Required for C.memcpy
*/
import "C"
import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"unsafe"
)

type AutoSyncDoc struct {
	yDoc *C.YDoc
}

func NewAutoSyncDoc() *AutoSyncDoc {
	autoSyncDoc := &AutoSyncDoc{
		yDoc: C.ydoc_new(),
	}
	rootKey := C.CString("root")
	defer C.free(unsafe.Pointer(rootKey))

	C.ymap(autoSyncDoc.yDoc, rootKey) // create root map
	return autoSyncDoc
}

// Destroy frees the underlying Yrs document. MUST be called when the AutoSyncDoc is no longer needed to prevent memory leaks.
func (autoSyncDoc *AutoSyncDoc) Destroy() {
	// Do we need to call ydoc_clear as well?
	C.ydoc_destroy(autoSyncDoc.yDoc)
}

// ToJSON serializes the current state of the YDoc root map to a Go map.
func (autoSyncDoc *AutoSyncDoc) ToJSON() (map[string]interface{}, error) {
	txn := C.ydoc_read_transaction(autoSyncDoc.yDoc)
	if txn == nil {
		return nil, errors.New("failed to create read transaction")
	}
	defer C.ytransaction_commit(txn)

	rootKey := C.CString("root")
	defer C.free(unsafe.Pointer(rootKey))

	rootBranch := C.ytype_get(txn, rootKey)
	if rootBranch == nil {
		// This might happen if the root map wasn't created, though NewAutoSyncDoc ensures it.
		return nil, errors.New("root map not found")
	}

	cJsonString := C.ybranch_json(rootBranch, txn)
	if cJsonString == nil {
		// ybranch_json might return null if the branch type can't be represented as JSON
		// or if there's an internal error.
		return nil, errors.New("failed to get JSON representation from ybranch_json")
	}
	defer C.ystring_destroy(cJsonString)

	goJsonString := C.GoString(cJsonString)

	var result map[string]interface{}
	err := json.Unmarshal([]byte(goJsonString), &result)
	if err != nil {
		return nil, errors.New("failed to unmarshal JSON from YDoc: " + err.Error())
	}

	return result, nil
}

// Represents allocated C memory that needs to be freed later.
type cAllocation struct {
	ptr  unsafe.Pointer
	kind string // "string", "byteArray", "inputArray", "keysArray" for debugging/clarity
}

// buildYInputRecursive converts a Go value into a C.YInput structure, suitable for use
// with Yrs insertion functions. It recursively handles nested slices and maps.
// IMPORTANT: This function allocates C memory (strings, arrays for nested structures).
// The caller is responsible for freeing ALL pointers added to the `allocations` slice
// AFTER the C.YInput has been used by the Yrs C API function (e.g., ymap_insert).
func buildYInputRecursive(value interface{}, allocations *[]cAllocation) (C.YInput, error) {
	if value == nil {
		return C.yinput_null(), nil
	}

	val := reflect.ValueOf(value)
	switch val.Kind() {
	case reflect.Bool:
		b := val.Bool()
		if b {
			return C.yinput_bool(C.Y_TRUE), nil
		}
		return C.yinput_bool(C.Y_FALSE), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return C.yinput_long(C.longlong(val.Int())), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := val.Uint()
		// Check for overflow if converting uint64 to int64
		if u > math.MaxInt64 {
			return C.YInput{}, fmt.Errorf("uint64 value %d overflows int64", u)
		}
		return C.yinput_long(C.longlong(u)), nil // C.longlong is int64_t
	case reflect.Float32, reflect.Float64:
		return C.yinput_float(C.double(val.Float())), nil
	case reflect.String:
		goStr := val.String()
		cStr := C.CString(goStr)
		if cStr == nil {
			// CString can return nil if memory allocation fails
			return C.YInput{}, errors.New("failed to allocate C string")
		}
		*allocations = append(*allocations, cAllocation{ptr: unsafe.Pointer(cStr), kind: "string"})
		return C.yinput_string(cStr), nil
	case reflect.Slice:
		sliceLen := val.Len()
		if sliceLen == 0 {
			// Return YInput for empty YArray
			// Allocate an empty C array pointer for consistency in freeing logic, though Yrs might handle nil.
			// It's safer to provide a valid (even if zero-size allocated) pointer.
			cArrayPtr := C.malloc(1) // Allocate minimal memory
			if cArrayPtr == nil {
				return C.YInput{}, errors.New("failed to allocate C array for empty slice")
			}
			*allocations = append(*allocations, cAllocation{ptr: cArrayPtr, kind: "inputArray"})
			return C.yinput_yarray((*C.YInput)(cArrayPtr), 0), nil
		}

		// 1. Recursively build YInput for each element
		goInputs := make([]C.YInput, sliceLen)
		for i := 0; i < sliceLen; i++ {
			elemInput, err := buildYInputRecursive(val.Index(i).Interface(), allocations)
			if err != nil {
				return C.YInput{}, fmt.Errorf("failed processing slice element %d: %w", i, err)
			}
			goInputs[i] = elemInput
		}

		// 2. Allocate C array and copy Go inputs into it
		inputSize := C.sizeof_YInput                                    // Size of one YInput struct
		cArrayPtr := C.malloc(C.size_t(sliceLen) * C.size_t(inputSize)) // Cast inputSize
		if cArrayPtr == nil {
			return C.YInput{}, fmt.Errorf("failed to allocate C array for %d YInputs", sliceLen)
		}
		*allocations = append(*allocations, cAllocation{ptr: cArrayPtr, kind: "inputArray"})

		// Copy memory - treat goInputs as a C array for memcpy
		// Calculate the correct unsafe pointer to the start of the Go slice data
		goInputsPtr := unsafe.Pointer(&goInputs[0])
		C.memcpy(cArrayPtr, goInputsPtr, C.size_t(sliceLen)*C.size_t(inputSize)) // Cast inputSize

		return C.yinput_yarray((*C.YInput)(cArrayPtr), C.uint32_t(sliceLen)), nil

	case reflect.Map:
		// Ensure keys are strings
		if val.Type().Key().Kind() != reflect.String {
			return C.YInput{}, errors.New("map keys must be strings")
		}

		mapLen := val.Len()
		if mapLen == 0 {
			// Return YInput for empty YMap
			// Provide allocated (but empty) pointers for consistency
			cKeysPtr := C.malloc(1)
			if cKeysPtr == nil {
				return C.YInput{}, errors.New("failed to allocate C array for empty map keys")
			}
			*allocations = append(*allocations, cAllocation{ptr: cKeysPtr, kind: "keysArray"})

			cValuesPtr := C.malloc(1)
			if cValuesPtr == nil {
				return C.YInput{}, errors.New("failed to allocate C array for empty map values")
			}
			*allocations = append(*allocations, cAllocation{ptr: cValuesPtr, kind: "inputArray"})

			return C.yinput_ymap((**C.char)(cKeysPtr), (*C.YInput)(cValuesPtr), 0), nil
		}

		// 1. Recursively build keys and values
		goKeys := make([]*C.char, mapLen)
		goValues := make([]C.YInput, mapLen)
		iter := val.MapRange()
		i := 0
		for iter.Next() {
			k := iter.Key().String()
			v := iter.Value().Interface()

			// Allocate C string for key
			cKey := C.CString(k)
			if cKey == nil {
				return C.YInput{}, fmt.Errorf("failed to allocate C string for map key '%s'", k)
			}
			*allocations = append(*allocations, cAllocation{ptr: unsafe.Pointer(cKey), kind: "string"})
			goKeys[i] = cKey

			// Recursively build value
			valInput, err := buildYInputRecursive(v, allocations)
			if err != nil {
				return C.YInput{}, fmt.Errorf("failed processing map value for key '%s': %w", k, err)
			}
			goValues[i] = valInput
			i++
		}

		// 2. Allocate C arrays and copy Go slices into them
		// Correct size calculation for array of pointers (*C.char)
		// Use unsafe.Sizeof on an element of the slice to get the pointer size
		keyPtrSize := unsafe.Sizeof(goKeys[0])
		cKeysPtr := C.malloc(C.size_t(mapLen) * C.size_t(keyPtrSize)) // Use C.size_t for multiplication result
		if cKeysPtr == nil {
			return C.YInput{}, fmt.Errorf("failed to allocate C array for %d map keys", mapLen)
		}
		*allocations = append(*allocations, cAllocation{ptr: cKeysPtr, kind: "keysArray"})
		// Correct pointer for memcpy source (pointer to first element of Go slice)
		goKeysPtr := unsafe.Pointer(&goKeys[0])
		C.memcpy(cKeysPtr, goKeysPtr, C.size_t(mapLen)*C.size_t(keyPtrSize)) // Use C.size_t for multiplication result

		valueSize := C.sizeof_YInput                                   // Size of one YInput struct
		cValuesPtr := C.malloc(C.size_t(mapLen) * C.size_t(valueSize)) // Cast valueSize
		if cValuesPtr == nil {
			return C.YInput{}, fmt.Errorf("failed to allocate C array for %d map values", mapLen)
		}
		*allocations = append(*allocations, cAllocation{ptr: cValuesPtr, kind: "inputArray"})
		// Correct pointer for memcpy source
		goValuesPtr := unsafe.Pointer(&goValues[0])
		C.memcpy(cValuesPtr, goValuesPtr, C.size_t(mapLen)*C.size_t(valueSize)) // Cast valueSize

		return C.yinput_ymap((**C.char)(cKeysPtr), (*C.YInput)(cValuesPtr), C.uint32_t(mapLen)), nil

	default:
		return C.YInput{}, fmt.Errorf("unsupported kind: %s", val.Kind())
	}
}

// Helper function to free memory allocated during buildYInputRecursive
func freeAllocations(allocations []cAllocation) {
	// fmt.Printf("Freeing %d allocations...\n", len(allocations)) // For debugging
	for i := len(allocations) - 1; i >= 0; i-- { // Free in reverse order potentially?
		alloc := allocations[i]
		// fmt.Printf("  Freeing %s at %p\n", alloc.kind, alloc.ptr) // For debugging
		C.free(alloc.ptr)
	}
}

// Example usage (will need to be integrated into applyOperations later)
func (autoSyncDoc *AutoSyncDoc) AddValue(key string, value interface{}) error {
	txn := C.ydoc_write_transaction(autoSyncDoc.yDoc, 0, nil)
	if txn == nil {
		return errors.New("failed to create write transaction")
	}
	defer C.ytransaction_commit(txn) // Rollbacks not supported, must commit to avoid memory leaks

	rootKeyC := C.CString("root")
	if rootKeyC == nil {
		return errors.New("failed to allocate C string for root key")
	}
	defer C.free(unsafe.Pointer(rootKeyC))

	rootBranch := C.ytype_get(txn, rootKeyC)
	if rootBranch == nil {
		return errors.New("root map not found")
	}

	// Check if rootBranch is actually a map (optional but good practice)
	if C.ytype_kind(rootBranch) != C.Y_MAP {
		return errors.New("root object is not a map")
	}

	// Slice to track all C allocations for this operation
	var allocations []cAllocation
	// Defer cleanup immediately after declaring the slice to handle potential errors.
	defer func() { freeAllocations(allocations) }()

	yInput, err := buildYInputRecursive(value, &allocations)
	if err != nil {
		return fmt.Errorf("failed to build YInput: %w", err)
	}

	targetKeyC := C.CString(key)
	if targetKeyC == nil {
		return errors.New("failed to allocate C string for target key")
	}
	defer C.free(unsafe.Pointer(targetKeyC))

	// Perform the insertion
	C.ymap_insert(rootBranch, txn, targetKeyC, &yInput)

	return nil
}
