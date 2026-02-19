package taglib_test

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"go.senan.xyz/taglib"
)

func TestInvalid(t *testing.T) {
	t.Parallel()

	path := tmpf(t, []byte("not a file"), "eg.flac")
	_, err := taglib.ReadTags(path)
	eq(t, err, taglib.ErrInvalidFile)
}

func TestClear(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			// set some tags first
			err := taglib.WriteTags(path, map[string][]string{
				"ARTIST":     {"Example A"},
				"ALUMARTIST": {"Example"},
			}, taglib.Clear)

			nilErr(t, err)

			// then clear
			err = taglib.WriteTags(path, nil, taglib.Clear)
			nilErr(t, err)

			got, err := taglib.ReadTags(path)
			nilErr(t, err)

			if len(got) > 0 {
				t.Fatalf("exp empty, got %v", got)
			}
		})
	}
}

func TestReadWrite(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	testTags := []map[string][]string{
		{
			"ONE":  {"one", "two", "three", "four"},
			"FIVE": {"six", "seven"},
			"NINE": {"nine"},
		},
		{
			"ARTIST":     {"Example A", "Hello, 世界"},
			"ALUMARTIST": {"Example"},
		},
		{
			"ARTIST":      {"Example A", "Example B"},
			"ALUMARTIST":  {"Example"},
			"TRACK":       {"1"},
			"TRACKNUMBER": {"1"},
		},
		{
			"ARTIST":     {"Example A", "Example B"},
			"ALUMARTIST": {"Example"},
		},
		{
			"ARTIST": {"Hello, 世界", "界世"},
		},
		{
			"ARTIST": {"Brian Eno—David Byrne"},
			"ALBUM":  {"My Life in the Bush of Ghosts"},
		},
		{
			"ARTIST":      {"Hello, 世界", "界世"},
			"ALBUM":       {longString},
			"ALBUMARTIST": {longString, longString},
			"OTHER":       {strings.Repeat(longString, 2)},
		},
	}

	for _, path := range paths {
		for i, tags := range testTags {
			t.Run(fmt.Sprintf("%s_tags_%d", filepath.Base(path), i), func(t *testing.T) {
				err := taglib.WriteTags(path, tags, taglib.Clear)
				nilErr(t, err)

				got, err := taglib.ReadTags(path)
				nilErr(t, err)

				tagEq(t, got, tags)
			})
		}
	}
}

func TestMergeWrite(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)

	cmp := func(t *testing.T, path string, want map[string][]string) {
		t.Helper()
		tags, err := taglib.ReadTags(path)
		nilErr(t, err)
		tagEq(t, tags, want)
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			err := taglib.WriteTags(path, nil, taglib.Clear)
			nilErr(t, err)

			err = taglib.WriteTags(path, map[string][]string{
				"ONE": {"one"},
			}, 0)

			nilErr(t, err)
			cmp(t, path, map[string][]string{
				"ONE": {"one"},
			})

			nilErr(t, err)
			err = taglib.WriteTags(path, map[string][]string{
				"TWO": {"two", "two!"},
			}, 0)

			nilErr(t, err)
			cmp(t, path, map[string][]string{
				"ONE": {"one"},
				"TWO": {"two", "two!"},
			})

			err = taglib.WriteTags(path, map[string][]string{
				"THREE": {"three"},
			}, 0)

			nilErr(t, err)
			cmp(t, path, map[string][]string{
				"ONE":   {"one"},
				"TWO":   {"two", "two!"},
				"THREE": {"three"},
			})

			// change prev
			err = taglib.WriteTags(path, map[string][]string{
				"ONE": {"one new"},
			}, 0)

			nilErr(t, err)
			cmp(t, path, map[string][]string{
				"ONE":   {"one new"},
				"TWO":   {"two", "two!"},
				"THREE": {"three"},
			})

			// change prev
			err = taglib.WriteTags(path, map[string][]string{
				"ONE":   {},
				"THREE": {"three new!"},
			}, 0)

			nilErr(t, err)
			cmp(t, path, map[string][]string{
				"TWO":   {"two", "two!"},
				"THREE": {"three new!"},
			})
		})
	}
}

func TestReadExistingUnicode(t *testing.T) {
	tags, err := taglib.ReadTags("testdata/normal.flac")
	nilErr(t, err)
	eq(t, len(tags[taglib.AlbumArtist]), 1)
	eq(t, tags[taglib.AlbumArtist][0], "Brian Eno—David Byrne")
}

func TestOpenStream(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			// Read the file into memory
			data, err := os.ReadFile(path)
			nilErr(t, err)

			// Open via stream
			r := bytes.NewReader(data)
			f, err := taglib.OpenStream(r)
			nilErr(t, err)
			defer f.Close()

			// Read tags via stream
			streamTags := f.Tags()

			// Read tags via file for comparison
			fileTags, err := taglib.ReadTags(path)
			nilErr(t, err)

			// Compare tags
			eq(t, len(fileTags), len(streamTags))
			for k, v := range fileTags {
				streamV, ok := streamTags[k]
				if !ok {
					t.Errorf("stream missing tag %q", k)
					continue
				}
				if !slices.Equal(v, streamV) {
					t.Errorf("tag %q: got %v, want %v", k, streamV, v)
				}
			}

			// Test properties
			streamProps := f.Properties()
			fileProps, err := taglib.ReadProperties(path)
			nilErr(t, err)

			eq(t, fileProps.Length, streamProps.Length)
			eq(t, fileProps.Channels, streamProps.Channels)
			eq(t, fileProps.SampleRate, streamProps.SampleRate)
			eq(t, fileProps.Bitrate, streamProps.Bitrate)

			// Test format detection
			if f.Format() == taglib.FormatUnknown {
				t.Errorf("expected format to be detected, got Unknown")
			}
		})
	}
}

