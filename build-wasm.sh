#!/bin/bash
set -e
# Clean up any previous build
rm -rf build

# Configure the build
cmake -DWASI_SDK_PREFIX=/opt/wasi-sdk -DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake -B build .

# Build the project
cd build
make

# Find the wasm file (it might be in a different location)
WASM_FILE=$(find . -name "*.wasm")
if [ -z "$WASM_FILE" ]; then
  echo "No .wasm file found in the build directory"
  exit 1
fi

# Move and optimize the wasm file
cp "$WASM_FILE" ../taglib.wasm
cd ..
wasm-opt --strip -c -O3 taglib.wasm -o taglib.wasm
EOF