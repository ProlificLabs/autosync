# Yrs-Go Bindings Test with AutoSyncDoc

This project provides Go bindings for the [Yrs](https://github.com/y-crdt/y-crdt/tree/main/yrs) CRDT library, enabling JSON Patch-based synchronization for Go applications. The core component is the `AutoSyncDoc` Go type, which wraps a Yrs document and exposes methods for manipulation and state management.

The project is set up to build static C-compatible libraries from the Rust `yffi` crate for multiple target architectures, which are then consumed by the Go package using `cgo`.

## Features

*   **`AutoSyncDoc` Go Type**: A Go struct that manages an underlying Yrs document.
*   **JSON Patch Synchronization**: Apply JSON patches to update the document state.
*   **State Serialization**: Get the document state as a JSON-compatible `map[string]interface{}`.
*   **State Vector Management**:
    *   `GetStateVector()`: Serialize the Yrs document state into a compact byte vector (Yrs update format v1).
    *   `ApplyStateVector()`: Restore a document from a previously obtained state vector.
*   **Cross-Platform Static Libraries**: Builds `.a` static libraries for:
    *   Linux x86_64 (`x86_64-unknown-linux-gnu`)
    *   Linux ARM64 (`aarch64-unknown-linux-gnu`)
    *   macOS x86_64 (Intel) (`x86_64-apple-darwin`)
    *   macOS ARM64 (Apple Silicon) (`aarch64-apple-darwin`)
    *   Windows x86_64 (MinGW) (`x86_64-pc-windows-gnu`)

## Build Environment Setup

Setting up the build environment requires Go, Rust, and several tools for cross-compilation if you intend to build for all target architectures.

### 1. Go
Install Go (version 1.18 or later recommended for `cgo` improvements).
*   Follow the official instructions: [golang.org/doc/install](https://golang.org/doc/install)

### 2. Rust
Install Rust using `rustup`.
*   Follow the official instructions: [rustup.rs](https://rustup.rs/)

### 3. Rust Target Toolchains
Add the Rust target toolchains for the architectures you intend to build. The `Makefile` is configured for all five listed above.
```bash
rustup target add x86_64-unknown-linux-gnu
rustup target add aarch64-unknown-linux-gnu
rustup target add x86_64-apple-darwin  # Usually present if on macOS Intel
rustup target add aarch64-apple-darwin  # Usually present if on macOS ARM
rustup target add x86_64-pc-windows-gnu
```

### 4. C Header Generator: `cbindgen`
Install `cbindgen` to generate the C header file (`libyrs.h`) from the Rust code.
```bash
brew install cbindgen
```

### 5. Cross-Compilers (Especially if on macOS building for Linux/Windows)
To build the Rust static libraries for non-native targets (e.g., building for Linux or Windows from macOS), you need appropriate C cross-compiler toolchains. The Rust build process (Cargo) needs these to link the static libraries correctly.

**For macOS users (using Homebrew):**
*   **Linux x86_64**:
    ```bash
    brew tap messense/macos-cross-toolchains
    brew install x86_64-unknown-linux-gnu
    ```
*   **Linux ARM64**:
    ```bash
    # brew tap messense/macos-cross-toolchains
    brew install aarch64-unknown-linux-gnu
    ```
*   **Windows x86_64 (MinGW)**:
    ```bash
    brew install mingw-w64
    ```
    This provides `x86_64-w64-mingw32-gcc` and related tools.

Ensure the installed cross-compilers are in your `PATH`.

## Building the Project

The `Makefile` provides several targets to manage the build process:

1.  **Clean Artifacts**:
    ```bash
    make clean
    ```
    Removes previously built Rust libraries, Go binaries, and the `yrs_package` directory.

2.  **Build Static Libraries and Header**:
    ```bash
    make yrs
    ```
    This is the primary target for preparing the C-compatible artifacts. It will:
    *   Compile the `yffi` Rust crate into `libyrs.a` for all target architectures defined in `TARGET_TRIPLES`.
    *   Copy these static libraries to `yrs_package/lib/<target_triple>/libyrs.a`.
    *   Generate `libyrs.h` using `cbindgen` from `yffi`.
    *   Copy and patch `libyrs.h` into `yrs_package/include/libyrs.h`.

3.  **Build Example Go Binary**:
    ```bash
    make build_go
    ```
    This depends on `make yrs` and then compiles the example Go program (`main.go`, if you create one, or the `autoSyncDoc` package tests) which links against the host architecture's static library.

4.  **Build All (Alias for `build_go`)**:
    ```bash
    make all
    ```

**Typical Build Workflow:**
```bash
make clean
make yrs      # Or 'make all' which includes this
make build_go # If you have a main Go program to test
```

## Using the `autoSyncDoc` Go Package

The `autoSyncDoc` package (located in the `autoSyncDoc/` directory) provides the `AutoSyncDoc` type.

### Integration Steps:

1.  **Ensure `cgo` is Enabled**: `cgo` is required for Go to interface with C libraries. It's enabled by default but ensure `CGO_ENABLED=1` if you've changed it.

2.  **Import the Package**:
    Assuming this project `yrs-bindings-test` is in your `GOPATH` or is a Go module dependency:
    ```go
    import (
        "fmt"
        // Adjust import path as needed, e.g.,
        // "your_module_path/yrs-bindings-test/autoSyncDoc"
        "github.com/yourusername/yrs-bindings-test/autoSyncDoc" 
    )
    ```
    If using it locally as a module, you might replace the `github.com/...` part in `go.mod` with a `replace` directive.

3.  **Linking Against Pre-built Static Libraries**:
    The `autoSyncDoc/autoSyncDoc.go` file itself contains `cgo` directives that demonstrate how to link against the static libraries produced by `make yrs`.
    ```go
    /*
    // Common CFLAGS for all supported platforms
    #cgo CFLAGS: -I${SRCDIR}/../yrs_package/include

    // Platform-specific LDFLAGS
    #cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/x86_64-unknown-linux-gnu -lyrs -ldl -lm
    #cgo linux,arm64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/aarch64-unknown-linux-gnu -lyrs -ldl -lm
    #cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/x86_64-apple-darwin -lyrs
    #cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/aarch64-apple-darwin -lyrs
    #cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../yrs_package/lib/x86_64-pc-windows-gnu -lyrs -lws2_32 -luserenv -lbcrypt

    #include <libyrs.h>
    #include <stdlib.h>
    #include <string.h>
    */
    import "C"
    ```
    *   `CFLAGS: -I${SRCDIR}/../yrs_package/include` tells `cgo` where to find `libyrs.h`. The `${SRCDIR}` variable points to the directory containing the Go source file.
    *   `LDFLAGS` are specified per target OS/architecture combination using Go build tags (e.g., `linux,amd64`). They tell the Go linker:
        *   `-L${SRCDIR}/../yrs_package/lib/<target_triple>`: Where to find the `libyrs.a` file.
        *   `-lyrs`: Link against `libyrs.a`.
        *   Additional flags (e.g., `-ldl`, `-lm` on Linux) link necessary system libraries.

    When you build your Go application that imports `autoSyncDoc`, `go build` (with appropriate `GOOS` and `GOARCH` if cross-compiling) will use these directives to link correctly.

### Key `AutoSyncDoc` Functions:

*   **`asd := autosyncdoc.NewAutoSyncDoc()`**: Creates a new `AutoSyncDoc`.
*   **`asd.Destroy()`**: Frees the underlying Yrs C resources. **Crucial to call this** when done to prevent memory leaks.
*   **`jsonState, err := asd.ToJSON()`**: Gets the current document state as `map[string]interface{}`.
*   **`err := asd.ApplyOperations(patchList)`**: Applies a `jsonpatch.JSONPatchList` to the document.
*   **`stateVec, err := asd.GetStateVector()`**: Serializes the document state to a byte slice.
*   **`err := asd.ApplyStateVector(stateVec)`**: Applies a previously obtained state vector to the document.
*   **`appliedPatches, err := autosyncdoc.UpdateToState(asd, newStateMap)`**: Calculates the JSON patch needed to transform the document's current state to `newStateMap`, applies it, and returns the patches.

### Example Usage Snippet:
```go
package main

import (
	"fmt"
	"log"

	"github.com/snorwin/jsonpatch" // For creating patch objects
	"yrs-bindings-test/autoSyncDoc" // Adjust import path
)

func main() {
	doc := autosyncdoc.NewAutoSyncDoc()
	defer doc.Destroy()

	// Initial state
	initialJSON, _ := doc.ToJSON()
	fmt.Println("Initial state:", initialJSON)

	// Apply an "add" operation
	// Corresponds to: {"op": "add", "path": "/foo", "value": "bar"}
	patch1, err := jsonpatch.ParsePatch([]byte(`[{"op": "add", "path": "/foo", "value": "bar"}]`))
	if err != nil {
		log.Fatal("Failed to parse patch1:", err)
	}
	err = doc.ApplyOperations(patch1)
	if err != nil {
		log.Fatal("Failed to apply patch1:", err)
	}

	state1, _ := doc.ToJSON()
	fmt.Println("State after patch1:", state1) // Should be map[foo:bar]

	// Update to a new state
	newState := map[string]interface{}{
		"foo": "baz",
		"newKey": 123,
	}
	applied, err := autosyncdoc.UpdateToState(doc, newState)
	if err != nil {
		log.Fatal("Failed to update to state:", err)
	}
	fmt.Println("Applied patches for UpdateToState:", applied.String())

	finalState, _ := doc.ToJSON()
	fmt.Println("Final state:", finalState) // Should be map[foo:baz newKey:123]

	// Get state vector
	stateVec, err := doc.GetStateVector()
	if err != nil {
		log.Fatal("Failed to get state vector:", err)
	}
	fmt.Printf("State vector length: %d bytes\n", len(stateVec))

	// Create a new doc and apply state vector
	doc2 := autosyncdoc.NewAutoSyncDoc()
	defer doc2.Destroy()
	err = doc2.ApplyStateVector(stateVec)
	if err != nil {
		log.Fatal("Failed to apply state vector to doc2:", err)
	}
	stateDoc2, _ := doc2.ToJSON()
	fmt.Println("State of doc2 from vector:", stateDoc2) // Should match finalState
}

```
Create a `main.go` with the above content (adjusting the import path for `autoSyncDoc` if necessary) and run `go mod tidy && go run main.go` (after `make yrs` has successfully run).

## Directory Structure

*   `./Makefile`: Main build script.
*   `./.cargo/config.toml`: Cargo configuration for cross-compilation linkers.
*   `./autoSyncDoc/`: Contains the Go package source code (`autoSyncDoc.go`).
*   `./yrs_package/`: Output directory created by `make yrs`.
    *   `./yrs_package/include/libyrs.h`: The generated C header file.
    *   `./yrs_package/lib/<target_triple>/libyrs.a`: The compiled static libraries for each architecture.
*   `./thirdParty/y-crdt/`: Submodule or vendored code for the Yrs Rust library.
    *   `./thirdParty/y-crdt/yffi/`: The Rust FFI crate that is compiled.