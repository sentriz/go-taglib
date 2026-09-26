cmake_minimum_required(VERSION 3.5.0 FATAL_ERROR)
project(taglib_wasm)

set(CMAKE_BUILD_TYPE Release)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
set(WITH_ZLIB OFF)
set(BUILD_SHARED_LIBS OFF)
set(BUILD_TESTING OFF)

# TagLib handles invalid text by catching utfcpp exceptions, which wasm can't
# do, so hand it a utf8.h whose conversions never throw
set(CMAKE_DISABLE_FIND_PACKAGE_utf8cpp ON)
set(utf8cpp_INCLUDE_DIR "${CMAKE_CURRENT_SOURCE_DIR}/utf8-nothrow" CACHE PATH "" FORCE)

add_subdirectory(
  taglib
)
include_directories(
  taglib/taglib
  taglib/taglib/toolkit
)

add_executable(taglib taglib.cpp)
set_target_properties(taglib PROPERTIES SUFFIX ".wasm")
target_compile_options(taglib PRIVATE --target=wasm32-wasi -g0 -O2)
target_link_options(taglib PRIVATE -Wl,--allow-undefined -mexec-model=reactor)
target_link_libraries(taglib PRIVATE tag)
