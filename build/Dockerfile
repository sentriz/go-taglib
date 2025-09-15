FROM ghcr.io/webassembly/wasi-sdk:wasi-sdk-27

RUN apt-get update && \
	apt-get install -y --no-install-recommends curl && \
	rm -rf /var/lib/apt/lists/*

ARG BINARYEN_VERSION=124
RUN \
	curl -L https://github.com/WebAssembly/binaryen/releases/download/version_${BINARYEN_VERSION}/binaryen-version_${BINARYEN_VERSION}-x86_64-linux.tar.gz -o binaryen.tar.gz && \
	tar -xzf binaryen.tar.gz && \
	cp binaryen-version_${BINARYEN_VERSION}/bin/* /usr/local/bin/ 
