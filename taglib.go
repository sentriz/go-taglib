package taglib

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
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

// FileFormat represents the detected audio file format.
type FileFormat uint8

// File format constants matching the C++ enum.
const (
	FormatUnknown   FileFormat = 0
	FormatMPEG      FileFormat = 1
	FormatMP4       FileFormat = 2
	FormatFLAC      FileFormat = 3
	FormatOggVorbis FileFormat = 4
	FormatOggOpus   FileFormat = 5
	FormatOggFLAC   FileFormat = 6
	FormatOggSpeex  FileFormat = 7
	FormatWAV       FileFormat = 8
	FormatAIFF      FileFormat = 9
	FormatASF       FileFormat = 10
	FormatAPE       FileFormat = 11
	FormatWavPack   FileFormat = 12
	FormatDSF       FileFormat = 13
	FormatDSDIFF    FileFormat = 14
	FormatTrueAudio FileFormat = 15
	FormatMPC       FileFormat = 16
	FormatShorten   FileFormat = 17
	FormatMatroska  FileFormat = 18
)

func (f FileFormat) String() string {
	switch f {
	case FormatMPEG:
		return "MPEG"
	case FormatMP4:
		return "MP4"
	case FormatFLAC:
		return "FLAC"
	case FormatOggVorbis:
		return "Ogg Vorbis"
	case FormatOggOpus:
		return "Ogg Opus"
	case FormatOggFLAC:
		return "Ogg FLAC"
	case FormatOggSpeex:
		return "Ogg Speex"
	case FormatWAV:
		return "WAV"
	case FormatAIFF:
		return "AIFF"
	case FormatASF:
		return "ASF"
	case FormatAPE:
		return "APE"
	case FormatWavPack:
		return "WavPack"
	case FormatDSF:
		return "DSF"
	case FormatDSDIFF:
		return "DSDIFF"
	case FormatTrueAudio:
		return "TrueAudio"
	case FormatMPC:
		return "MPC"
	case FormatShorten:
		return "Shorten"
	case FormatMatroska:
		return "Matroska"
	default:
		return "Unknown"
	}
}

// ReadStyle controls how thoroughly audio properties are read from a file.
// Higher accuracy requires reading more of the file, which takes longer.
type ReadStyle uint8

const (
	// ReadStyleFast reads as little of the file as possible for quick metadata access.
	ReadStyleFast ReadStyle = 0
	// ReadStyleAverage provides a balance between speed and accuracy (default).
	ReadStyleAverage ReadStyle = 1
	// ReadStyleAccurate reads as much of the file as needed for precise values.
	ReadStyleAccurate ReadStyle = 2
)

// OpenOption configures how a file is opened.
type OpenOption func(*openOptions)

type openOptions struct {
	readStyle ReadStyle
	filename  string // hint for format detection in OpenStream
}

// WithReadStyle sets the read style for audio properties.
// Default is [ReadStyleAverage].
func WithReadStyle(style ReadStyle) OpenOption {
	return func(o *openOptions) {
		o.readStyle = style
	}
}

// WithFilename provides a filename hint for format detection when using [OpenStream].
// TagLib uses the file extension (e.g., ".opus", ".flac") to assist format detection.
// Without this hint, TagLib relies on content-sniffing alone, which may fail for some formats.
func WithFilename(name string) OpenOption {
	return func(o *openOptions) {
		o.filename = name
	}
}

// File represents an open audio file handle for efficient multiple operations.
// Use [Open] or [OpenReadOnly] to create a File, and always call [File.Close] when done.
type File struct {
	mod      module
	handle   uint32
	format   FileFormat
	streamId uint32 // non-zero if opened via OpenStream
}

// Open opens an audio file for reading and writing.
// The returned File must be closed with [File.Close] when done.
// Options can be provided to configure behavior (e.g., [WithReadStyle]).
func Open(path string, opts ...OpenOption) (*File, error) {
	o := &openOptions{readStyle: ReadStyleAverage}
	for _, opt := range opts {
		opt(o)
	}
	return openFile(path, false, o.readStyle)
}

