#!/usr/bin/env sh

set -e

image="go-taglib-build"

docker build -t "$image" "./build"
docker run -v "$PWD":/pwd --entrypoint sh "$image" -c "cd /pwd; ./build/build.sh /opt/wasi-sdk"
