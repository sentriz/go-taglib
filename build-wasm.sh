cmake -DWASI_SDK_PREFIX=/opt/wasi-sdk -DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake -B build .
cmake --build build --target taglib
mv build/taglib.wasm .
wasm-opt --strip -c -O3 taglib.wasm -o taglib.wasm