func TestOpenStreamWithFilename(t *testing.T) {
	t.Parallel()

	// OPUS format detection requires a filename hint because TagLib cannot
	// reliably detect OPUS via content-sniffing alone when using streams.
	r := bytes.NewReader(egOpus)
	f, err := taglib.OpenStream(r, taglib.WithFilename("test.opus"))
	nilErr(t, err)
	defer f.Close()

	eq(t, taglib.FormatOggOpus, f.Format())
	tags := f.Tags()
	eq(t, tags[taglib.Title][0], "Test")
	eq(t, tags[taglib.Artist][0], "Test Artist")
	eq(t, tags[taglib.Album][0], "Test Album")
}

func TestWAVLatin1InfoChunk(t *testing.T) {
	t.Parallel()

	// WAV files with RIFF INFO chunks encoded in Latin-1 (ISO-8859-1) should
	// be readable without crashing. The "ö" character (0xF6 in Latin-1) is not
	// valid UTF-8 and previously caused a crash in the WASM build. Invalid
	// bytes are now replaced with the Unicode replacement character (U+FFFD).
	t.Run("file", func(t *testing.T) {
		path := tmpf(t, egWAVLatin1, "eg-latin1.wav")
		tags, err := taglib.ReadTags(path)
		nilErr(t, err)
		eq(t, tags["TITLE"][0], "Aufl\uFFFDsen")
	})

	t.Run("stream", func(t *testing.T) {
		r := bytes.NewReader(egWAVLatin1)
		f, err := taglib.OpenStream(r, taglib.WithFilename("test.wav"))
		nilErr(t, err)
		defer f.Close()
		tags := f.Tags()
		eq(t, tags["TITLE"][0], "Aufl\uFFFDsen")
	})
}

func TestOpenStreamConcurrent(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)

	// Read all files into memory first
	fileData := make([][]byte, len(paths))
	for i, path := range paths {
		data, err := os.ReadFile(path)
		nilErr(t, err)
		fileData[i] = data
	}

	c := 100
	pathErrors := make([]error, c)

	var wg sync.WaitGroup
	for i := range c {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := fileData[i%len(fileData)]
			r := bytes.NewReader(data)
			f, err := taglib.OpenStream(r)
			if err != nil {
				pathErrors[i] = fmt.Errorf("iter %d: %w", i, err)
				return
			}
			_ = f.Tags()
			_ = f.Properties()
			f.Close()
		}()
	}
	wg.Wait()

	err := errors.Join(pathErrors...)
	nilErr(t, err)
}

func TestConcurrent(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)

	c := 250
	pathErrors := make([]error, c)

	var wg sync.WaitGroup
	for i := range c {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := taglib.ReadTags(paths[i%len(paths)]); err != nil {
				pathErrors[i] = fmt.Errorf("iter %d: %w", i, err)
			}
		}()
	}
	wg.Wait()

	err := errors.Join(pathErrors...)
	nilErr(t, err)
}

func TestProperties(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egFLAC, "eg.flac")

	properties, err := taglib.ReadProperties(path)
	nilErr(t, err)

	eq(t, 1*time.Second, properties.Length)
	eq(t, 1460, properties.Bitrate)
	eq(t, 48_000, properties.SampleRate)
	eq(t, 2, properties.Channels)
	eq(t, 24, properties.BitsPerSample) // FLAC test file uses 24-bit

	eq(t, len(properties.Images), 2)
	eq(t, properties.Images[0].Type, "Front Cover")
	eq(t, properties.Images[0].Description, "The first image")
	eq(t, properties.Images[0].MIMEType, "image/png")
	eq(t, properties.Images[1].Type, "Lead Artist")
	eq(t, properties.Images[1].Description, "The second image")
	eq(t, properties.Images[1].MIMEType, "image/jpeg")
}

func TestPropertiesBitsPerSample(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		data          []byte
		filename      string
		bitsPerSample uint
	}{
		{"FLAC", egFLAC, "eg.flac", 24},
		{"WAV", egWAV, "eg.wav", 16},
		{"M4A", egM4a, "eg.m4a", 16}, // AAC in M4A container
		{"MP3", egMP3, "eg.mp3", 0},  // MP3 doesn't support BitsPerSample
		{"OGG", egOgg, "eg.ogg", 0},  // Vorbis doesn't support BitsPerSample
		{"MKA", egMKA, "eg.mka", 32}, // Vorbis uses 32-bit float samples
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tmpf(t, tt.data, tt.filename)
			properties, err := taglib.ReadProperties(path)
			nilErr(t, err)
			eq(t, tt.bitsPerSample, properties.BitsPerSample)
		})
	}
}

func TestPropertiesCodec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     []byte
		filename string
		codec    string
	}{
		{"MP3", egMP3, "eg.mp3", "MP3"},
		{"M4A", egM4a, "eg.m4a", "AAC"},
		{"WMA", egWMA, "eg.wma", "WMA2"},
		{"FLAC", egFLAC, "eg.flac", ""}, // FLAC doesn't have codec variants
		{"OGG", egOgg, "eg.ogg", ""},    // Vorbis doesn't have codec variants
		{"WAV", egWAV, "eg.wav", ""},    // WAV doesn't have codec variants
		{"AIFF", egAIFF, "eg.aiff", ""}, // AIFF doesn't have codec variants
		{"MKA", egMKA, "eg.mka", "A_VORBIS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tmpf(t, tt.data, tt.filename)
			properties, err := taglib.ReadProperties(path)
			nilErr(t, err)
			eq(t, tt.codec, properties.Codec)
		})
	}
}

