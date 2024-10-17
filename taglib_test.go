package taglib_test

import (
	_ "embed"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.senan.xyz/taglib-wasm"
	_ "go.senan.xyz/taglib-wasm/embed"
)

//go:embed testdata/eg.flac
var egFlac []byte

func TestNew(t *testing.T) {
	path := tmpf(t, egFlac, "eg.flac")
	_, err := taglib.New(path)
	nilErr(t, err)
}

func TestFile(t *testing.T) {
	path := tmpf(t, egFlac, "eg.flac")
	f, err := taglib.New(path)
	nilErr(t, err)
	defer f.Close()

	artists := f.GetTag("artist")
	eq(t, len(artists), 2)
	eq(t, artists[0], "Artist One")
	eq(t, artists[1], "Artist Two")

	f.SetTag("artist")

	artists = f.GetTag("artist")
	eq(t, len(artists), 0)

	f.SetTag("artist", "ABC")

	artists = f.GetTag("artist")
	eq(t, len(artists), 1)
	eq(t, artists[0], "ABC")

	f.SetTag("artist", "ABC", "DEF")

	artists = f.GetTag("artist")
	eq(t, len(artists), 2)
	eq(t, artists[0], "ABC")
	eq(t, artists[1], "DEF")

	f.SetTag("artist", "💪")

	artists = f.GetTag("artist")
	eq(t, len(artists), 1)
	eq(t, artists[0], "💪")

	eq(t, 1*time.Second, f.Length())
	eq(t, 1460, f.Bitrate())
	eq(t, 48_000, f.SampleRate())
	eq(t, 2, f.Channels())

	err = f.Save()
	nilErr(t, err)
}

func TestMultiOpen(t *testing.T) {
	t.Skip("")

	{
		path := tmpf(t, egFlac, "eg.flac")
		f, err := taglib.New(path)
		nilErr(t, err)
		defer f.Close()
	}
	{
		path := tmpf(t, egFlac, "eg.flac")
		f, err := taglib.New(path)
		nilErr(t, err)
		defer f.Close()
	}
}

func BenchmarkOpen(b *testing.B) {
	path := tmpf(b, egFlac, "eg.flac")
	b.ResetTimer()

	for range b.N {
		f, err := taglib.New(path)
		nilErr(b, err)
		_ = maps.Collect(f.IterTags())
		f.Close()
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
