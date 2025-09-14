#!/usr/bin/env sh

set -e

wasi_loc="$1"
if [ -z "$wasi_loc" ]; then
    echo "please provide <wasi loc>" >&2
    exit 1
fi

cmake -DWASI_SDK_PREFIX="$wasi_loc" -DCMAKE_TOOLCHAIN_FILE="$wasi_loc/share/cmake/wasi-sdk.cmake" -B build .
cmake --build build --target taglib
mv build/taglib.wasm .

wasm-opt --strip -c -O3 taglib.wasm -o taglib.wasm
