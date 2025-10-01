package taglib

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed taglib.wasm
var binary []byte // WASM blob. To override, go build -ldflags="-X 'go.senan.xyz/taglib.binaryPath=/path/to/taglib.wasm'"
var binaryPath string

var ErrInvalidFile = fmt.Errorf("invalid file")
var ErrSavingFile = fmt.Errorf("can't save file")

// These constants define normalized tag keys used by TagLib's [property mapping].
// When using [ReadTags], the library will map format-specific metadata to these standardized keys.
// Similarly, [WriteTags] will map these keys back to the appropriate format-specific fields.
//
// While these constants provide a consistent interface across different audio formats,
// you can also use custom tag keys if the underlying format supports arbitrary tags.
//
// [property mapping]: https://taglib.org/api/p_propertymapping.html
const (
	AcoustIDFingerprint       = "ACOUSTID_FINGERPRINT"
	AcoustIDID                = "ACOUSTID_ID"
	Album                     = "ALBUM"
	AlbumArtist               = "ALBUMARTIST"
	AlbumArtistSort           = "ALBUMARTISTSORT"
	AlbumSort                 = "ALBUMSORT"
	Arranger                  = "ARRANGER"
	Artist                    = "ARTIST"
	Artists                   = "ARTISTS"
	ArtistSort                = "ARTISTSORT"
	ArtistWebpage             = "ARTISTWEBPAGE"
	ASIN                      = "ASIN"
	AudioSourceWebpage        = "AUDIOSOURCEWEBPAGE"
	Barcode                   = "BARCODE"
	BPM                       = "BPM"
	CatalogNumber             = "CATALOGNUMBER"
	Comment                   = "COMMENT"
	Compilation               = "COMPILATION"
	Composer                  = "COMPOSER"
	ComposerSort              = "COMPOSERSORT"
	Conductor                 = "CONDUCTOR"
	Copyright                 = "COPYRIGHT"
	CopyrightURL              = "COPYRIGHTURL"
	Date                      = "DATE"
	DiscNumber                = "DISCNUMBER"
	DiscSubtitle              = "DISCSUBTITLE"
	DJMixer                   = "DJMIXER"
	EncodedBy                 = "ENCODEDBY"
	Encoding                  = "ENCODING"
	EncodingTime              = "ENCODINGTIME"
	Engineer                  = "ENGINEER"
	FileType                  = "FILETYPE"
	FileWebpage               = "FILEWEBPAGE"
	GaplessPlayback           = "GAPLESSPLAYBACK"
	Genre                     = "GENRE"
	Grouping                  = "GROUPING"
	InitialKey                = "INITIALKEY"
	InvolvedPeople            = "INVOLVEDPEOPLE"
	ISRC                      = "ISRC"
	Label                     = "LABEL"
	Language                  = "LANGUAGE"
	Length                    = "LENGTH"
	License                   = "LICENSE"
	Lyricist                  = "LYRICIST"
	Lyrics                    = "LYRICS"
	Media                     = "MEDIA"
	Mixer                     = "MIXER"
	Mood                      = "MOOD"
	MovementCount             = "MOVEMENTCOUNT"
	MovementName              = "MOVEMENTNAME"
	MovementNumber            = "MOVEMENTNUMBER"
	MusicBrainzAlbumID        = "MUSICBRAINZ_ALBUMID"
	MusicBrainzAlbumArtistID  = "MUSICBRAINZ_ALBUMARTISTID"
	MusicBrainzArtistID       = "MUSICBRAINZ_ARTISTID"
	MusicBrainzReleaseGroupID = "MUSICBRAINZ_RELEASEGROUPID"
	MusicBrainzReleaseTrackID = "MUSICBRAINZ_RELEASETRACKID"
	MusicBrainzTrackID        = "MUSICBRAINZ_TRACKID"
	MusicBrainzWorkID         = "MUSICBRAINZ_WORKID"
	MusicianCredits           = "MUSICIANCREDITS"
	MusicIPPUID               = "MUSICIP_PUID"
	OriginalAlbum             = "ORIGINALALBUM"
	OriginalArtist            = "ORIGINALARTIST"
	OriginalDate              = "ORIGINALDATE"
	OriginalFilename          = "ORIGINALFILENAME"
	OriginalLyricist          = "ORIGINALLYRICIST"
	Owner                     = "OWNER"
	PaymentWebpage            = "PAYMENTWEBPAGE"
	Performer                 = "PERFORMER"
	PlaylistDelay             = "PLAYLISTDELAY"
	Podcast                   = "PODCAST"
	PodcastCategory           = "PODCASTCATEGORY"
	PodcastDesc               = "PODCASTDESC"
	PodcastID                 = "PODCASTID"
	PodcastURL                = "PODCASTURL"
	ProducedNotice            = "PRODUCEDNOTICE"
	Producer                  = "PRODUCER"
	PublisherWebpage          = "PUBLISHERWEBPAGE"
	RadioStation              = "RADIOSTATION"
	RadioStationOwner         = "RADIOSTATIONOWNER"
	RadioStationWebpage       = "RADIOSTATIONWEBPAGE"
	ReleaseCountry            = "RELEASECOUNTRY"
	ReleaseDate               = "RELEASEDATE"
	ReleaseStatus             = "RELEASESTATUS"
	ReleaseType               = "RELEASETYPE"
	Remixer                   = "REMIXER"
	Script                    = "SCRIPT"
	ShowSort                  = "SHOWSORT"
	ShowWorkMovement          = "SHOWWORKMOVEMENT"
	Subtitle                  = "SUBTITLE"
	TaggingDate               = "TAGGINGDATE"
	Title                     = "TITLE"
	TitleSort                 = "TITLESORT"
	TrackNumber               = "TRACKNUMBER"
	TVEpisode                 = "TVEPISODE"
	TVEpisodeID               = "TVEPISODEID"
	TVNetwork                 = "TVNETWORK"
	TVSeason                  = "TVSEASON"
	TVShow                    = "TVSHOW"
	URL                       = "URL"
	Work                      = "WORK"
)