// OpenReadOnly opens an audio file for reading only.
// The returned File must be closed with [File.Close] when done.
// Options can be provided to configure behavior (e.g., [WithReadStyle]).
func OpenReadOnly(path string, opts ...OpenOption) (*File, error) {
	o := &openOptions{readStyle: ReadStyleAverage}
	for _, opt := range opts {
		opt(o)
	}
	return openFile(path, true, o.readStyle)
}

// OpenStream opens an audio stream for reading metadata.
// The reader must remain valid for the lifetime of the returned File.
// The returned File must be closed with [File.Close] when done.
// This is useful for reading from network streams, archives, or in-memory buffers.
// Options can be provided to configure behavior (e.g., [WithReadStyle]).
func OpenStream(r io.ReadSeeker, opts ...OpenOption) (*File, error) {
	o := &openOptions{readStyle: ReadStyleAverage}
	for _, opt := range opts {
		opt(o)
	}
	streamId := registerStream(r)

	mod, err := newModuleForStream()
	if err != nil {
		unregisterStream(streamId)
		return nil, fmt.Errorf("init module: %w", err)
	}

	var result wasmOpenResult
	if err := mod.call("taglib_stream_open", &result, wasmUint32(streamId), wasmString(o.filename), wasmUint8(o.readStyle)); err != nil {
		mod.close()
		unregisterStream(streamId)
		return nil, fmt.Errorf("call: %w", err)
	}
	if result.handle == 0 {
		mod.close()
		unregisterStream(streamId)
		return nil, ErrInvalidFile
	}

	return &File{
		mod:      mod,
		handle:   result.handle,
		format:   FileFormat(result.format),
		streamId: streamId,
	}, nil
}

func openFile(path string, readOnly bool, readStyle ReadStyle) (*File, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("make path abs: %w", err)
	}

	dir := filepath.Dir(path)
	var mod module
	if readOnly {
		mod, err = newModuleRO(dir)
	} else {
		mod, err = newModule(dir)
	}
	if err != nil {
		return nil, fmt.Errorf("init module: %w", err)
	}

	var result wasmOpenResult
	if err := mod.call("taglib_file_open", &result, wasmString(wasmPath(path)), wasmUint8(readStyle)); err != nil {
		mod.close()
		return nil, fmt.Errorf("call: %w", err)
	}
	if result.handle == 0 {
		mod.close()
		return nil, ErrInvalidFile
	}

	return &File{
		mod:    mod,
		handle: result.handle,
		format: FileFormat(result.format),
	}, nil
}

// Close releases the file handle and associated resources.
// After Close is called, the File should not be used.
func (f *File) Close() error {
	if f.handle == 0 {
		return nil
	}
	var out wasmBool
	_ = f.mod.call("taglib_file_close", &out, wasmUint32(f.handle))
	f.handle = 0
	if f.streamId != 0 {
		unregisterStream(f.streamId)
		f.streamId = 0
	}
	f.mod.close()
	return nil
}

// Format returns the detected audio file format.
func (f *File) Format() FileFormat {
	return f.format
}

// Tags reads all normalized metadata tags from the file.
func (f *File) Tags() map[string][]string {
	var raw wasmStrings
	if err := f.mod.call("taglib_handle_tags", &raw, wasmUint32(f.handle)); err != nil {
		return nil
	}
	if raw == nil {
		return nil
	}

	tags := map[string][]string{}
	for _, row := range raw {
		k, v, ok := strings.Cut(row, "\t")
		if !ok {
			continue
		}
		tags[k] = append(tags[k], v)
	}
	return tags
}

// RawTags reads format-specific tags from the file.
// For MP3/WAV/AIFF: returns ID3v2 frames
// For MP4: returns MP4 atoms
// For ASF: returns ASF attributes
// For other formats (FLAC, OGG, etc.): returns same as Tags() (Vorbis Comments)
func (f *File) RawTags() map[string][]string {
	var raw wasmStrings
	if err := f.mod.call("taglib_handle_raw_tags", &raw, wasmUint32(f.handle)); err != nil {
		return nil
	}
	if raw == nil {
		return nil
	}

	tags := map[string][]string{}
	for _, row := range raw {
		k, v, ok := strings.Cut(row, "\t")
		if !ok {
			continue
		}
		tags[k] = append(tags[k], v)
	}
	return tags
}