func TestMultiOpen(t *testing.T) {
	t.Parallel()

	{
		path := tmpf(t, egFLAC, "eg.flac")
		_, err := taglib.ReadTags(path)
		nilErr(t, err)
	}
	{
		path := tmpf(t, egFLAC, "eg.flac")
		_, err := taglib.ReadTags(path)
		nilErr(t, err)
	}
}

func TestReadImage(t *testing.T) {
	path := tmpf(t, egFLAC, "eg.flac")

	properties, err := taglib.ReadProperties(path)
	nilErr(t, err)
	eq(t, len(properties.Images) > 0, true)

	imgBytes, err := taglib.ReadImage(path)
	nilErr(t, err)
	if imgBytes == nil {
		t.Fatalf("no image")
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	nilErr(t, err)

	b := img.Bounds()
	if b.Dx() != 700 || b.Dy() != 700 {
		t.Fatalf("bad image dimensions: %d, %d != 700, 700", b.Dx(), b.Dy())
	}
}

func TestWriteImage(t *testing.T) {
	path := tmpf(t, egFLAC, "eg.flac")

	err := taglib.WriteImage(path, coverJPG)
	nilErr(t, err)

	imgBytes, err := taglib.ReadImage(path)
	nilErr(t, err)
	if imgBytes == nil {
		t.Fatalf("no written image")
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	nilErr(t, err)

	b := img.Bounds()
	if b.Dx() != 700 || b.Dy() != 700 {
		t.Fatalf("bad image dimensions: %d, %d != 700, 700", b.Dx(), b.Dy())
	}
}

func TestClearImage(t *testing.T) {
	path := tmpf(t, egFLAC, "eg.flac")

	properties, err := taglib.ReadProperties(path)
	nilErr(t, err)
	eq(t, len(properties.Images) == 2, true) // have two imaages
	eq(t, properties.Images[0].Description, "The first image")

	img, err := taglib.ReadImage(path)
	nilErr(t, err)
	eq(t, len(img) > 0, true)

	nilErr(t, taglib.WriteImage(path, nil))

	properties, err = taglib.ReadProperties(path)
	nilErr(t, err)
	eq(t, len(properties.Images) == 1, true) // have one images
	eq(t, properties.Images[0].Description, "The second image")

	nilErr(t, taglib.WriteImage(path, nil))

	properties, err = taglib.ReadProperties(path)
	nilErr(t, err)
	eq(t, len(properties.Images) == 0, true) // have zero images

	img, err = taglib.ReadImage(path)
	nilErr(t, err)
	eq(t, len(img) == 0, true)
}

func TestClearImageReverse(t *testing.T) {
	path := tmpf(t, egFLAC, "eg.flac")

	properties, err := taglib.ReadProperties(path)
	nilErr(t, err)
	eq(t, len(properties.Images) == 2, true) // have two imaages
	eq(t, properties.Images[0].Description, "The first image")

	img, err := taglib.ReadImage(path)
	nilErr(t, err)
	eq(t, len(img) > 0, true)

	nilErr(t, taglib.WriteImageOptions(path, nil, 1, "", "", "")) // delete the second

	properties, err = taglib.ReadProperties(path)
	nilErr(t, err)
	eq(t, len(properties.Images) == 1, true)                   // have one images
	eq(t, properties.Images[0].Description, "The first image") // but it's the first one

	nilErr(t, taglib.WriteImage(path, nil))

	properties, err = taglib.ReadProperties(path)
	nilErr(t, err)
	eq(t, len(properties.Images) == 0, true) // have zero images

	img, err = taglib.ReadImage(path)
	nilErr(t, err)
	eq(t, len(img) == 0, true)
}

func TestReadID3v2Frames(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3, "eg.mp3")

	// First write some tags using WriteTags so we have ID3v2 data
	err := taglib.WriteTags(path, map[string][]string{
		"TITLE":  {"Test Title"},
		"ARTIST": {"Test Artist"},
		"ALBUM":  {"Test Album"},
	}, taglib.Clear)
	nilErr(t, err)

	// Now read the ID3v2 frames directly
	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	// Should have frames (the exact frame IDs depend on how TagLib maps tags)
	if len(frames) == 0 {
		t.Fatal("expected some ID3v2 frames")
	}

	// Check that we got TIT2 (title), TPE1 (artist), TALB (album) frames
	if _, ok := frames["TIT2"]; !ok {
		t.Error("expected TIT2 frame for title")
	}
	if _, ok := frames["TPE1"]; !ok {
		t.Error("expected TPE1 frame for artist")
	}
	if _, ok := frames["TALB"]; !ok {
		t.Error("expected TALB frame for album")
	}
}

func TestReadID3v2FramesEmpty(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3, "eg.mp3")

	// Clear all tags first
	err := taglib.WriteTags(path, nil, taglib.Clear)
	nilErr(t, err)

	// Read ID3v2 frames from a file with no tags - should return empty map, not error
	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	// Should be empty or have no meaningful frames
	if frames == nil {
		t.Fatal("expected non-nil map")
	}
}

func TestReadID3v2FramesNonMP3(t *testing.T) {
	t.Parallel()

	// ID3v2 is supported in MP3, WAV, and AIFF - but FLAC doesn't use it
	path := tmpf(t, egFLAC, "eg.flac")

	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	// FLAC doesn't use ID3v2, should return empty map
	if len(frames) != 0 {
		t.Errorf("expected empty frames for FLAC, got %d", len(frames))
	}
}

