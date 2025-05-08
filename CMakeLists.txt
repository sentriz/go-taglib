cmake_minimum_required(VERSION 3.5.0 FATAL_ERROR)
project(taglib_wasm)

set(CMAKE_BUILD_TYPE Release)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
set(WITH_ZLIB OFF)
set(BUILD_SHARED_LIBS OFF)
set(BUILD_TESTING OFF)

add_subdirectory(
  taglib
)
include_directories(
  taglib/taglib
  taglib/taglib/toolkit
  taglib/taglib/mpeg
  taglib/taglib/mpeg/id3v1
  taglib/taglib/mpeg/id3v2
  taglib/taglib/mpeg/id3v2/frames
  taglib/taglib/ogg
  taglib/taglib/ogg/flac
  taglib/taglib/flac
  taglib/taglib/mp4
)

add_executable(taglib taglib.cpp)
set_target_properties(taglib PROPERTIES SUFFIX ".wasm")
target_compile_options(taglib PRIVATE --target=wasm32-wasi -g0 -O2)
target_link_options(taglib PRIVATE -Wl,--allow-undefined -mexec-model=reactor)
target_link_libraries(taglib PRIVATE tag)
