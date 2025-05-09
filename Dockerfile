FROM ghcr.io/webassembly/wasi-sdk:sha-5e4756e

RUN apt-get update 
RUN apt-get install -y --no-install-recommends binaryen
RUN rm -rf /var/lib/apt/lists/*

COPY . /taglib

WORKDIR /taglib