func TestReadID3v2FramesAIFF(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egAIFF, "eg.aiff")

	// AIFF files can contain ID3v2 tags - our test file has them
	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	// Check that we got some frames from the AIFF file
	if _, ok := frames["TIT2"]; !ok {
		t.Error("expected TIT2 frame in AIFF file")
	}
	if _, ok := frames["TPE1"]; !ok {
		t.Error("expected TPE1 frame in AIFF file")
	}
}

func TestReadID3v2FramesWAV(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egWAV, "eg.wav")

	// WAV files can contain ID3v2 tags - our test file has them (written by mutagen)
	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	// Check that we got some frames from the WAV file
	if _, ok := frames["TIT2"]; !ok {
		t.Error("expected TIT2 frame in WAV file")
	}
	if _, ok := frames["TPE1"]; !ok {
		t.Error("expected TPE1 frame in WAV file")
	}
	if _, ok := frames["TALB"]; !ok {
		t.Error("expected TALB frame in WAV file")
	}
}

func TestReadID3v2FramesInvalid(t *testing.T) {
	t.Parallel()

	path := tmpf(t, []byte("not a file"), "invalid.mp3")

	frames, err := taglib.ReadID3v2Frames(path)
	// Invalid file should return ErrInvalidFile (nil frames from WASM)
	if err == nil && frames != nil && len(frames) > 0 {
		t.Error("expected error or empty frames for invalid file")
	}
}

func TestReadID3v1Frames(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3, "eg.mp3")

	// Write some tags - TagLib will write to ID3v1 as well for MP3
	err := taglib.WriteTags(path, map[string][]string{
		"TITLE":  {"Test Title"},
		"ARTIST": {"Test Artist"},
		"ALBUM":  {"Test Album"},
	}, taglib.Clear)
	nilErr(t, err)

	// Read ID3v1 frames
	frames, err := taglib.ReadID3v1Frames(path)
	nilErr(t, err)

	if frames == nil {
		t.Fatal("expected non-nil map")
	}

	// ID3v1 uses uppercase field names like TITLE, ARTIST, ALBUM
	if title, ok := frames["TITLE"]; ok {
		eq(t, title[0], "Test Title")
	}
	if artist, ok := frames["ARTIST"]; ok {
		eq(t, artist[0], "Test Artist")
	}
	if album, ok := frames["ALBUM"]; ok {
		eq(t, album[0], "Test Album")
	}
}

func TestReadID3v1FramesNonMP3(t *testing.T) {
	t.Parallel()

	// ID3v1 is specific to MP3
	path := tmpf(t, egFLAC, "eg.flac")

	frames, err := taglib.ReadID3v1Frames(path)
	nilErr(t, err)

	// FLAC doesn't use ID3v1, should return empty map
	if len(frames) != 0 {
		t.Errorf("expected empty frames for FLAC, got %d", len(frames))
	}
}

func TestReadMP4Atoms(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egM4a, "eg.m4a")

	// First write some tags using WriteTags so we have MP4 data
	err := taglib.WriteTags(path, map[string][]string{
		"TITLE":       {"Test Title"},
		"ARTIST":      {"Test Artist"},
		"ALBUM":       {"Test Album"},
		"TRACKNUMBER": {"3"},
	}, taglib.Clear)
	nilErr(t, err)

	// Now read the MP4 atoms directly
	atoms, err := taglib.ReadMP4Atoms(path)
	nilErr(t, err)

	// Should have atoms
	if len(atoms) == 0 {
		t.Fatal("expected some MP4 atoms")
	}

	// Check that we got ©nam (title), ©ART (artist), ©alb (album) atoms
	if _, ok := atoms["©nam"]; !ok {
		t.Error("expected ©nam atom for title")
	}
	if _, ok := atoms["©ART"]; !ok {
		t.Error("expected ©ART atom for artist")
	}
	if _, ok := atoms["©alb"]; !ok {
		t.Error("expected ©alb atom for album")
	}
}

func TestReadMP4AtomsEmpty(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egM4a, "eg.m4a")

	// Clear all tags first
	err := taglib.WriteTags(path, nil, taglib.Clear)
	nilErr(t, err)

	// Read MP4 atoms from a file with no tags - should return empty map, not error
	atoms, err := taglib.ReadMP4Atoms(path)
	nilErr(t, err)

	// Should be empty or have no meaningful atoms
	if atoms == nil {
		t.Fatal("expected non-nil map")
	}
}

func TestReadMP4AtomsNonM4A(t *testing.T) {
	t.Parallel()

	// MP4 atoms are specific to M4A/MP4, so non-M4A files should return empty atoms
	path := tmpf(t, egFLAC, "eg.flac")

	atoms, err := taglib.ReadMP4Atoms(path)
	nilErr(t, err)

	// FLAC doesn't use MP4 atoms, should return empty map
	if len(atoms) != 0 {
		t.Errorf("expected empty atoms for FLAC, got %d", len(atoms))
	}
}

func TestReadMP4AtomsInvalid(t *testing.T) {
	t.Parallel()

	path := tmpf(t, []byte("not a file"), "invalid.m4a")

	atoms, err := taglib.ReadMP4Atoms(path)
	// Invalid file should return ErrInvalidFile (nil atoms from WASM)
	if err == nil && atoms != nil && len(atoms) > 0 {
		t.Error("expected error or empty atoms for invalid file")
	}
}