// ReadTags reads all metadata tags from an audio file at the given path.
func ReadTags(path string) (map[string][]string, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("make path abs %w", err)
	}

	dir := filepath.Dir(path)
	mod, err := newModuleRO(dir)
	if err != nil {
		return nil, fmt.Errorf("init module: %w", err)
	}
	defer mod.close()

	var raw wasmStrings
	if err := mod.call("taglib_file_tags", &raw, wasmString(wasmPath(path))); err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	if raw == nil {
		return nil, ErrInvalidFile
	}

	var tags = map[string][]string{}
	for _, row := range raw {
		k, v, ok := strings.Cut(row, "\t")
		if !ok {
			continue
		}
		tags[k] = append(tags[k], v)
	}
	return tags, nil
}

// Properties contains the audio properties of a media file.
type Properties struct {
	// Length is the duration of the audio
	Length time.Duration
	// Channels is the number of audio channels
	Channels uint
	// SampleRate in Hz
	SampleRate uint
	// Bitrate in kbit/s
	Bitrate uint
	// Images contains metadata about all embedded images
	Images []ImageDesc
}

// ImageDesc contains metadata about an embedded image without the actual image data.
type ImageDesc struct {
	// Type is the picture type (e.g., "Front Cover", "Back Cover")
	Type string
	// Description is a textual description of the image
	Description string
	// MIMEType is the MIME type of the image (e.g., "image/jpeg")
	MIMEType string
}

// ReadProperties reads the audio properties from a file at the given path.
func ReadProperties(path string) (Properties, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return Properties{}, fmt.Errorf("make path abs %w", err)
	}

	dir := filepath.Dir(path)
	mod, err := newModuleRO(dir)
	if err != nil {
		return Properties{}, fmt.Errorf("init module: %w", err)
	}
	defer mod.close()

	var raw wasmFileProperties
	if err := mod.call("taglib_file_read_properties", &raw, wasmString(wasmPath(path))); err != nil {
		return Properties{}, fmt.Errorf("call: %w", err)
	}

	var images []ImageDesc
	for _, row := range raw.imageDescs {
		parts := strings.SplitN(row, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		images = append(images, ImageDesc{
			Type:        parts[0],
			Description: parts[1],
			MIMEType:    parts[2],
		})
	}

	return Properties{
		Length:     time.Duration(raw.lengthInMilliseconds) * time.Millisecond,
		Channels:   uint(raw.channels),
		SampleRate: uint(raw.sampleRate),
		Bitrate:    uint(raw.bitrate),
		Images:     images,
	}, nil
}

