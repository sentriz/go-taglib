package taglib

import (
	"bytes"
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var ErrInvalidFile = fmt.Errorf("invalid file")
var ErrSavingFile = fmt.Errorf("can't save file")

type File struct {
	ptr     uint32
	propPtr func() uint32
}

func New(path string) (File, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return File{}, fmt.Errorf("make path abs %w", err)
	}
	f := taglibFileNew(path)
	if !taglibFileValid(f) {
		return File{}, ErrInvalidFile
	}
	return File{}, nil
	return File{
		ptr: f,
		propPtr: sync.OnceValue(func() uint32 {
			return taglibFileAudioProperties(f)
		}),
	}, nil
}

type FileType int

const (
	MPEG FileType = iota
	OggVorbis
	FLAC
	MPC
	OggFlac
	WavPack
	Speex
	TrueAudio
	MP4
	ASF
	AIFF
	WAV
	APE
	IT
	Mod
	S3M
	XM
	Opus
	DSF
	DSDIFF
)

func NewType(path string, typ FileType) (File, error) {
	f := taglibFileNewType(path, uint32(typ))
	if !taglibFileValid(f) {
		return File{}, ErrInvalidFile
	}
	return File{ptr: f}, nil
}

func (f File) GetTag(key string) []string {
	return taglibPropertyGet(f.ptr, key)
}

func (f File) SetTag(key string, vs ...string) {
	if len(vs) == 0 {
		taglibPropertyClear(f.ptr, key)
		return
	}
	taglibPropertySet(f.ptr, key, vs[0])
	for _, v := range vs[1:] {
		taglibPropertySetAppend(f.ptr, key, v)
	}
}

func (f File) IterTagKeys() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, k := range taglibPropertyKeys(f.ptr) {
			if !yield(k) {
				break
			}
		}
	}
}
func (f File) IterTags() iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		for _, k := range taglibPropertyKeys(f.ptr) {
			if !yield(k, taglibPropertyGet(f.ptr, k)) {
				break
			}
		}
	}
}

func (f File) Length() time.Duration {
	return time.Duration(taglibAudioPropertiesLength(f.propPtr())) * time.Second
}
func (f File) Bitrate() int {
	return int(taglibAudioPropertiesBitrate(f.propPtr()))
}
func (f File) SampleRate() int {
	return int(taglibAudioPropertiesSamplerate(f.propPtr()))
}
func (f File) Channels() int {
	return int(taglibAudioPropertiesChannels(f.propPtr()))
}

func (f File) Save() error {
	if !taglibFileSave(f.ptr) {
		return ErrSavingFile
	}
	return nil
}

func (f File) Close() {
	taglibFileFree(f.ptr)
}

var stk stack

func taglibFileNew(filename string) uint32 {
	defer stk.reset()()
	stk.string(filename)
	call(&stk, "taglib_file_new")
	return stk.rint()
}

func taglibFileNewType(filename string, typ uint32) uint32 {
	defer stk.reset()()
	stk.string(filename)
	stk.int(typ)
	call(&stk, "taglib_file_new_type")
	return stk.rint()
}

func taglibFileValid(file uint32) bool {
	defer stk.reset()()
	stk.int(file)
	call(&stk, "taglib_file_is_valid")
	return stk.rbool()
}

func taglibPropertyKeys(file uint32) []string {
	defer stk.reset()()
	stk.int(file)
	call(&stk, "taglib_property_keys")
	return stk.rstrings()
}

func taglibPropertyClear(file uint32, property string) {
	defer stk.reset()()
	stk.int(file)
	stk.string(property)
	stk.int(0)
	call(&stk, "taglib_property_set_append")
}

func taglibPropertySet(file uint32, property string, value string) {
	defer stk.reset()()
	stk.int(file)
	stk.string(property)
	stk.string(value)
	call(&stk, "taglib_property_set")
}

func taglibPropertySetAppend(file uint32, property string, value string) {
	defer stk.reset()()
	stk.int(file)
	stk.string(property)
	stk.string(value)
	call(&stk, "taglib_property_set_append")
}

func taglibPropertyGet(file uint32, property string) []string {
	defer stk.reset()()
	stk.int(file)
	stk.string(property)
	call(&stk, "taglib_property_get")
	return stk.rstrings()
}

func taglibFileAudioProperties(file uint32) uint32 {
	defer stk.reset()()
	stk.int(file)
	call(&stk, "taglib_file_audioproperties")
	return stk.rint()
}