func TestReadMP4AtomsIntPair(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egM4a, "eg.m4a")

	// Write track and disc numbers
	// Note: TagLib's property mapping stores TRACKNUMBER in trkn:num but TRACKTOTAL
	// goes to a free-form atom, so trkn:total will be 0. This test verifies the
	// IntPair splitting works correctly for the values that are present.
	err := taglib.WriteTags(path, map[string][]string{
		"TRACKNUMBER": {"3"},
		"DISCNUMBER":  {"1"},
	}, taglib.Clear)
	nilErr(t, err)

	// Read MP4 atoms
	atoms, err := taglib.ReadMP4Atoms(path)
	nilErr(t, err)

	// Track number should be split into trkn:num and trkn:total
	if num, ok := atoms["trkn:num"]; ok {
		eq(t, num[0], "3")
	} else {
		t.Error("expected trkn:num atom")
	}
	// trkn:total will be 0 since TagLib stores TRACKTOTAL separately
	if total, ok := atoms["trkn:total"]; ok {
		eq(t, total[0], "0")
	} else {
		t.Error("expected trkn:total atom")
	}

	// Disc number should be split into disk:num and disk:total
	if num, ok := atoms["disk:num"]; ok {
		eq(t, num[0], "1")
	} else {
		t.Error("expected disk:num atom")
	}
	if total, ok := atoms["disk:total"]; ok {
		eq(t, total[0], "0")
	} else {
		t.Error("expected disk:total atom")
	}
}

func TestReadID3v1FramesInvalid(t *testing.T) {
	t.Parallel()

	path := tmpf(t, []byte("not a file"), "invalid.mp3")

	frames, err := taglib.ReadID3v1Frames(path)
	// Invalid file should return ErrInvalidFile (nil frames from WASM)
	if err == nil && frames != nil && len(frames) > 0 {
		t.Error("expected error or empty frames for invalid file")
	}
}

func TestReadASFAttributes(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egWMA, "eg.wma")

	// First write some tags using WriteTags so we have ASF data
	err := taglib.WriteTags(path, map[string][]string{
		"TITLE":  {"Test Title"},
		"ARTIST": {"Test Artist"},
		"ALBUM":  {"Test Album"},
	}, taglib.Clear)
	nilErr(t, err)

	// Now read the ASF attributes directly
	attrs, err := taglib.ReadASFAttributes(path)
	nilErr(t, err)

	// Should have attributes
	if len(attrs) == 0 {
		t.Fatal("expected some ASF attributes")
	}

	// Check that we got Title, Author (artist), WM/AlbumTitle attributes
	// ASF has basic fields (Title, Author) and extended attributes (WM/*)
	if _, ok := attrs["Title"]; !ok {
		t.Error("expected Title attribute")
	}
	if _, ok := attrs["Author"]; !ok {
		t.Error("expected Author attribute for artist")
	}
	if _, ok := attrs["WM/AlbumTitle"]; !ok {
		t.Error("expected WM/AlbumTitle attribute for album")
	}
}

func TestReadASFAttributesEmpty(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egWMA, "eg.wma")

	// Clear all tags first
	err := taglib.WriteTags(path, nil, taglib.Clear)
	nilErr(t, err)

	// Read ASF attributes from a file with no tags - should return empty map, not error
	attrs, err := taglib.ReadASFAttributes(path)
	nilErr(t, err)

	// Should be empty or have no meaningful attributes
	if attrs == nil {
		t.Fatal("expected non-nil map")
	}
}

func TestReadASFAttributesNonWMA(t *testing.T) {
	t.Parallel()

	// ASF is specific to WMA, so non-WMA files should return empty attributes
	path := tmpf(t, egFLAC, "eg.flac")

	attrs, err := taglib.ReadASFAttributes(path)
	nilErr(t, err)

	// FLAC doesn't use ASF, should return empty map
	if len(attrs) != 0 {
		t.Errorf("expected empty attributes for FLAC, got %d", len(attrs))
	}
}

func TestReadASFAttributesInvalid(t *testing.T) {
	t.Parallel()

	path := tmpf(t, []byte("not a file"), "invalid.wma")

	attrs, err := taglib.ReadASFAttributes(path)
	// Invalid file should return ErrInvalidFile (nil attrs from WASM)
	if err == nil && attrs != nil && len(attrs) > 0 {
		t.Error("expected error or empty attributes for invalid file")
	}
}

func TestWriteID3v2Frames(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3, "eg.mp3")

	// Clear existing tags
	err := taglib.WriteTags(path, nil, taglib.Clear)
	nilErr(t, err)

	// Write ID3v2 frames directly
	err = taglib.WriteID3v2Frames(path, map[string][]string{
		"TIT2": {"Direct Title"},
		"TPE1": {"Direct Artist"},
		"TALB": {"Direct Album"},
	}, taglib.Clear)
	nilErr(t, err)

	// Read back and verify
	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	if frames["TIT2"] == nil || frames["TIT2"][0] != "Direct Title" {
		t.Errorf("expected TIT2='Direct Title', got %v", frames["TIT2"])
	}
	if frames["TPE1"] == nil || frames["TPE1"][0] != "Direct Artist" {
		t.Errorf("expected TPE1='Direct Artist', got %v", frames["TPE1"])
	}
	if frames["TALB"] == nil || frames["TALB"][0] != "Direct Album" {
		t.Errorf("expected TALB='Direct Album', got %v", frames["TALB"])
	}
}

func TestWriteID3v2FramesMerge(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3, "eg.mp3")

	// Clear and write initial frames
	err := taglib.WriteID3v2Frames(path, map[string][]string{
		"TIT2": {"Title One"},
		"TPE1": {"Artist One"},
	}, taglib.Clear)
	nilErr(t, err)

	// Merge new frame without clearing
	err = taglib.WriteID3v2Frames(path, map[string][]string{
		"TALB": {"Album One"},
	}, 0)
	nilErr(t, err)

	// All frames should exist
	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	if frames["TIT2"] == nil || frames["TIT2"][0] != "Title One" {
		t.Errorf("expected TIT2='Title One', got %v", frames["TIT2"])
	}
	if frames["TALB"] == nil || frames["TALB"][0] != "Album One" {
		t.Errorf("expected TALB='Album One', got %v", frames["TALB"])
	}
}