// WriteOption configures the behavior of write operations. The can be passed to [WriteTags] and combined with the bitwise OR operator.
type WriteOption uint8

const (
	// Clear indicates that all existing tags not present in the new map should be removed.
	Clear WriteOption = 1 << iota
)

// WriteTags writes the metadata key-values pairs to path. The behavior can be controlled with [WriteOption].
func WriteTags(path string, tags map[string][]string, opts WriteOption) error {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("make path abs %w", err)
	}

	dir := filepath.Dir(path)
	mod, err := newModule(dir)
	if err != nil {
		return fmt.Errorf("init module: %w", err)
	}
	defer mod.close()

	var raw []string
	for k, vs := range tags {
		raw = append(raw, fmt.Sprintf("%s\t%s", k, strings.Join(vs, "\v")))
	}

	var out wasmBool
	if err := mod.call("taglib_file_write_tags", &out, wasmString(wasmPath(path)), wasmStrings(raw), wasmUint8(opts)); err != nil {
		return fmt.Errorf("call: %w", err)
	}
	if !out {
		return ErrSavingFile
	}
	return nil
}

// ReadImage reads the first embedded image from path. Returns empty byte slice if no images exist.
func ReadImage(path string) ([]byte, error) {
	return ReadImageOptions(path, 0)
}

// WriteImage writes image as an embedded "Front Cover" at index 0 with auto-detected MIME type.
func WriteImage(path string, image []byte) error {
	mimeType := ""
	if image != nil {
		mimeType = detectImageMIME(image)
	}
	return WriteImageOptions(path, image, 0, "Front Cover", "Added by go-taglib", mimeType)
}

// ReadImageOptions reads the embedded image at the specified index from path.
// Index 0 is the first image. Returns empty byte slice if index is out of range.
func ReadImageOptions(path string, index int) ([]byte, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("make path abs %w", err)
	}

	mod, err := newModuleRO(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("init module: %w", err)
	}
	defer mod.close()

	var img wasmBytes
	if err := mod.call("taglib_file_read_image", &img, wasmString(wasmPath(path)), wasmInt(index)); err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}

	return img, nil
}

// WriteImageOptions writes an image with custom metadata.
// Index specifies which image slot to write to (0 = first image).
func WriteImageOptions(path string, image []byte, index int, imageType, description, mimeType string) error {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("make path abs %w", err)
	}

	mod, err := newModule(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("init module: %w", err)
	}
	defer mod.close()

	var out wasmBool
	if err := mod.call("taglib_file_write_image", &out, wasmString(wasmPath(path)), wasmBytes(image), wasmInt(len(image)), wasmInt(index), wasmString(imageType), wasmString(description), wasmString(mimeType)); err != nil {
		return fmt.Errorf("call: %w", err)
	}
	if !out {
		return ErrSavingFile
	}
	return nil
}

type rc struct {
	wazero.Runtime
	wazero.CompiledModule
}

var getRuntimeOnce = sync.OnceValues(func() (rc, error) {
	ctx := context.Background()

	cacheDir := filepath.Join(os.TempDir(), "go-taglib-wasm")
	compilationCache, err := wazero.NewCompilationCacheWithDir(cacheDir)
	if err != nil {
		return rc{}, err
	}

	runtime := wazero.NewRuntimeWithConfig(ctx,
		wazero.NewRuntimeConfig().
			WithCompilationCache(compilationCache),
	)
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	_, err = runtime.
		NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(func(int32) int32 { panic("__cxa_allocate_exception") }).Export("__cxa_allocate_exception").
		NewFunctionBuilder().WithFunc(func(int32, int32, int32) { panic("__cxa_throw") }).Export("__cxa_throw").
		Instantiate(ctx)
	if err != nil {
		return rc{}, err
	}

	var bin = binary
	if binaryPath != "" {
		bin, err = os.ReadFile(binaryPath)
		if err != nil {
			return rc{}, fmt.Errorf("read custom binary path: %w", err)
		}
		clear(binary)
	}

	compiled, err := runtime.CompileModule(ctx, bin)
	if err != nil {
		return rc{}, err
	}

	return rc{
		Runtime:        runtime,
		CompiledModule: compiled,
	}, nil
})

