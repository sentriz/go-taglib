cmake_minimum_required(VERSION 3.5.0 FATAL_ERROR)
project(taglib_wasm)

add_subdirectory(${CMAKE_CURRENT_SOURCE_DIR}/taglib)

include_directories(
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/toolkit
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/asf
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/mpeg
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/ogg
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/ogg/vorbis
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/ogg/flac
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/flac
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/mpc
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/mp4
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/mpeg/id3v2
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/mpeg/id3v2/frames
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/wavpack
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/ogg/speex
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/trueaudio
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/riff
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/riff/aiff
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/riff/wav
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/ape
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/it
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/mod
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/s3m
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/xm
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/ogg/opus
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/dsf
  ${CMAKE_CURRENT_SOURCE_DIR}/taglib/taglib/dsdiff
)

add_executable(taglib ${CMAKE_CURRENT_SOURCE_DIR}/taglib_wasm.cpp)
set_target_properties(taglib PROPERTIES SUFFIX ".wasm" C_VISIBILITY_PRESET hidden)
target_compile_options(taglib PRIVATE --target=wasm32-wasi -g0 -O2)
target_link_options(taglib PRIVATE -Wl,--no-entry -Wl,--allow-undefined -Wl,@${CMAKE_CURRENT_SOURCE_DIR}/exports.txt)
target_link_libraries(taglib PRIVATE tag)
