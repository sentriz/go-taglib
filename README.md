# go-taglib

This project is a Go library for reading and writing audio metadata tags. By packaging an embedded **Wasm** binary, the library needs no external dependencies or CGo. Meaning easy static builds and cross compilation.

Current bundled TagLib version is [v2.1.1](https://github.com/taglib/taglib/releases/tag/v2.1.1).

To reproduce or verify the bundled binary, see the [attestations](https://github.com/sentriz/go-taglib/attestations/).

[![godoc](https://img.shields.io/badge/pkg.go.dev-doc-blue)](http://pkg.go.dev/go.senan.xyz/taglib)

## Features

- **Read** and **write** metadata tags for audio files, including support for **multi-valued** tags.
- **Read** and **write** embedded images (album artwork) from audio files.
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
        taglib.AlbumArtist:   {"David Byrne", "Brian Eno"},
        taglib.Album:         {"My Life in the Bush of Ghosts"},
        taglib.TrackNumber:   {"1"},

        // Non-standard allowed too
        "ALBUMARTIST_CREDIT": {"Brian Eno & David Byrne"},
    }, 0)
    // check(err)
}
```

#### Options for writing

The behaviour of writing can be configured with some bitset flags

The options are

- `Clear` which indicates that all existing tags not present in the new map should be removed

The options can be combined the with the bitwise `OR` operator (`|`)

```go
    taglib.WriteTags(path, tags, taglib.Clear)
    taglib.WriteTags(path, tags, 0)
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

    // Image metadata (without reading actual image data)
    for i, img := range properties.Images {
        fmt.Printf("Image %d - Type: %s, Description: %s, MIME type: %s\n",
            i, img.Type, img.Description, img.MIMEType)
    }
}
```

### Reading embedded images

```go
import (
    "bytes"
    "image"

    _ "image/gif"
    _ "image/jpeg"
    _ "image/png"

    // ...
)

func main() {
    // Read first image (index 0)
    imageBytes, err := taglib.ReadImage("path/to/audiofile.mp3")
    // check(err)

    if imageBytes == nil {
        fmt.Printf("File contains no image")
        return
    }

    img, format, err := image.Decode(bytes.NewReader(imageBytes))
    // check(err)

    fmt.Printf("format: %q\n", format)

    bounds := img.Bounds()
    fmt.Printf("width: %d\n", bounds.Dx())
    fmt.Printf("height: %d\n", bounds.Dy())

    // Read a specific image by index
    backCover, err := taglib.ReadImageOptions("path/to/audiofile.mp3", 1)
    // check(err)
}
```

### Writing embedded images

```go
func main() {
    var imageBytes []byte // Some image data, from somewhere

    // Write as front cover with auto-detected MIME type
    err = taglib.WriteImage("path/to/audiofile.mp3", imageBytes)
    // check(err)

    // Write with custom options
    err = taglib.WriteImageOptions(
        "path/to/audiofile.mp3",
        imageBytes,
        0,                  // replaces image at index; use higher index to append
        "Back Cover",       // picture type
        "Back artwork",     // description
        "image/jpeg",       // MIME type
    )
    // check(err)
}
```

## Manually Building and Using the Wasm Binary

The binary is already included in the package. However if you want to manually build and override it, you can with WASI SDK and Go build flags

1. Clone this repository and Git submodules

   ```console
   $ git clone "https://github.com/sentriz/go-taglib.git" --recursive
   $ cd go-taglib
   ```

> [!NOTE]
> Make sure to use the `--recursive` flag, without it there will be no TagLib submodule to build with

2. Generate the Wasm binary:

   ```console
   $ ./build/build-docker.sh
   $ # taglib.wasm created
   ```

   Or, to build without Docker install [WASI SDK](https://github.com/WebAssembly/wasi-sdk) and [Binaryen](https://github.com/WebAssembly/binaryen), then

   ```console
   $ ./build/build.sh /path/to/wasi-sdk # eg /opt/wasi-sdk
   $ # taglib.wasm created
   ```

3. Use the new binary in your project

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