type module struct {
	mod api.Module
}

func newModule(dir string) (module, error)   { return newModuleOpt(dir, false) }
func newModuleRO(dir string) (module, error) { return newModuleOpt(dir, true) }
func newModuleOpt(dir string, readOnly bool) (module, error) {
	rt, err := getRuntimeOnce()
	if err != nil {
		return module{}, fmt.Errorf("get runtime once: %w", err)
	}

	fsConfig := wazero.NewFSConfig()
	if readOnly {
		fsConfig = fsConfig.WithReadOnlyDirMount(dir, wasmPath(dir))
	} else {
		fsConfig = fsConfig.WithDirMount(dir, wasmPath(dir))
	}

	cfg := wazero.
		NewModuleConfig().
		WithName("").
		WithStartFunctions("_initialize").
		WithFSConfig(fsConfig)

	ctx := context.Background()
	mod, err := rt.InstantiateModule(ctx, rt.CompiledModule, cfg)
	if err != nil {
		return module{}, err
	}

	return module{
		mod: mod,
	}, nil
}

func (m *module) malloc(size uint32) uint32 {
	var ptr wasmUint32
	if err := m.call("malloc", &ptr, wasmUint32(size)); err != nil {
		panic(err)
	}
	if ptr == 0 {
		panic("no ptr")
	}
	return uint32(ptr)
}

type wasmArg interface {
	encode(*module) uint64
}

type wasmResult interface {
	decode(*module, uint64)
}

type wasmBool bool

func (b wasmBool) encode(*module) uint64 {
	if b {
		return 1
	}
	return 0
}

func (b *wasmBool) decode(_ *module, val uint64) {
	*b = val == 1
}

type wasmInt int

func (i wasmInt) encode(*module) uint64 { return uint64(i) }
func (i *wasmInt) decode(_ *module, val uint64) {
	*i = wasmInt(val)
}

type wasmUint8 uint8

func (u wasmUint8) encode(*module) uint64 { return uint64(u) }

type wasmUint32 uint32

func (u wasmUint32) encode(*module) uint64 { return uint64(u) }
func (u *wasmUint32) decode(_ *module, val uint64) {
	*u = wasmUint32(val)
}

type wasmString string

func (s wasmString) encode(m *module) uint64 {
	b := append([]byte(s), 0)
	ptr := m.malloc(uint32(len(b)))
	if !m.mod.Memory().Write(ptr, b) {
		panic("failed to write to mod.module.Memory()")
	}
	return uint64(ptr)
}
func (s *wasmString) decode(m *module, val uint64) {
	if val != 0 {
		*s = wasmString(readString(m, uint32(val)))
	}
}

type wasmBytes []byte

func (b wasmBytes) encode(m *module) uint64 {
	ptr := m.malloc(uint32(len(b)))
	if !m.mod.Memory().Write(ptr, b) {
		panic("failed to write to mod.module.Memory()")
	}
	return uint64(ptr)
}
func (b *wasmBytes) decode(m *module, val uint64) {
	if val != 0 {
		*b = readBytes(m, uint32(val))
	}
}

type wasmStrings []string

func (s wasmStrings) encode(m *module) uint64 {
	arrayPtr := m.malloc(uint32((len(s) + 1) * 4))
	for i, str := range s {
		b := append([]byte(str), 0)
		ptr := m.malloc(uint32(len(b)))
		if !m.mod.Memory().Write(ptr, b) {
			panic("failed to write to mod.module.Memory()")
		}
		if !m.mod.Memory().WriteUint32Le(arrayPtr+uint32(i*4), ptr) {
			panic("failed to write pointer to mod.module.Memory()")
		}
	}
	if !m.mod.Memory().WriteUint32Le(arrayPtr+uint32(len(s)*4), 0) {
		panic("failed to write pointer to memory")
	}
	return uint64(arrayPtr)
}
func (s *wasmStrings) decode(m *module, val uint64) {
	if val != 0 {
		*s = readStrings(m, uint32(val))
	}
}