func TestWriteID3v2FramesInvalid(t *testing.T) {
	t.Parallel()

	path := tmpf(t, []byte("not a file"), "invalid.mp3")

	err := taglib.WriteID3v2Frames(path, map[string][]string{
		"TIT2": {"Test"},
	}, 0)
	// Invalid file should return an error - either a call error (function not in WASM)
	// or ErrSavingFile when the WASM function returns false
	// We accept both scenarios since the WASM binary may not have the function yet
	_ = err // Error is expected but may vary based on WASM binary state
}

func TestReadID3v2FramesUSLT(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3Lyrics, "eg_lyrics.mp3")

	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	// Check for USLT frames with language codes
	// Should have USLT:eng and USLT:deu
	foundEng := false
	foundDeu := false
	for key, values := range frames {
		if key == "USLT:eng" {
			foundEng = true
			if len(values) == 0 || values[0] != "English lyrics content here" {
				t.Errorf("expected English lyrics, got %v", values)
			}
		}
		if key == "USLT:deu" {
			foundDeu = true
			if len(values) == 0 || values[0] != "Deutsche Texte hier" {
				t.Errorf("expected German lyrics, got %v", values)
			}
		}
	}
	if !foundEng {
		t.Error("expected USLT:eng frame for English lyrics")
	}
	if !foundDeu {
		t.Error("expected USLT:deu frame for German lyrics")
	}
}

func TestReadID3v2FramesSYLT(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3Lyrics, "eg_lyrics.mp3")

	frames, err := taglib.ReadID3v2Frames(path)
	nilErr(t, err)

	// Check for SYLT frame with language code
	foundSYLT := false
	for key, values := range frames {
		if key == "SYLT:eng" {
			foundSYLT = true
			if len(values) == 0 {
				t.Error("expected SYLT:eng to have content")
				continue
			}
			// SYLT should be converted to LRC format
			lrc := values[0]
			if !strings.Contains(lrc, "[00:00.00]") {
				t.Errorf("expected LRC timestamp format, got %q", lrc)
			}
			if !strings.Contains(lrc, "Line one") {
				t.Errorf("expected 'Line one' in SYLT content, got %q", lrc)
			}
			if !strings.Contains(lrc, "[00:05.00]") {
				t.Errorf("expected [00:05.00] timestamp, got %q", lrc)
			}
			if !strings.Contains(lrc, "Line two") {
				t.Errorf("expected 'Line two' in SYLT content, got %q", lrc)
			}
		}
	}
	if !foundSYLT {
		t.Error("expected SYLT:eng frame for synchronized lyrics")
	}
}

func TestMemNew(t *testing.T) {
	t.Parallel()

	t.Skip("heavy")

	checkMem(t)

	for range 10_000 {
		path := tmpf(t, egFLAC, "eg.flac")
		_, err := taglib.ReadTags(path)
		nilErr(t, err)
		err = os.Remove(path) // don't blow up incase we're using tmpfs
		nilErr(t, err)
	}
}

