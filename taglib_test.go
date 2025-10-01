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

	img, err := taglib.ReadImage(path)
	nilErr(t, err)

	if img == nil {
		t.Fatalf("expected image, found none")
	}

	nilErr(t, taglib.WriteImage(path, nil))

	img, err = taglib.ReadImage(path)
	nilErr(t, err)

	if img != nil {
		t.Fatalf("expected no image, found one")
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
