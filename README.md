# go-taglib

This project is a Go library for reading and writing audio metadata tags. By packaging an embedded **WASM** binary, the library needs no external dependencies or CGo. Meaning easy static builds and cross compilation.

[![godoc](https://img.shields.io/badge/pkg.go.dev-doc-blue)](http://pkg.go.dev/go.senan.xyz/taglib)

## Features

- **Read** and **write** metadata tags for audio files, including support for **multi-valued** tags.
- Retrieve audio properties such as length, bitrate, sample rate, and channels.
- Supports multiple audio formats including _MP3_, _FLAC_, _M4A_, _WAV_, _OGG_, _WMA_, and more.
- Safe for concurrent use
- [Reasonably fast](#performance)

## Usage

Add the library to your project with `go get go.senan.xyz/taglib@latest`

### Reading metadata

```go
func main() {
    tags, err := taglib.ReadTags("path/to/audiofile.mp3")
    // check(err)

    fmt.Printf("tags: %v\n", tags) // map[string][]string

    fmt.Printf("AlbumArtist: %q\n", tags[taglib.AlbumArtist])
    fmt.Printf("Album: %q\n", tags[taglib.Album])
    fmt.Printf("TrackNumber: %q\n", tags[taglib.TrackNumber])
}
```

### Writing metadata

```go
func main() {
    err := taglib.WriteTags("path/to/audiofile.mp3", map[string][]string{
        // Multi-valued tags allowed
        taglib.AlbumArtist:   {"David Bynre", "Brian Eno"},
        taglib.Album:         {"My Life in the Bush of Ghosts"},
        taglib.TrackNumber:   {"1"},

        // Non-standard allowed too
        "ALBUMARTIST_CREDIT": {"Brian Eno & David Bynre"},
    }, 0)
    // check(err)
}
```

#### Options for writing

The behaviour of writing can be configured with some bitset flags

The options are

- `Clear` which indicates that all existing tags not present in the new map should be removed
- `DiffBeforeWrite` which won't modify the file on disk if the new metadata is the same as the old

The options can be combined the with the bitwise `OR` operator (`|`)

```go
    taglib.WriteTags(path, tags, taglib.Clear|taglib.DiffBeforeWrite) // clear and diff
    taglib.WriteTags(path, tags, taglib.DiffBeforeWrite)              // only diff diff
    taglib.WriteTags(path, tags, taglib.Clear)                        // only clear
    taglib.WriteTags(path, tags, 0)                                   // don't clear or diff
```

### Reading properties

```go
func main() {
    properties, err := taglib.ReadProperties("path/to/audiofile.mp3")
    // check(err)

    fmt.Printf("Length: %v\n", properties.Length)
    fmt.Printf("Bitrate: %d\n", properties.Bitrate)
    fmt.Printf("SampleRate: %d\n", properties.SampleRate)
    fmt.Printf("Channels: %d\n", properties.Channels)
}
```

## Manually Building and Using the WASM Binary

A docker file is provided to build the WASM binary. 
[WASI SDK](https://github.com/WebAssembly/wasi-sdk) (includes Autotools, CMake, and Ninja) and 
[Binaryen](https://github.com/WebAssembly/binaryen) are installed during the docker container build.

1. Install Docker
2. Clone this repository and Git submodules

   ```console
   $ git clone "https://github.com/sentriz/go-taglib.git" --recursive
   $ cd go-taglib
   ```

> [!NOTE]
> Make sure to use the `--recursive` flag, without it there will be no TagLib submodule to build with 

3. Generate the WASM binary:

   ```console
   $ docker compose run taglib ./build-wasm.sh
   $ # taglib.wasm created
   ```

4. Use the new binary in your project

   ```console
   $ CGO_ENABLED=0 go build -ldflags="-X 'go.senan.xyz/taglib.binaryPath=/path/to/taglib.wasm'" ./your/project/...
   ```

### Performance

In this example, tracks are read on average in `0.3 ms`, and written in `1.85 ms`

```
goos: linux
goarch: amd64
pkg: go.senan.xyz/taglib
cpu: AMD Ryzen 7 7840U w/ Radeon  780M Graphics
BenchmarkWrite-16         608   1847873 ns/op
BenchmarkRead-16         3802    299247 ns/op
```

## License

This project is licensed under the GNU Lesser General Public License v2.1. See the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## Acknowledgments

- [TagLib](https://taglib.org/) for the audio metadata library.
- [Wazero](https://github.com/tetratelabs/wazero) for the WebAssembly runtime in Go.