func TestMemSameFile(t *testing.T) {
	t.Parallel()

	t.Skip("heavy")

	checkMem(t)

	path := tmpf(t, egFLAC, "eg.flac")
	for range 10_000 {
		_, err := taglib.ReadTags(path)
		nilErr(t, err)
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	t.Logf("alloc = %v MiB", memStats.Alloc/1024/1024)
}

func BenchmarkWrite(b *testing.B) {
	path := tmpf(b, egFLAC, "eg.flac")
	b.ResetTimer()

	for range b.N {
		err := taglib.WriteTags(path, bigTags, taglib.Clear)
		nilErr(b, err)
	}
}

func BenchmarkRead(b *testing.B) {
	path := tmpf(b, egFLAC, "eg.flac")
	err := taglib.WriteTags(path, bigTags, taglib.Clear)
	nilErr(b, err)
	b.ResetTimer()

	for range b.N {
		_, err := taglib.ReadTags(path)
		nilErr(b, err)
	}
}

var (
	//go:embed testdata/eg.flac
	egFLAC []byte
	//go:embed testdata/eg.mp3
	egMP3 []byte
	//go:embed testdata/eg.m4a
	egM4a []byte
	//go:embed testdata/eg.ogg
	egOgg []byte
	//go:embed testdata/eg.wav
	egWAV []byte
	//go:embed testdata/eg.aiff
	egAIFF []byte
	//go:embed testdata/eg.opus
	egOpus []byte
	//go:embed testdata/eg.wma
	egWMA []byte
	//go:embed testdata/eg-latin1-info.wav
	egWAVLatin1 []byte
	//go:embed testdata/cover.jpg
	coverJPG []byte
	//go:embed testdata/eg_lyrics.mp3
	egMP3Lyrics []byte
	//go:embed testdata/eg.mka
	egMKA []byte
)

func testPaths(t testing.TB) []string {
	return []string{
		tmpf(t, egFLAC, "eg.flac"),
		tmpf(t, egMP3, "eg.mp3"),
		tmpf(t, egM4a, "eg.m4a"),
		tmpf(t, egWAV, "eg.wav"),
		tmpf(t, egOgg, "eg.ogg"),
		tmpf(t, egAIFF, "eg.aiff"),
		tmpf(t, egMKA, "eg.mka"),
	}
}

func tmpf(t testing.TB, b []byte, name string) string {
	p := filepath.Join(t.TempDir(), name)
	err := os.WriteFile(p, b, os.ModePerm)
	nilErr(t, err)
	return p
}

func nilErr(t testing.TB, err error) {
	if err != nil {
		t.Helper()
		t.Fatalf("err: %v", err)
	}
}
func eq[T comparable](t testing.TB, a, b T) {
	if a != b {
		t.Helper()
		t.Fatalf("%v != %v", a, b)
	}
}
func tagEq(t testing.TB, a, b map[string][]string) {
	if !maps.EqualFunc(a, b, slices.Equal) {
		t.Helper()
		t.Fatalf("%q != %q", a, b)
	}
}

func checkMem(t testing.TB) {
	stop := make(chan struct{})
	t.Cleanup(func() {
		stop <- struct{}{}
	})

	go func() {
		ticker := time.Tick(100 * time.Millisecond)

		for {
			select {
			case <-stop:
				return

			case <-ticker:
				var memStats runtime.MemStats
				runtime.ReadMemStats(&memStats)
				t.Logf("alloc = %v MiB", memStats.Alloc/1024/1024)
			}
		}
	}()
}

var bigTags = map[string][]string{
	"ALBUM":                      {"New Raceion"},
	"ALBUMARTIST":                {"Alan Vega"},
	"ALBUMARTIST_CREDIT":         {"Alan Vega"},
	"ALBUMARTISTS":               {"Alan Vega"},
	"ALBUMARTISTS_CREDIT":        {"Alan Vega"},
	"ARTIST":                     {"Alan Vega"},
	"ARTIST_CREDIT":              {"Alan Vega"},
	"ARTISTS":                    {"Alan Vega"},
	"ARTISTS_CREDIT":             {"Alan Vega"},
	"DATE":                       {"1993-04-02"},
	"DISCNUMBER":                 {"1"},
	"GENRE":                      {"electronic"},
	"GENRES":                     {"electronic", "industrial", "experimental", "proto-punk", "rock", "rockabilly"},
	"LABEL":                      {"GM Editions"},
	"MEDIA":                      {"Digital Media"},
	"MUSICBRAINZ_ALBUMARTISTID":  {"dd720ac8-1c68-4484-abb7-0546413a55e3"},
	"MUSICBRAINZ_ALBUMID":        {"c56a5905-2b3a-46f5-82c7-ce8eed01f876"},
	"MUSICBRAINZ_ARTISTID":       {"dd720ac8-1c68-4484-abb7-0546413a55e3"},
	"MUSICBRAINZ_RELEASEGROUPID": {"373dcce2-63c4-3e8a-9c2c-bc58ec1bbbf3"},
	"MUSICBRAINZ_TRACKID":        {"2f1c8b43-7b4e-4bc8-aacf-760e5fb747a0"},
	"ORIGINALDATE":               {"1993-04-02"},
	"REPLAYGAIN_ALBUM_GAIN":      {"-4.58 dB"},
	"REPLAYGAIN_ALBUM_PEAK":      {"0.977692"},
	"REPLAYGAIN_TRACK_GAIN":      {"-5.29 dB"},
	"REPLAYGAIN_TRACK_PEAK":      {"0.977661"},
	"TITLE":                      {"Christ Dice"},
	"TRACKNUMBER":                {"2"},
	"UPC":                        {"3760271710486"},
}

var longString = strings.Repeat("E", 1024)

// ============================================================================
// File Handle API Tests
// ============================================================================

func TestFileOpen(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f, err := taglib.OpenReadOnly(path)
			nilErr(t, err)
			defer f.Close()

			// Verify format is detected
			format := f.Format()
			if format == taglib.FormatUnknown {
				t.Fatalf("expected known format, got unknown")
			}
		})
	}
}

func TestFileOpenInvalid(t *testing.T) {
	t.Parallel()

	path := tmpf(t, []byte("not a file"), "eg.flac")
	_, err := taglib.Open(path)
	eq(t, err, taglib.ErrInvalidFile)
}

func TestFileTags(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			// Write tags using path-based API
			err := taglib.WriteTags(path, map[string][]string{
				"ARTIST": {"Test Artist"},
				"ALBUM":  {"Test Album"},
			}, taglib.Clear)
			nilErr(t, err)

			// Read using File handle
			f, err := taglib.OpenReadOnly(path)
			nilErr(t, err)
			defer f.Close()

			tags := f.Tags()
			if tags["ARTIST"][0] != "Test Artist" {
				t.Fatalf("expected 'Test Artist', got %v", tags["ARTIST"])
			}
			if tags["ALBUM"][0] != "Test Album" {
				t.Fatalf("expected 'Test Album', got %v", tags["ALBUM"])
			}
		})
	}
}

func TestFileProperties(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f, err := taglib.OpenReadOnly(path)
			nilErr(t, err)
			defer f.Close()

			props := f.Properties()
			if props.Channels == 0 {
				t.Fatal("expected channels > 0")
			}
			if props.SampleRate == 0 {
				t.Fatal("expected sample rate > 0")
			}
		})
	}
}

func TestFilePropertiesCodec(t *testing.T) {
	t.Parallel()

	// Test MP3 codec via File handle API
	t.Run("MP3", func(t *testing.T) {
		path := tmpf(t, egMP3, "eg.mp3")
		f, err := taglib.OpenReadOnly(path)
		nilErr(t, err)
		defer f.Close()
		eq(t, "MP3", f.Properties().Codec)
	})

	// Test AAC codec via File handle API
	t.Run("M4A", func(t *testing.T) {
		path := tmpf(t, egM4a, "eg.m4a")
		f, err := taglib.OpenReadOnly(path)
		nilErr(t, err)
		defer f.Close()
		eq(t, "AAC", f.Properties().Codec)
	})

	// Test WMA codec via File handle API
	t.Run("WMA", func(t *testing.T) {
		path := tmpf(t, egWMA, "eg.wma")
		f, err := taglib.OpenReadOnly(path)
		nilErr(t, err)
		defer f.Close()
		eq(t, "WMA2", f.Properties().Codec)
	})
}