func taglibAudioPropertiesLength(properties uint32) uint32 {
	defer stk.reset()()
	stk.int(properties)
	call(&stk, "taglib_audioproperties_length")
	return stk.rint()
}

func taglibAudioPropertiesBitrate(properties uint32) uint32 {
	defer stk.reset()()
	stk.int(properties)
	call(&stk, "taglib_audioproperties_bitrate")
	return stk.rint()
}

func taglibAudioPropertiesSamplerate(properties uint32) uint32 {
	defer stk.reset()()
	stk.int(properties)
	call(&stk, "taglib_audioproperties_samplerate")
	return stk.rint()
}

func taglibAudioPropertiesChannels(properties uint32) uint32 {
	defer stk.reset()()
	stk.int(properties)
	call(&stk, "taglib_audioproperties_channels")
	return stk.rint()
}

func taglibFileFree(file uint32) {
	defer stk.reset()()
	stk.int(file)
	call(&stk, "taglib_file_free")
}

func taglibFileSave(file uint32) bool {
	defer stk.reset()()
	stk.int(file)
	call(&stk, "taglib_file_save")
	return stk.rbool()
}

var mstk stack

func malloc(size uint64) uint32 {
	defer mstk.reset()()
	mstk.int(uint32(size))
	call(&mstk, "malloc")
	return mstk.rint()
}

func free(ptr uint32) {
	defer mstk.reset()()
	mstk.int(ptr)
	call(&mstk, "free")
}

func call(stk *stack, name string) {
	if err := module.ExportedFunction(name).CallWithStack(context.Background(), stk.stack); err != nil {
		panic(err)
	}
}

type stack struct {
	stack []uint64
	ptrs  []uint64
	mu    sync.Mutex
}

func (stk *stack) reset() (cleanup func()) {
	stk.mu.Lock()

	if stk.stack == nil {
		stk.stack = make([]uint64, 0, 8)
		stk.ptrs = make([]uint64, 0, 8)
	}
	stk.stack = stk.stack[:0]
	stk.ptrs = stk.ptrs[:0]

	return func() {
		for _, ptr := range stk.ptrs {
			free(uint32(ptr))
		}

		stk.mu.Unlock()
	}
}

func (stk *stack) int(i uint32) {
	stk.stack = append(stk.stack, uint64(i))
}

func (stk *stack) string(s string) {
	b := append([]byte(s), 0)

	ptr := malloc(uint64(len(b)))
	if !module.Memory().Write(ptr, b) {
		panic("failed to write to memory")
	}

	stk.stack = append(stk.stack, uint64(ptr))
	stk.ptrs = append(stk.ptrs, uint64(ptr))
}

func (stk *stack) rint() uint32       { return uint32(stk.stack[0]) }
func (stk *stack) rbool() bool        { return stk.stack[0] == 1 }
func (stk *stack) rstring() string    { return readString(uint32(stk.stack[0])) }
func (stk *stack) rstrings() []string { return readStrings(uint32(stk.stack[0])) }

// readString reads a null terminated string at ptr
func readString(ptr uint32) string {
	size := uint32(256)
	buf, ok := module.Memory().Read(ptr, size)
	if !ok {
		panic("memory error")
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		return string(buf[:i])
	}

	for {
		next, ok := module.Memory().Read(ptr+size, size)
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

// readString reads a null terminated string array at ptr
func readStrings(ptr uint32) []string {
	var strs []string
	for {
		stringPtr, ok := module.Memory().ReadUint32Le(ptr)
		if !ok {
			panic("memory error")
		}
		if stringPtr == 0 {
			break
		}
		str := readString(stringPtr)
		strs = append(strs, str)
		ptr += 4
	}
	return strs
}

var module api.Module

func LoadBinary(ctx context.Context, bin []byte) {
	runtime := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	hostmod := runtime.NewHostModuleBuilder("env")
	hostmod = hostmod.NewFunctionBuilder().WithFunc(func(int32) int32 { panic("__cxa_allocate_exception") }).Export("__cxa_allocate_exception")
	hostmod = hostmod.NewFunctionBuilder().WithFunc(func(int32, int32, int32) { panic("__cxa_throw") }).Export("__cxa_throw")
	if _, err := hostmod.Instantiate(ctx); err != nil {
		panic(err)
	}

	guest, err := runtime.CompileModule(ctx, bin)
	if err != nil {
		panic(err)
	}

	config := wazero.
		NewModuleConfig().
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithFSConfig(wazero.NewFSConfig().WithDirMount("/", "/"))

	module, err = runtime.InstantiateModule(ctx, guest, config)
	if err != nil {
		panic(err)
	}
}
