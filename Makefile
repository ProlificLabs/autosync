# Makefile for building Yrs C bindings (static libs for multi-arch) and preparing them for Go CGO

# Variables
YCRDT_DIR := thirdParty/y-crdt
YFFI_DIR := $(YCRDT_DIR)/yffi
CARGO_BASE_TARGET_DIR := $(YCRDT_DIR)/target
# Base directory for Rust target-specific builds

PACKAGE_DIR := yrs_package
INCLUDE_DIR := $(PACKAGE_DIR)/include
LIB_BASE_DIR := $(PACKAGE_DIR)/lib
# Base directory for storing arch-specific libs

# Go binary name (for the example build)
GO_BINARY := autoSync

# Rust Target Triples
TARGET_TRIPLES := x86_64-unknown-linux-gnu
TARGET_TRIPLES += aarch64-unknown-linux-gnu
TARGET_TRIPLES += x86_64-apple-darwin
TARGET_TRIPLES += aarch64-apple-darwin
TARGET_TRIPLES += x86_64-pc-windows-gnu
    # Add more targets as needed, e.g. x86_64-pc-windows-msvc

# Generated header source and destination
H_GENERATED_SRC := $(YFFI_DIR)/libyrs.h
H_DEST := $(INCLUDE_DIR)/libyrs.h

# Detect OS for platform-specific commands (used by patch_header)
UNAME_S := $(shell uname -s)

# Phony targets (targets that don't represent files)
.PHONY: all build_go yrs build_rust_all copy_static_libs_all gen_header copy_header patch_header clean
.PHONY: $(foreach triple,$(TARGET_TRIPLES),build_rust_$(triple) copy_lib_$(triple))

# Default target
all: build_go

# Main target to prepare all static libraries and headers for the package
# Build the Go binary using the generated C bindings
build_go: yrs
	@echo "Building Go binary '$(GO_BINARY)'..."
	@echo "Note: Ensure your Go files have correct cgo build tags and LDFLAGS pointing to static libraries in $(LIB_BASE_DIR)/<GOOS>_<GOARCH_OR_TRIPLE>/"
	@go build -o $(GO_BINARY) .
	@# Check if build succeeded
	@test -f $(GO_BINARY) || (echo "Error: Go build failed for $(GO_BINARY)."; exit 1)
	@echo "Go binary '$(GO_BINARY)' built successfully."

# Depends on patching the header and setting the install name (if needed)
yrs: copy_static_libs_all patch_header
	@echo "Yrs package artifacts (static libraries and header) are ready in $(PACKAGE_DIR)/"

# Template for building Rust for a specific target triple
# $(1) is the target triple
define RUST_BUILD_TARGET_RULE
build_rust_$(1):
	@echo "Building Yrs Rust static library for target: $(1)..."
	@(cd $(YCRDT_DIR) && cargo build -p yffi --release --target $(1))
	@# Check if the static library (.a) was built
	@test -f "$(CARGO_BASE_TARGET_DIR)/$(1)/release/deps/libyrs.a" || \
	  (echo "Error: libyrs.a not found for target $(1) at '$(CARGO_BASE_TARGET_DIR)/$(1)/release/deps/libyrs.a'. Ensure 'staticlib' crate-type in yffi/Cargo.toml."; exit 1)
	@echo "Successfully built libyrs.a for target: $(1)"
endef

# Instantiate the Rust build rule for each target triple
$(foreach triple,$(TARGET_TRIPLES),$(eval $(call RUST_BUILD_TARGET_RULE,$(triple))))

# Aggregate target to build all Rust static libraries
build_rust_all: $(foreach triple,$(TARGET_TRIPLES),build_rust_$(triple))
	@echo "All Rust static libraries built successfully."

