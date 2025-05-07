//go:build cgo

// Package autosync provides JSON-Patch-based synchronization with a yrs-backed CRDT.
package autosync

/*
// Common CFLAGS for all supported platforms
#cgo CFLAGS: -I${SRCDIR}/../yrs_package/include

// Platform-specific LDFLAGS
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/x86_64-unknown-linux-gnu -lyrs -ldl -lm
#cgo linux,arm64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/aarch64-unknown-linux-gnu -lyrs -ldl -lm
#cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/x86_64-apple-darwin -lyrs
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/aarch64-apple-darwin -lyrs
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/x86_64-pc-windows-gnu -lyrs -lws2_32 -luserenv -lbcrypt

// Common includes for all platforms
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
	"strconv"
	"strings"
	"unsafe"

	"github.com/snorwin/jsonpatch"
)

type Doc struct {
	yDoc *C.YDoc
}

func NewDoc() *Doc {
	d := &Doc{
		yDoc: C.ydoc_new(),
	}
	rootKey := C.CString("root")
	defer C.free(unsafe.Pointer(rootKey))

	C.ymap(d.yDoc, rootKey) // create root map
	return d
}

// Destroy frees the underlying Yrs document. MUST be called when the Doc is no longer needed to prevent memory leaks.
func (d *Doc) Destroy() {
	// Do we need to call ydoc_clear as well?
	C.ydoc_destroy(d.yDoc)
}

// ToJSON serializes the current state of the YDoc root map to a Go map.
func (d *Doc) ToJSON() (map[string]interface{}, error) {
	txn := C.ydoc_read_transaction(d.yDoc)
	if txn == nil {
		return nil, errors.New("failed to create read transaction")
	}
	defer C.ytransaction_commit(txn)

	rootKey := C.CString("root")
	defer C.free(unsafe.Pointer(rootKey))

	rootBranch := C.ytype_get(txn, rootKey)
	if rootBranch == nil {
		// This might happen if the root map wasn't created, though NewDoc ensures it.
		return nil, errors.New("root map not found")
	}

	cJsonString := C.ybranch_json(rootBranch, txn)
	if cJsonString == nil {
		// ybranch_json might return nil if the branch type can't be represented as JSON
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

	if result == nil {
		return make(map[string]interface{}), nil
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

// Helper to navigate the YDoc structure based on JSON Pointer path segments.
// Returns the parent Branch, the final key/index, and a slice of C.YOutput pointers
// that were generated during navigation and need to be freed by the caller.
func navigateToParent(txn *C.YTransaction, rootMap *C.Branch, pathSegments []string) (*C.Branch, interface{}, []*C.YOutput, error) {
	parent := rootMap
	if parent == nil {
		return nil, nil, nil, errors.New("navigateToParent received nil rootMap")
	}
	if len(pathSegments) == 0 {
		return nil, nil, nil, errors.New("operation cannot target root directly, must specify key")
	}

	outputsToFree := []*C.YOutput{}
	// Helper function to clean up allocated outputs in case of error during navigation
	cleanupOnError := func(err error) (*C.Branch, interface{}, []*C.YOutput, error) {
		for _, outputPtr := range outputsToFree {
			if outputPtr != nil {
				C.youtput_destroy(outputPtr)
			}
		}
		return nil, nil, nil, err
	}

	parentPathSegments := pathSegments[:len(pathSegments)-1]
	lastSegmentStr := pathSegments[len(pathSegments)-1]

	for _, segmentStr := range parentPathSegments {
		parentKind := C.ytype_kind(parent)

		var nextParentOutput *C.YOutput = nil // Use YOutput* to handle potential NULL

		if parentKind == C.Y_MAP {
			segmentC := C.CString(segmentStr)
			if segmentC == nil {
				return cleanupOnError(fmt.Errorf("failed to allocate C string for path segment '%s'", segmentStr))
			}
			defer C.free(unsafe.Pointer(segmentC))
			nextParentOutput = C.ymap_get(parent, txn, segmentC)

			if nextParentOutput == nil {
				return cleanupOnError(fmt.Errorf("path segment '%s' not found in map", segmentStr))
			}

		} else if parentKind == C.Y_ARRAY {
			index64, err := strconv.ParseUint(segmentStr, 10, 32)
			if err != nil {
				return cleanupOnError(fmt.Errorf("invalid array index '%s' in path: %w", segmentStr, err))
			}
			index := C.uint32_t(index64)
			arrayLen := C.yarray_len(parent)
			if index >= arrayLen {
				return cleanupOnError(fmt.Errorf("array index %d out of bounds (len %d) for segment '%s'", index, arrayLen, segmentStr))
			}
			nextParentOutput = C.yarray_get(parent, txn, index)

			if nextParentOutput == nil {
				return cleanupOnError(fmt.Errorf("failed to get element at index %d for segment '%s'", index, segmentStr))
			}

		} else {
			return cleanupOnError(fmt.Errorf("cannot navigate through non-container type at path segment '%s' (parent kind: %d)", segmentStr, parentKind))
		}

		// Successfully got nextParentOutput, add it to the list to be freed later by the caller
		outputsToFree = append(outputsToFree, nextParentOutput)

		outputTag := nextParentOutput.tag
		var nextParentBranch *C.Branch = nil

		if outputTag == C.Y_MAP {
			nextParentBranch = C.youtput_read_ymap(nextParentOutput)
		} else if outputTag == C.Y_ARRAY {
			nextParentBranch = C.youtput_read_yarray(nextParentOutput)
		} else {
			return cleanupOnError(fmt.Errorf("path segment '%s' resolves to a non-container type (tag: %d)", segmentStr, outputTag))
		}

		if nextParentBranch == nil {
			return cleanupOnError(fmt.Errorf("failed to resolve branch pointer for segment '%s' despite correct tag", segmentStr))
		}
		parent = nextParentBranch // Move to the next level
	}

	parentKind := C.ytype_kind(parent)
	if parentKind == C.Y_MAP {
		return parent, lastSegmentStr, outputsToFree, nil // Return string key
	} else if parentKind == C.Y_ARRAY {
		index64, err := strconv.ParseUint(lastSegmentStr, 10, 32)
		if err != nil {
			// Check for '-' which is valid for append in JSON patch 'add' for arrays
			if lastSegmentStr == "-" {
				return parent, "-", outputsToFree, nil
			}
			return cleanupOnError(fmt.Errorf("invalid array index '%s' for final path segment: %w", lastSegmentStr, err))
		}
		return parent, C.uint32_t(index64), outputsToFree, nil
	} else {
		return cleanupOnError(fmt.Errorf("final parent navigated to is not a map or array (kind: %d)", parentKind))
	}
}

func applyOp(txn *C.YTransaction, rootBranch *C.Branch, op jsonpatch.JSONPatch) error {
	var allocations []cAllocation
	defer func() { freeAllocations(allocations) }()

	// --- Handle Root Operation ---
	if op.Path == "" {
		switch op.Operation {
		case "replace":
			valuesToAdd, ok := op.Value.(map[string]interface{})
			if !ok {
				if op.Value == nil {
					valuesToAdd = make(map[string]interface{})
				} else {
					return fmt.Errorf("operation (replace %s): value for root replacement must be a map (or nil), got %T", op.Path, op.Value)
				}
			}

			// Clear the existing root map
			C.ymap_remove_all(rootBranch, txn)

			// Insert new values
			for key, value := range valuesToAdd {
				yInput, err := buildYInputRecursive(value, &allocations)
				if err != nil {
					return fmt.Errorf("operation (replace %s): failed to build YInput for key '%s': %w", op.Path, key, err)
				}

				keyC := C.CString(key)
				if keyC == nil {
					return fmt.Errorf("operation (replace %s): failed to allocate C string for map key '%s'", op.Path, key)
				}
				defer C.free(unsafe.Pointer(keyC))

				C.ymap_insert(rootBranch, txn, keyC, &yInput)
			}
			return nil // Root replacement successful

		case "add":
			valuesToAdd, ok := op.Value.(map[string]interface{})
			if !ok {
				return fmt.Errorf("operation (add %s): value for root addition must be a map, got %T", op.Path, op.Value)
			}

			// Insert/Update values
			for key, value := range valuesToAdd {
				yInput, err := buildYInputRecursive(value, &allocations)
				if err != nil {
					return fmt.Errorf("operation (add %s): failed to build YInput for key '%s': %w", op.Path, key, err)
				}

				keyC := C.CString(key)
				if keyC == nil {
					return fmt.Errorf("operation (add %s): failed to allocate C string for map key '%s'", op.Path, key)
				}
				defer C.free(unsafe.Pointer(keyC))

				C.ymap_insert(rootBranch, txn, keyC, &yInput) // ymap_insert adds or updates
			}
			return nil // Root addition successful

		default:
			return fmt.Errorf("operation (%s %s): only 'replace' or 'add' operations are supported for the root object", op.Operation, op.Path)
		}
	}

	// --- Handle Non-Root Operation ---
	// Pointer paths start with "/", split and remove the first empty element.
	pathSegments := strings.Split(op.Path, "/")
	if len(pathSegments) > 0 && pathSegments[0] == "" {
		pathSegments = pathSegments[1:]
	} else { // Handle non-empty paths that don't start with / (technically invalid JSON Pointer?)
		// This case should ideally not happen if op.Path != "" but doesn't start with "/"
		return fmt.Errorf("invalid path format '%s', must start with '/'", op.Path)
	}

	// --- Navigate to Parent ---
	parentBranch, targetKeyOrIndex, navigationOutputsToDestroy, err := navigateToParent(txn, rootBranch, pathSegments)
	if err != nil {
		// navigateToParent already cleaned up its outputs on error
		return fmt.Errorf("operation (%s %s): navigation failed: %w", op.Operation, op.Path, err)
	}
	defer func() {
		for _, outputPtr := range navigationOutputsToDestroy {
			if outputPtr != nil {
				C.youtput_destroy(outputPtr)
			}
		}
	}()

	parentKind := C.ytype_kind(parentBranch)

	switch op.Operation {
	case "add":
		yInput, err := buildYInputRecursive(op.Value, &allocations) // Pass the op-specific allocations slice
		if err != nil {
			return fmt.Errorf("operation (add %s): failed to build YInput for value: %w", op.Path, err)
		}

		if parentKind == C.Y_MAP {
			mapKey, ok := targetKeyOrIndex.(string)
			if !ok {
				return fmt.Errorf("operation (add %s): expected string map key, got %T", op.Path, targetKeyOrIndex)
			}
			mapKeyC := C.CString(mapKey)
			if mapKeyC == nil {
				return fmt.Errorf("operation (add %s): failed to allocate C string for map key '%s'", op.Path, mapKey)
			}
			defer C.free(unsafe.Pointer(mapKeyC))

			C.ymap_insert(parentBranch, txn, mapKeyC, &yInput)

		} else if parentKind == C.Y_ARRAY {
			targetIndex := C.uint32_t(0)
			arrayLen := C.yarray_len(parentBranch)

			// Handle different index types from navigation
			switch idx := targetKeyOrIndex.(type) {
			case uint32:
				targetIndex = C.uint32_t(idx)
			case C.uint32_t:
				targetIndex = idx
			case string:
				if idx == "-" {
					// Append case:
					targetIndex = arrayLen
				} else {
					return fmt.Errorf("operation (add %s): invalid array index representation '%v'", op.Path, idx)
				}
			default:
				return fmt.Errorf("operation (add %s): unexpected type for array index %T", op.Path, targetKeyOrIndex)
			}

			if targetIndex > arrayLen { // Add allows insertion at the end (index == len)
				return fmt.Errorf("operation (add %s): index %d out of bounds for array insert (len %d)", op.Path, targetIndex, arrayLen)
			}

			C.yarray_insert_range(parentBranch, txn, targetIndex, &yInput, 1)

		} else {
			return fmt.Errorf("operation (add %s): parent is not a map or array (kind %d)", op.Path, parentKind)
		}

	case "remove":
		if parentKind == C.Y_MAP {
			mapKey, ok := targetKeyOrIndex.(string)
			if !ok {
				return fmt.Errorf("operation (remove %s): expected string map key, got %T", op.Path, targetKeyOrIndex)
			}
			mapKeyC := C.CString(mapKey)
			if mapKeyC == nil {
				return fmt.Errorf("operation (remove %s): failed to allocate C string for map key '%s'", op.Path, mapKey)
			}
			removed := C.ymap_remove(parentBranch, txn, mapKeyC) // 0 if not found, 1 if found
			defer C.free(unsafe.Pointer(mapKeyC))

			if removed == 0 {
				return fmt.Errorf("operation (remove %s): key '%s' not found in map", op.Path, mapKey)
			}
		} else if parentKind == C.Y_ARRAY {
			targetIndex, ok := targetKeyOrIndex.(C.uint32_t)
			if !ok {
				return fmt.Errorf("operation (remove %s): expected numeric array index (C.uint32_t), got %T", op.Path, targetKeyOrIndex)
			}
			arrayLen := C.yarray_len(parentBranch)
			if targetIndex >= arrayLen {
				return fmt.Errorf("operation (remove %s): index %d out of bounds for array remove (len %d)", op.Path, targetIndex, arrayLen)
			}
			C.yarray_remove_range(parentBranch, txn, targetIndex, 1)
		} else {
			return fmt.Errorf("operation (remove %s): parent is not a map or array (kind %d)", op.Path, parentKind)
		}

	case "replace":
		yInput, err := buildYInputRecursive(op.Value, &allocations)
		if err != nil {
			return fmt.Errorf("operation (replace %s): failed to build YInput for value: %w", op.Path, err)
		}

		if parentKind == C.Y_MAP {
			mapKey, ok := targetKeyOrIndex.(string)
			if !ok {
				return fmt.Errorf("operation (replace %s): expected string map key, got %T", op.Path, targetKeyOrIndex)
			}
			mapKeyC := C.CString(mapKey)
			if mapKeyC == nil {
				return fmt.Errorf("operation (replace %s): failed to allocate C string for map key '%s'", op.Path, mapKey)
			}
			defer C.free(unsafe.Pointer(mapKeyC))
			// Use a temporary output to check existence without modifying allocations list yet
			existingOutput := C.ymap_get(parentBranch, txn, mapKeyC)
			if existingOutput == nil {
				return fmt.Errorf("operation (replace %s): key '%s' not found in map for replacement", op.Path, mapKey)
			}
			C.youtput_destroy(existingOutput) // Destroy the temporary output
			C.ymap_insert(parentBranch, txn, mapKeyC, &yInput)

		} else if parentKind == C.Y_ARRAY {
			targetIndex, ok := targetKeyOrIndex.(C.uint32_t)
			if !ok {
				return fmt.Errorf("operation (replace %s): expected numeric array index (C.uint32_t), got %T", op.Path, targetKeyOrIndex)
			}
			arrayLen := C.yarray_len(parentBranch)
			if targetIndex >= arrayLen {
				return fmt.Errorf("operation (replace %s): index %d out of bounds for array replace (len %d)", op.Path, targetIndex, arrayLen)
			}
			// Yjs doesn't have replace, so remove then insert
			C.yarray_remove_range(parentBranch, txn, targetIndex, 1)
			C.yarray_insert_range(parentBranch, txn, targetIndex, &yInput, 1)
		} else {
			return fmt.Errorf("operation (replace %s): parent is not a map or array (kind %d)", op.Path, parentKind)
		}

	default:
		// move, copy, and test are not generated by jsonpatch, can ignore
		return fmt.Errorf("operation (%s %s): unsupported operation type '%s'", op.Operation, op.Path, op.Operation)
	}

	return nil
}

// ApplyOperations applies a list of JSON Patch operations to this document.
func (d *Doc) ApplyOperations(patchList jsonpatch.JSONPatchList) error {
	txn := C.ydoc_write_transaction(d.yDoc, 0, nil)
	if txn == nil {
		return errors.New("failed to create write transaction")
	}
	// We must commit, even if errors occur mid-way, to avoid transaction leaks in Yrs.
	defer C.ytransaction_commit(txn)

	rootKeyC := C.CString("root")
	if rootKeyC == nil {
		return errors.New("failed to allocate C string for root key")
	}
	defer C.free(unsafe.Pointer(rootKeyC))

	rootBranch := C.ytype_get(txn, rootKeyC)
	if rootBranch == nil {
		// This shouldn't happen if NewDoc worked correctly.
		return errors.New("root map not found in YDoc")
	}
	if C.ytype_kind(rootBranch) != C.Y_MAP {
		return errors.New("root Yrs object is not a map")
	}

	for _, op := range patchList.List() {
		err := applyOp(txn, rootBranch, op)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Doc) GetState() (map[string]interface{}, error) {
	return d.ToJSON()
}

// UpdateToState synchronizes the document to match newState, returning the applied patches.
func UpdateToState(d *Doc, newState map[string]interface{}) (jsonpatch.JSONPatchList, error) {
	currentState, err := d.GetState()
	if err != nil {
		return jsonpatch.JSONPatchList{}, fmt.Errorf("failed to get current state: %w", err)
	}

	patch, err := jsonpatch.CreateJSONPatch(newState, currentState)
	if err != nil {
		return jsonpatch.JSONPatchList{}, fmt.Errorf("failed to create JSON patch: %w", err)
	}

	err = d.ApplyOperations(patch)
	if err != nil {
		fmt.Printf("failed to apply JSON patch operations:\n%+v\n", patch)
		return jsonpatch.JSONPatchList{}, fmt.Errorf("failed to apply JSON patch operations: %w", err)
	}

	return patch, nil
}

// GetStateVector serializes the entire document state into a byte slice using Yrs update format v1.
// This byte slice can be used later with ApplyStateVector to restore the document.
func (d *Doc) GetStateVector() ([]byte, error) {
	txn := C.ydoc_read_transaction(d.yDoc)
	if txn == nil {
		return nil, errors.New("GetStateVector: failed to create read transaction")
	}
	defer C.ytransaction_commit(txn) // Must commit even read transactions

	var updateLen C.uint32_t
	// Passing nil state vector encodes the whole document
	updateDataC := C.ytransaction_state_diff_v1(txn, nil, 0, &updateLen)
	if updateDataC == nil {
		return nil, errors.New("GetStateVector: ytransaction_state_diff_v1 returned nil")
	}
	defer C.ybinary_destroy(updateDataC, updateLen)

	if updateLen == 0 {
		return []byte{}, nil
	}

	// Copy the C data into a Go byte slice, can destroy binary after this
	goData := C.GoBytes(unsafe.Pointer(updateDataC), C.int(updateLen))

	return goData, nil
}

// ApplyStateVector applies a previously saved state (obtained via GetStateVector) to the document,
// overwriting its current content. It uses Yrs update format v1.
func (d *Doc) ApplyStateVector(stateData []byte) error {
	txn := C.ydoc_write_transaction(d.yDoc, 0, nil)
	if txn == nil {
		return errors.New("ApplyStateVector: failed to create write transaction")
	}
	// Must commit to apply changes and avoid leaks, even if apply fails midway.
	defer C.ytransaction_commit(txn)

	stateDataC := C.CBytes(stateData)
	if stateDataC == nil {
		return errors.New("ApplyStateVector: failed to allocate C memory for state data")
	}
	defer C.free(stateDataC)

	stateDataLen := C.uint32_t(len(stateData))

	errorCode := C.ytransaction_apply(txn, (*C.char)(stateDataC), stateDataLen)

	if errorCode != 0 {
		return fmt.Errorf("ApplyStateVector: ytransaction_apply failed with error code %d", errorCode)
	}

	return nil
}