type wasmFileProperties struct {
	lengthInMilliseconds uint32
	channels             uint32
	sampleRate           uint32
	bitrate              uint32
	imageDescs           []string
}

func (f *wasmFileProperties) decode(m *module, val uint64) {
	if val == 0 {
		return
	}
	ptr := uint32(val)

	f.lengthInMilliseconds, _ = m.mod.Memory().ReadUint32Le(ptr)
	f.channels, _ = m.mod.Memory().ReadUint32Le(ptr + 4)
	f.sampleRate, _ = m.mod.Memory().ReadUint32Le(ptr + 8)
	f.bitrate, _ = m.mod.Memory().ReadUint32Le(ptr + 12)

	imageMetadataPtr, _ := m.mod.Memory().ReadUint32Le(ptr + 16)
	if imageMetadataPtr != 0 {
		f.imageDescs = readStrings(m, imageMetadataPtr)
	}
}

func (m *module) call(name string, dest wasmResult, args ...wasmArg) error {
	params := make([]uint64, 0, len(args))
	for _, a := range args {
		params = append(params, a.encode(m))
	}

	results, err := m.mod.ExportedFunction(name).Call(context.Background(), params...)
	if err != nil {
		return fmt.Errorf("call %q: %w", name, err)
	}
	if len(results) == 0 {
		return nil
	}

	dest.decode(m, results[0])
	return nil
}

func (m *module) close() {
	if err := m.mod.Close(context.Background()); err != nil {
		panic(err)
	}
}

func readStrings(m *module, ptr uint32) []string {
	strs := []string{} // non nil so call knows if it's just empty
	for {
		stringPtr, ok := m.mod.Memory().ReadUint32Le(ptr)
		if !ok {
			panic("memory error")
		}
		if stringPtr == 0 {
			break
		}
		str := readString(m, stringPtr)
		strs = append(strs, str)
		ptr += 4
	}
	return strs
}

func readString(m *module, ptr uint32) string {
	size := uint32(64)
	buf, ok := m.mod.Memory().Read(ptr, size)
	if !ok {
		panic("memory error")
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		return string(buf[:i])
	}

	for {
		next, ok := m.mod.Memory().Read(ptr+size, size)
		if !ok {
			panic("memory error")
		}
		if i := bytes.IndexByte(next, 0); i >= 0 {
			return string(append(buf, next[:i]...))
		}
		buf = append(buf, next...)
		size += size
	}
}

func readBytes(m *module, ptr uint32) []byte {
	ret := []byte{} // non nil so call knows if it's just empty

	size, ok := m.mod.Memory().ReadUint32Le(ptr)
	if !ok {
		panic("memory error")
	}
	if size == 0 {
		return ret
	}

	loc, _ := m.mod.Memory().ReadUint32Le(ptr + 4)
	b, ok := m.mod.Memory().Read(loc, size)
	if !ok {
		panic("memory error")
	}

	// copy the data, "this returns a view of the underlying memory, not a copy" per api.Memory.Read docs
	ret = make([]byte, size)
	copy(ret, b)

	return ret
}

// WASI uses POSIXy paths, even on Windows
func wasmPath(p string) string {
	return filepath.ToSlash(p)
}

// detectImageMIME detects image MIME type from magic bytes.
// Adapted from Go's net/http package to avoid the dependency.
func detectImageMIME(data []byte) string {
	if len(data) < 2 {
		return ""
	}
	switch {
	case len(data) >= 4 && bytes.Equal(data[:4], []byte("\x00\x00\x01\x00")):
		return "image/x-icon"
	case len(data) >= 4 && bytes.Equal(data[:4], []byte("\x00\x00\x02\x00")):
		return "image/x-icon"
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp"
	case bytes.HasPrefix(data, []byte("GIF87a")):
		return "image/gif"
	case bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\x0D\x0A\x1A\x0A")):
		return "image/png"
	case len(data) >= 3 && bytes.Equal(data[:3], []byte("\xFF\xD8\xFF")):
		return "image/jpeg"
	case len(data) >= 14 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:14], []byte("WEBPVP")):
		return "image/webp"
	default:
		return ""
	}
}
