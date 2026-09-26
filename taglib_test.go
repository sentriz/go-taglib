package taglib_test

import (
	"bytes"
	_ "embed"
	"encoding/binary"
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

func TestReadInvalidUTF8(t *testing.T) {
	t.Parallel()

	// Vorbis comments must be UTF-8, but old taggers wrote Windows-1252
	path := tmpf(t, withVorbisComments(t, egFLAC,
		"ALBUM=Fijaci\xf3n Oral",
		"TITLE=\x93Quoted\x94 \x80 caf\xe9",
		"ARTIST=Brian Eno—David Byrne",
	), "eg.flac")

	tags, err := taglib.ReadTags(path)
	nilErr(t, err)
	tagEq(t, tags, map[string][]string{
		taglib.Album:  {"Fijación Oral"},
		taglib.Title:  {"“Quoted” € café"},
		taglib.Artist: {"Brian Eno—David Byrne"},
	})
}

func TestWriteInvalidUTF8(t *testing.T) {
	t.Parallel()

	path := tmpf(t, withVorbisComments(t, egFLAC, "ALBUM=Fijaci\xf3n Oral"), "eg.flac")

	err := taglib.WriteTags(path, map[string][]string{taglib.Title: {"New"}}, 0)
	nilErr(t, err)

	tags, err := taglib.ReadTags(path)
	nilErr(t, err)
	tagEq(t, tags, map[string][]string{
		taglib.Album: {"Fijación Oral"},
		taglib.Title: {"New"},
	})

	// the untouched field was saved back as valid UTF-8
	b, err := os.ReadFile(path)
	nilErr(t, err)
	if !bytes.Contains(b, []byte("ALBUM=Fijación Oral")) {
		t.Fatalf("album not rewritten as UTF-8")
	}
}

func TestReadUnpairedSurrogate(t *testing.T) {
	t.Parallel()

	// UTF-16LE "A", a high surrogate with no low surrogate after it, "B"
	path := tmpf(t, withID3v2Title(t, egMP3, []byte{'A', 0, 0x00, 0xd8, 'B', 0}), "eg.mp3")

	tags, err := taglib.ReadTags(path)
	nilErr(t, err)
	eq(t, len(tags[taglib.Title]), 1)
	eq(t, tags[taglib.Title][0], "A�B")
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
	eq(t, 1460, properties.BitRate)
	eq(t, 48_000, properties.SampleRate)
	eq(t, 2, properties.Channels)
	eq(t, "flac", properties.Format)
	eq(t, "", properties.InnerCodec)
	eq(t, 24, properties.BitDepth)

	eq(t, len(properties.Images), 2)
	eq(t, properties.Images[0].Type, "Front Cover")
	eq(t, properties.Images[0].Description, "The first image")
	eq(t, properties.Images[0].MIMEType, "image/png")
	eq(t, properties.Images[1].Type, "Lead Artist")
	eq(t, properties.Images[1].Description, "The second image")
	eq(t, properties.Images[1].MIMEType, "image/jpeg")
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
	//go:embed testdata/cover.jpg
	coverJPG []byte
)

func testPaths(t testing.TB) []string {
	return []string{
		tmpf(t, egFLAC, "eg.flac"),
		tmpf(t, egMP3, "eg.mp3"),
		tmpf(t, egM4a, "eg.m4a"),
		tmpf(t, egWAV, "eg.wav"),
		tmpf(t, egOgg, "eg.ogg"),
	}
}

func tmpf(t testing.TB, b []byte, name string) string {
	p := filepath.Join(t.TempDir(), name)
	err := os.WriteFile(p, b, os.ModePerm)
	nilErr(t, err)
	return p
}

// withVorbisComments returns flac with its Vorbis comment block replaced by one
// holding fields as raw bytes, so tests can store text that isn't valid UTF-8.
func withVorbisComments(t testing.TB, flac []byte, fields ...string) []byte {
	t.Helper()

	comment := binary.LittleEndian.AppendUint32(nil, 0) // empty vendor string
	comment = binary.LittleEndian.AppendUint32(comment, uint32(len(fields)))
	for _, f := range fields {
		comment = binary.LittleEndian.AppendUint32(comment, uint32(len(f)))
		comment = append(comment, f...)
	}

	if string(flac[:4]) != "fLaC" {
		t.Fatalf("not a flac file")
	}
	out := slices.Clone(flac[:4])
	for pos := 4; ; {
		header := flac[pos]
		size := int(flac[pos+1])<<16 | int(flac[pos+2])<<8 | int(flac[pos+3])
		body := flac[pos+4 : pos+4+size]
		if header&0x7f == 4 { // VORBIS_COMMENT
			body = comment
		}
		out = append(out, header, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
		out = append(out, body...)
		pos += 4 + size
		if header&0x80 != 0 { // last metadata block
			return append(out, flac[pos:]...)
		}
	}
}

// withID3v2Title returns mp3 with its ID3v2 tag replaced by an ID3v2.3 tag
// holding a single TIT2 frame of raw UTF-16LE text.
func withID3v2Title(t testing.TB, mp3 []byte, utf16le []byte) []byte {
	t.Helper()

	if string(mp3[:3]) != "ID3" {
		t.Fatalf("no ID3v2 tag")
	}
	oldSize := int(mp3[6])<<21 | int(mp3[7])<<14 | int(mp3[8])<<7 | int(mp3[9])

	text := append([]byte{1, 0xff, 0xfe}, utf16le...) // UTF-16 with a little-endian BOM
	frame := binary.BigEndian.AppendUint32([]byte("TIT2"), uint32(len(text)))
	frame = append(frame, 0, 0) // frame flags
	frame = append(frame, text...)

	// header: v2.3, no flags, then the tag size as a syncsafe integer
	size := len(frame)
	out := []byte{'I', 'D', '3', 3, 0, 0, byte(size >> 21 & 0x7f), byte(size >> 14 & 0x7f), byte(size >> 7 & 0x7f), byte(size & 0x7f)}
	out = append(out, frame...)
	return append(out, mp3[10+oldSize:]...)
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
