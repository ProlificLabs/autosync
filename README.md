# Yrs-Go Bindings with AutoSync

This project provides Go bindings for the [Yrs](https://github.com/y-crdt/y-crdt/tree/main/yrs) CRDT library, enabling JSON Patch-based synchronization for Go applications. The core component is the `Doc` Go type, which wraps a Yrs document and exposes methods for manipulation and state management.

The project is set up to build static C-compatible libraries from the Rust `yffi` crate for multiple target architectures, which are then consumed by the Go package using `cgo`.

## Features

*   **`Doc` Go Type**: A Go struct that manages an underlying Yrs document.
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

### 3.1. Initialize Git Submodules
If the project uses Git submodules (e.g., for `thirdParty/y-crdt`), you'll need to initialize and update them:
```bash
git submodule update --init --recursive
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

3.  **Run Go Package Tests**:
    ```bash
    make build_go
    ```
    This depends on `make yrs` and then runs the Go tests for the `autosync` package (`go test ./autosync/... -v`), linking against the host architecture's static library.

4.  **Build All (Alias for `build_go`)**:
    ```bash
    make all
    ```

**Typical Build Workflow:**
```bash
make clean
make yrs      # Prepare library artifacts
make build_go # Run tests for the autosync package
```

## Using the `autosync` Go Package

The `autosync` package (defined in the root directory) provides the `Doc` type.

### Integration Steps:

1.  **Ensure `cgo` is Enabled**: `cgo` is required for Go to interface with C libraries. It's enabled by default but ensure `CGO_ENABLED=1` if you've changed it.

2.  **Import the Package**:
    Assuming this module (`github.com/ProlificLabs/autosync`) is a dependency:
    ```go
    import (
        "fmt"
        "github.com/ProlificLabs/autosync" 
    )
    ```
    If using it locally, you might use a `replace` directive in the consuming module's `go.mod` file.

### Key `Doc` Functions:

*   **`d := autosync.NewDoc()`**: Creates a new `Doc`.
*   **`d.Destroy()`**: Frees the underlying Yrs C resources. **Crucial to call this** when done to prevent memory leaks.
*   **`jsonState, err := d.ToJSON()`**: Gets the current document state as `map[string]interface{}`.
*   **`err := d.ApplyOperations(patchList)`**: Applies a `jsonpatch.JSONPatchList` to the document.
*   **`stateVec, err := d.GetStateVector()`**: Serializes the document state to a byte slice.
*   **`err := d.ApplyStateVector(stateVec)`**: Applies a previously obtained state vector to the document.
*   **`appliedPatches, err := d.UpdateToState(newStateMap)`**: Calculates the JSON patch needed to transform the document's current state to `newStateMap`, applies it, and returns the patches.

### Example Usage Snippet:

This demonstrates basic usage within a Go program. You would integrate this logic into your application where needed.

```go
package main

import (
	"fmt"
	"log"

	"github.com/ProlificLabs/autosync"
	"github.com/snorwin/jsonpatch" // For creating patch objects
)

func main() {
	doc := autosync.NewDoc()
	// IMPORTANT: Ensure Destroy is called eventually, e.g., using defer in a relevant scope
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
	applied, err := doc.UpdateToState(newState)
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
	doc2 := autosync.NewDoc()
	defer doc2.Destroy()
	err = doc2.ApplyStateVector(stateVec)
	if err != nil {
		log.Fatal("Failed to apply state vector to doc2:", err)
	}
	stateDoc2, _ := doc2.ToJSON()
	fmt.Println("State of doc2 from vector:", stateDoc2) // Should match finalState
}

```

## Directory Structure

*   `./Makefile`: Main build script.
*   `./go.mod`, `./go.sum`: Go module definition files.
*   `./autosync.go`, `./autosync_test.go`: The Go package source and test files.
*   `./.cargo/config.toml`: Cargo configuration for cross-compilation linkers.
*   `./yrs_package/`: Output directory created by `make yrs`.
    *   `./yrs_package/include/libyrs.h`: The generated C header file.
    *   `./yrs_package/lib/<target_triple>/libyrs.a`: The compiled static libraries for each architecture.
*   `./thirdParty/y-crdt/`: Submodule or vendored code for the Yrs Rust library.
    *   `./thirdParty/y-crdt/yffi/`: The Rust FFI crate that is compiled.