# Template for copying the static library for a specific target triple
# $(1) is the target triple
define COPY_LIB_TARGET_RULE
copy_lib_$(1): build_rust_$(1)
	@echo "Copying libyrs.a for target $(1) to $(LIB_BASE_DIR)/$(1)/..."
	@mkdir -p "$(LIB_BASE_DIR)/$(1)"
	@cp "$(CARGO_BASE_TARGET_DIR)/$(1)/release/deps/libyrs.a" "$(LIB_BASE_DIR)/$(1)/libyrs.a"
	@# Handle potential .dll.a suffix for Windows MinGW target if it occurs, and general check
	@if [ "$(1)" = "x86_64-pc-windows-gnu" ] && [ -f "$(CARGO_BASE_TARGET_DIR)/$(1)/release/deps/libyrs.dll.a" ]; then echo "Found libyrs.dll.a for $(1), renaming to libyrs.a in destination."; mv "$(CARGO_BASE_TARGET_DIR)/$(1)/release/deps/libyrs.dll.a" "$(LIB_BASE_DIR)/$(1)/libyrs.a"; elif [ ! -f "$(LIB_BASE_DIR)/$(1)/libyrs.a" ]; then echo "Warning: '$(LIB_BASE_DIR)/$(1)/libyrs.a' was not found after copy attempt for target $(1)."; fi
	@echo "Copied libyrs.a for target $(1)."
endef

# Instantiate the copy library rule for each target triple
$(foreach triple,$(TARGET_TRIPLES),$(eval $(call COPY_LIB_TARGET_RULE,$(triple))))

# Aggregate target to copy all static libraries
copy_static_libs_all: $(foreach triple,$(TARGET_TRIPLES),copy_lib_$(triple))
	@echo "All static libraries copied to $(LIB_BASE_DIR)/"

# Generate the C header file using cbindgen
gen_header:
	@echo "Generating C header ($(H_GENERATED_SRC)) using cbindgen..."
	@(cd $(YFFI_DIR) && cbindgen --config cbindgen.toml --crate yffi --output libyrs.h)
	@# Check if header was generated
	@test -f "$(H_GENERATED_SRC)" || (echo "Error: '$(H_GENERATED_SRC)' not found. Is cbindgen installed and in PATH?"; exit 1)

# Copy the generated header
copy_header: gen_header
	@echo "Copying generated header to $(INCLUDE_DIR)/..."
	@mkdir -p $(INCLUDE_DIR)
	@cp $(H_GENERATED_SRC) $(H_DEST)

# Patch the copied header file to comment out specific typedefs causing recursive type definitions in go
patch_header: copy_header
	@echo "Patching $(H_DEST)..."
ifeq ($(UNAME_S),Darwin)
	@# Use macOS compatible sed -i ''
	@sed -i '' 's/^typedef YDoc YDoc;/\/\/ typedef YDoc YDoc;/' $(H_DEST)
	@sed -i '' 's/^typedef Branch Branch;/\/\/ typedef Branch Branch;/' $(H_DEST)
else
	@# Use Linux compatible sed -i
	@sed -i 's/^typedef YDoc YDoc;/\/\/ typedef YDoc YDoc;/' $(H_DEST)
	@sed -i 's/^typedef Branch Branch;/\/\/ typedef Branch Branch;/' $(H_DEST)
endif
	@echo "Header patching complete."

# Clean up generated files
clean:
	@echo "Cleaning generated C header file ($(H_GENERATED_SRC))..."
	@rm -f $(H_GENERATED_SRC)
	@echo "Cleaning package directory ($(PACKAGE_DIR)/)..."
	@rm -rf $(PACKAGE_DIR)
	@echo "Cleaning Rust target directories for all specified architectures..."
	$(foreach triple,$(TARGET_TRIPLES), rm -rf $(CARGO_BASE_TARGET_DIR)/$(triple);)
	@# Also clean the host-specific release directory if it exists from previous builds
	@rm -rf $(YCRDT_DIR)/target/release
	@echo "Cleaning Go build output ('$(GO_BINARY)')..."
	@rm -f $(GO_BINARY)
	@echo "Clean complete."

