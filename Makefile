# Makefile for building Yrs C bindings and preparing them for Go CGO

# Variables
YCRDT_DIR := thirdParty/y-crdt
YFFI_DIR := $(YCRDT_DIR)/yffi
CARGO_TARGET_DIR := $(YCRDT_DIR)/target
RELEASE_DIR := $(CARGO_TARGET_DIR)/release
DEPS_DIR := $(RELEASE_DIR)/deps

OUTPUT_DIR := yrs

# Source files (locations after build/generation)
DYLIB_SRC := $(DEPS_DIR)/libyrs.dylib
A_SRC := $(DEPS_DIR)/libyrs.a
LIB_D_SRC := $(RELEASE_DIR)/libyrs.d
D_SRC := $(DEPS_DIR)/yrs.d
H_GENERATED_SRC := $(YFFI_DIR)/libyrs.h

# Destination files in OUTPUT_DIR (yrs/)
DYLIB_DEST := $(OUTPUT_DIR)/libyrs.dylib
A_DEST := $(OUTPUT_DIR)/libyrs.a
LIB_D_DEST := $(OUTPUT_DIR)/libyrs.d
D_DEST := $(OUTPUT_DIR)/yrs.d
H_DEST := $(OUTPUT_DIR)/libyrs.h

# Detect OS for platform-specific commands
UNAME_S := $(shell uname -s)

# Go binary name
GO_BINARY := autoSync

# Phony targets (targets that don't represent files)
.PHONY: all yrs build_rust copy_libs gen_header copy_header patch_header set_install_name clean

# Default target
all: build_go

# Main target to build everything
# Build the Go binary using the generated C bindings
# CGO_LDFLAGS_ALLOW is needed to allow linking options like -rpath
build_go: yrs
	@echo "Building Go binary..."
	@go build -ldflags="-extldflags='-Wl,-rpath,@executable_path/yrs'" -o $(GO_BINARY) .
	@# Check if build succeeded
	@test -f $(GO_BINARY) || (echo "Error: Go build failed."; exit 1)

# Depends on patching the header and setting the install name (if needed)
yrs: patch_header set_install_name

# Build the Rust library
build_rust:
	@echo "Building Yrs Rust library (yffi package)..."
	@(cd $(YCRDT_DIR) && \
	  cargo build -p yffi --release)
	@# Check if build succeeded
	@test -f $(DYLIB_SRC) || (echo "Error: $(DYLIB_SRC) not found after build. Check Rust build logs."; exit 1)
	@test -f $(A_SRC) || (echo "Error: $(A_SRC) not found after build. Check Rust build logs."; exit 1)

# Copy the built libraries (.dylib, .a, .d)
copy_libs: build_rust
	@echo "Copying Yrs libraries to $(OUTPUT_DIR)/..."
	@mkdir -p $(OUTPUT_DIR)
	@cp $(DYLIB_SRC) $(DYLIB_DEST)
	@cp $(A_SRC) $(A_DEST)
	@cp $(LIB_D_SRC) $(LIB_D_DEST)
	@cp $(D_SRC) $(D_DEST)

# Generate the C header file using cbindgen
gen_header:
	@echo "Generating C header (libyrs.h) using cbindgen..."
	@(cd $(YFFI_DIR) && \
	  cbindgen --config cbindgen.toml --crate yffi --output libyrs.h)
	@# Check if header was generated
	@test -f $(H_GENERATED_SRC) || (echo "Error: $(H_GENERATED_SRC) not found after cbindgen. Is cbindgen installed and in PATH?"; exit 1)

# Copy the generated header
copy_header: gen_header
	@echo "Copying generated header to $(OUTPUT_DIR)/..."
	@mkdir -p $(OUTPUT_DIR)
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

# Set the install name for the copied dylib on macOS
set_install_name: copy_libs
ifeq ($(UNAME_S),Darwin)
	@echo "Setting install name for $(DYLIB_DEST) on macOS..."
	@install_name_tool -id "@rpath/libyrs.dylib" $(DYLIB_DEST)
else
	@echo "Skipping install name setting (not on macOS)."
endif

# Clean up generated files
clean:
	@echo "Cleaning header file ($(H_DEST))..."
	@rm -f $(H_GENERATED_SRC)
	@echo "Cleaning build artifacts (removing $(OUTPUT_DIR)/)..."
	@rm -rf $(OUTPUT_DIR)
	@echo "Cleaning Rust target directory ($(CARGO_TARGET_DIR)/)..."
	@rm -rf $(RELEASE_DIR)
	@echo "Cleaning Go build output..."
	@rm -f $(GO_BINARY)
	@echo "Clean complete."