// AllTags contains both normalized and format-specific tags.
type AllTags struct {
	// Tags contains normalized tag keys (TITLE, ARTIST, etc.)
	Tags map[string][]string
	// Raw contains format-specific tags (ID3v2 frames, MP4 atoms, etc.)
	Raw map[string][]string
	// Format is the detected audio file format
	Format FileFormat
}

// AllTags reads both normalized and format-specific tags in a single struct.
func (f *File) AllTags() AllTags {
	return AllTags{
		Tags:   f.Tags(),
		Raw:    f.RawTags(),
		Format: f.format,
	}
}

// Properties reads the audio properties from the file.
func (f *File) Properties() Properties {
	var raw wasmFileProperties
	if err := f.mod.call("taglib_handle_properties", &raw, wasmUint32(f.handle)); err != nil {
		return Properties{}
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
		Length:        time.Duration(raw.lengthInMilliseconds) * time.Millisecond,
		Channels:      uint(raw.channels),
		SampleRate:    uint(raw.sampleRate),
		Bitrate:       uint(raw.bitrate),
		BitsPerSample: uint(raw.bitsPerSample),
		Codec:         raw.codec,
		Images:        images,
	}
}

// Image reads the embedded image at the specified index from the file.
// Index 0 is the first image. Returns empty byte slice if index is out of range.
func (f *File) Image(index int) ([]byte, error) {
	var img wasmBytes
	if err := f.mod.call("taglib_handle_image", &img, wasmUint32(f.handle), wasmInt(index)); err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	return img, nil
}

// WriteTags writes the metadata key-values pairs to the file.
// The behavior can be controlled with [WriteOption].
func (f *File) WriteTags(tags map[string][]string, opts WriteOption) error {
	var raw []string
	for k, vs := range tags {
		raw = append(raw, fmt.Sprintf("%s\t%s", k, strings.Join(vs, "\v")))
	}

	var out wasmBool
	if err := f.mod.call("taglib_handle_write_tags", &out, wasmUint32(f.handle), wasmStrings(raw), wasmUint8(opts)); err != nil {
		return fmt.Errorf("call: %w", err)
	}
	if !out {
		return ErrSavingFile
	}
	return nil
}

// WriteImage writes an image with custom metadata.
// Index specifies which image slot to write to (0 = first image).
// Set image to nil to clear the image at that index.
func (f *File) WriteImage(image []byte, index int, imageType, description, mimeType string) error {
	var out wasmBool
	if err := f.mod.call("taglib_handle_write_image", &out, wasmUint32(f.handle), wasmBytes(image), wasmUint32(uint32(len(image))), wasmInt(index), wasmString(imageType), wasmString(description), wasmString(mimeType)); err != nil {
		return fmt.Errorf("call: %w", err)
	}
	if !out {
		return ErrSavingFile
	}
	return nil
}

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