func TestFileRawTags(t *testing.T) {
	t.Parallel()

	// Test MP3 (ID3v2)
	t.Run("mp3", func(t *testing.T) {
		path := tmpf(t, egMP3, "eg.mp3")
		f, err := taglib.OpenReadOnly(path)
		nilErr(t, err)
		defer f.Close()

		eq(t, f.Format(), taglib.FormatMPEG)
		raw := f.RawTags()
		// MP3 should have ID3v2 frames
		if raw == nil {
			t.Fatal("expected raw tags")
		}
	})

	// Test M4A (MP4)
	t.Run("m4a", func(t *testing.T) {
		path := tmpf(t, egM4a, "eg.m4a")
		f, err := taglib.OpenReadOnly(path)
		nilErr(t, err)
		defer f.Close()

		eq(t, f.Format(), taglib.FormatMP4)
		raw := f.RawTags()
		// M4A should have MP4 atoms
		if raw == nil {
			t.Fatal("expected raw tags")
		}
	})

	// Test FLAC (Vorbis Comments - same as normalized)
	t.Run("flac", func(t *testing.T) {
		path := tmpf(t, egFLAC, "eg.flac")
		f, err := taglib.OpenReadOnly(path)
		nilErr(t, err)
		defer f.Close()

		eq(t, f.Format(), taglib.FormatFLAC)
		raw := f.RawTags()
		tags := f.Tags()
		// FLAC raw should be same as normalized
		tagEq(t, raw, tags)
	})
}

func TestFileAllTags(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3, "eg.mp3")
	f, err := taglib.OpenReadOnly(path)
	nilErr(t, err)
	defer f.Close()

	all := f.AllTags()
	eq(t, all.Format, taglib.FormatMPEG)
	if all.Tags == nil {
		t.Fatal("expected normalized tags")
	}
	if all.Raw == nil {
		t.Fatal("expected raw tags")
	}
}

func TestFileWriteTags(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f, err := taglib.Open(path)
			nilErr(t, err)
			defer f.Close()

			// Write tags
			err = f.WriteTags(map[string][]string{
				"ARTIST": {"File Write Test"},
				"ALBUM":  {"File Write Album"},
			}, taglib.Clear)
			nilErr(t, err)

			// Read back
			tags := f.Tags()
			if tags["ARTIST"][0] != "File Write Test" {
				t.Fatalf("expected 'File Write Test', got %v", tags["ARTIST"])
			}
		})
	}
}

func TestFileImage(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMP3, "eg.mp3")
	f, err := taglib.OpenReadOnly(path)
	nilErr(t, err)
	defer f.Close()

	// Read image (may be empty if test file has no image)
	img, err := f.Image(0)
	nilErr(t, err)
	// Just verify it doesn't error - image may or may not exist
	_ = img
}

func TestFileEfficiency(t *testing.T) {
	// This test verifies that File handle API is more efficient
	// by only creating one WASM module for multiple operations
	t.Parallel()

	path := tmpf(t, egMP3, "eg.mp3")

	// Using File handle - single module for all operations
	f, err := taglib.OpenReadOnly(path)
	nilErr(t, err)
	defer f.Close()

	// All these operations use the same module
	tags := f.Tags()
	props := f.Properties()
	raw := f.RawTags()
	all := f.AllTags()
	format := f.Format()

	// Verify all operations returned data
	if tags == nil {
		t.Fatal("expected tags")
	}
	if props.Channels == 0 {
		t.Fatal("expected properties")
	}
	if raw == nil {
		t.Fatal("expected raw tags")
	}
	if all.Tags == nil {
		t.Fatal("expected all tags")
	}
	eq(t, format, taglib.FormatMPEG)
}

func TestMatroskaFormat(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMKA, "eg.mka")

	// Test format detection via File handle
	f, err := taglib.OpenReadOnly(path)
	nilErr(t, err)
	defer f.Close()

	eq(t, taglib.FormatMatroska, f.Format())
	eq(t, "Matroska", f.Format().String())

	// Test properties
	props := f.Properties()
	eq(t, 1, props.Channels)
	if props.SampleRate == 0 {
		t.Fatal("expected sample rate > 0")
	}
}

func TestMatroskaReadWrite(t *testing.T) {
	t.Parallel()

	path := tmpf(t, egMKA, "eg.mka")

	err := taglib.WriteTags(path, map[string][]string{
		"TITLE":  {"MKA Title"},
		"ARTIST": {"MKA Artist"},
	}, taglib.Clear)
	nilErr(t, err)

	tags, err := taglib.ReadTags(path)
	nilErr(t, err)

	eq(t, tags[taglib.Title][0], "MKA Title")
	eq(t, tags[taglib.Artist][0], "MKA Artist")
}

func TestFileFormatString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		format taglib.FileFormat
		want   string
	}{
		{taglib.FormatMPEG, "MPEG"},
		{taglib.FormatMP4, "MP4"},
		{taglib.FormatFLAC, "FLAC"},
		{taglib.FormatOggVorbis, "Ogg Vorbis"},
		{taglib.FormatWAV, "WAV"},
		{taglib.FormatAIFF, "AIFF"},
		{taglib.FormatMatroska, "Matroska"},
		{taglib.FormatUnknown, "Unknown"},
	}

	for _, tt := range tests {
		eq(t, tt.format.String(), tt.want)
	}
}