// ReadID3v2Frames reads all ID3v2 frames from an audio file at the given path.
// Supported formats: MP3, WAV, and AIFF.
// This provides direct access to the raw ID3v2 frames, including custom frames like TXXX.
// The returned map has frame IDs as keys (like "TIT2", "TPE1", "TXXX") and frame data as values.
// For TXXX frames, the description is included in the key as "TXXX:description".
func ReadID3v2Frames(path string) (map[string][]string, error) {
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
	if err := mod.call("taglib_file_id3v2_frames", &raw, wasmString(wasmPath(path))); err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	if raw == nil {
		return nil, ErrInvalidFile
	}

	// If raw is empty, the file has no ID3v2 frames
	var frames = map[string][]string{}
	for _, row := range raw {
		parts := strings.SplitN(row, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		frames[parts[0]] = append(frames[parts[0]], parts[1])
	}
	return frames, nil
}

// ReadID3v1Frames reads all ID3v1 tags from an MP3 file at the given path.
// This provides access to the standard ID3v1 fields: title, artist, album, year, comment, track, and genre.
// The returned map has standardized keys (like "TITLE", "ARTIST", "ALBUM") and values.
// Note that ID3v1 is a much simpler format than ID3v2 with a fixed set of fields.
func ReadID3v1Frames(path string) (map[string][]string, error) {
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
	if err := mod.call("taglib_file_id3v1_tags", &raw, wasmString(wasmPath(path))); err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	if raw == nil {
		return nil, ErrInvalidFile
	}

	// If raw is empty, the file has no ID3v1 tags
	var frames = map[string][]string{}
	for _, row := range raw {
		parts := strings.SplitN(row, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		frames[parts[0]] = append(frames[parts[0]], parts[1])
	}
	return frames, nil
}

// ReadMP4Atoms reads all MP4 atoms from an M4A/MP4 file at the given path.
// This provides direct access to the raw MP4 atoms/items, including standard atoms like ©ART (artist),
// ©alb (album), trkn (track number), etc.
// The returned map has atom names as keys and atom data as values.
// For IntPair atoms (like trkn, disk), values are split into separate keys with :num and :total suffixes.
// For example, track 3 of 12 returns as "trkn:num" -> ["3"] and "trkn:total" -> ["12"].
func ReadMP4Atoms(path string) (map[string][]string, error) {
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
	if err := mod.call("taglib_file_mp4_atoms", &raw, wasmString(wasmPath(path))); err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	if raw == nil {
		return nil, ErrInvalidFile
	}

	// If raw is empty, the file has no MP4 atoms
	var atoms = map[string][]string{}
	for _, row := range raw {
		parts := strings.SplitN(row, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		atoms[parts[0]] = append(atoms[parts[0]], parts[1])
	}
	return atoms, nil
}

// ReadASFAttributes reads all ASF attributes from a WMA/ASF file at the given path.
// This provides direct access to the raw ASF attributes, including standard attributes like
// WM/AlbumTitle, WM/AlbumArtist, WM/TrackNumber, etc.
// The returned map has attribute names as keys and attribute data as values.
// Multi-valued attributes are returned as slices with multiple values.
func ReadASFAttributes(path string) (map[string][]string, error) {
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
	if err := mod.call("taglib_file_asf_attributes", &raw, wasmString(wasmPath(path))); err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	if raw == nil {
		return nil, ErrInvalidFile
	}

	// If raw is empty, the file has no ASF attributes
	var attrs = map[string][]string{}
	for _, row := range raw {
		parts := strings.SplitN(row, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		attrs[parts[0]] = append(attrs[parts[0]], parts[1])
	}
	return attrs, nil
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
	// BitsPerSample is the bit depth (e.g., 16, 24, 32). May be 0 for formats that don't support this.
	BitsPerSample uint
	// Codec is the audio codec (e.g., "MP3", "AAC", "ALAC"). May be empty for formats without codec variants.
	Codec string
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
		Length:        time.Duration(raw.lengthInMilliseconds) * time.Millisecond,
		Channels:      uint(raw.channels),
		SampleRate:    uint(raw.sampleRate),
		Bitrate:       uint(raw.bitrate),
		BitsPerSample: uint(raw.bitsPerSample),
		Codec:         raw.codec,
		Images:        images,
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

// WriteID3v2Frames writes ID3v2 frames to an MP3 file at the given path.
// This provides direct access to modify raw ID3v2 frames, including custom frames like TXXX.
// The map should have frame IDs as keys (like "TIT2", "TPE1", "TXXX") and frame data as values.
// The opts parameter can include taglib.Clear to remove all existing frames not in the new map.
func WriteID3v2Frames(path string, frames map[string][]string, opts WriteOption) error {
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

	// Convert the frames map to a slice of strings
	var framesList []string
	for k, vs := range frames {
		framesList = append(framesList, fmt.Sprintf("%s\t%s", k, strings.Join(vs, "\v")))
	}

	var out wasmBool
	if err := mod.call("taglib_file_write_id3v2_frames", &out, wasmString(wasmPath(path)), wasmStrings(framesList), wasmUint8(opts)); err != nil {
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
// Set image to nil to clear the image at that index.
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
// Set image to nil to clear the image at that index.
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

// Stream registry for io.ReadSeeker streams used by OpenStream
var (
	streamRegistry   = make(map[uint32]io.ReadSeeker)
	streamRegistryMu sync.RWMutex
	nextStreamId     uint32 = 1
)

// Buffer pool for stream reads - avoids allocation per read call
var streamReadPool = sync.Pool{
	New: func() any {
		// Match C++ BUFFER_SIZE (32KB)
		buf := make([]byte, 32*1024)
		return &buf
	},
}

func registerStream(r io.ReadSeeker) uint32 {
	streamRegistryMu.Lock()
	defer streamRegistryMu.Unlock()
	id := nextStreamId
	nextStreamId++
	streamRegistry[id] = r
	return id
}

func unregisterStream(id uint32) {
	streamRegistryMu.Lock()
	defer streamRegistryMu.Unlock()
	delete(streamRegistry, id)
}

func getStream(id uint32) io.ReadSeeker {
	streamRegistryMu.RLock()
	defer streamRegistryMu.RUnlock()
	return streamRegistry[id]
}

// Host functions called by WASM for stream I/O
func hostStreamRead(_ context.Context, m api.Module, streamId, bufPtr, length uint32) uint32 {
	r := getStream(streamId)
	if r == nil {
		return 0
	}

	// Use pooled buffer to avoid allocation
	bufp := streamReadPool.Get().(*[]byte)
	buf := *bufp
	defer streamReadPool.Put(bufp)

	// Read up to buffer size
	toRead := length
	if toRead > uint32(len(buf)) {
		toRead = uint32(len(buf))
	}

	n, err := r.Read(buf[:toRead])
	if err != nil && n == 0 {
		return 0
	}
	if n > 0 {
		m.Memory().Write(bufPtr, buf[:n])
	}
	return uint32(n)
}

func hostStreamSeek(_ context.Context, streamId uint32, offset int64, whence int32) int32 {
	r := getStream(streamId)
	if r == nil {
		return -1
	}
	_, err := r.Seek(offset, int(whence))
	if err != nil {
		return -1
	}
	return 0
}

func hostStreamTell(_ context.Context, streamId uint32) int64 {
	r := getStream(streamId)
	if r == nil {
		return -1
	}
	pos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return -1
	}
	return pos
}

func hostStreamLength(_ context.Context, streamId uint32) int64 {
	r := getStream(streamId)
	if r == nil {
		return -1
	}
	// Save current position
	cur, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return -1
	}
	// Seek to end to get length
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return -1
	}
	// Restore position
	_, _ = r.Seek(cur, io.SeekStart)
	return end
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

	// Register stream I/O host functions for OpenStream support
	_, err = runtime.
		NewHostModuleBuilder("go_io").
		NewFunctionBuilder().WithFunc(hostStreamRead).Export("stream_read").
		NewFunctionBuilder().WithFunc(hostStreamSeek).Export("stream_seek").
		NewFunctionBuilder().WithFunc(hostStreamTell).Export("stream_tell").
		NewFunctionBuilder().WithFunc(hostStreamLength).Export("stream_length").
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

// newModuleForStream creates a module without filesystem mounts for stream-based access
func newModuleForStream() (module, error) {
	rt, err := getRuntimeOnce()
	if err != nil {
		return module{}, fmt.Errorf("get runtime once: %w", err)
	}

	cfg := wazero.
		NewModuleConfig().
		WithName("").
		WithStartFunctions("_initialize")

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
	bitsPerSample        uint32
	imageDescs           []string
	codec                string
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
	f.bitsPerSample, _ = m.mod.Memory().ReadUint32Le(ptr + 16)

	imageMetadataPtr, _ := m.mod.Memory().ReadUint32Le(ptr + 20)
	if imageMetadataPtr != 0 {
		f.imageDescs = readStrings(m, imageMetadataPtr)
	}

	codecPtr, _ := m.mod.Memory().ReadUint32Le(ptr + 24)
	if codecPtr != 0 {
		f.codec = readString(m, codecPtr)
	}
}

type wasmOpenResult struct {
	handle uint32
	format uint8
}

func (r *wasmOpenResult) decode(m *module, val uint64) {
	if val == 0 {
		return
	}
	ptr := uint32(val)

	r.handle, _ = m.mod.Memory().ReadUint32Le(ptr)
	format, _ := m.mod.Memory().ReadByte(ptr + 4)
	r.format = format
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
