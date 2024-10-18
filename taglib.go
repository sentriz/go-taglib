package taglib

import (
	"bytes"
	"context"
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

var ErrInvalidFile = fmt.Errorf("invalid file")
var ErrSavingFile = fmt.Errorf("can't save file")

type File struct {
	ptr        uint32
	properties func() []int
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
	return File{
		ptr: f,
		properties: sync.OnceValue(func() []int {
			return taglibFileAudioProperties(f)
		}),
	}, nil
}

func (f File) ReadTags() map[string][]string {
	var m = map[string][]string{}
	for _, row := range taglibFileTags(f.ptr) {
		k, v, ok := strings.Cut(row, "\t")
		if !ok {
			continue
		}
		m[k] = append(m[k], v)
	}
	return m
}

func (f File) Length() time.Duration {
	return time.Duration(f.properties()[audioPropertyLengthInMilliseconds]) * time.Millisecond
}
func (f File) Bitrate() int {
	return f.properties()[audioPropertyBitrate]
}
func (f File) SampleRate() int {
	return f.properties()[audioPropertySampleRate]
}
func (f File) Channels() int {
	return f.properties()[audioPropertyChannels]
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
	return stk.getPtr()
}

func taglibFileValid(file uint32) bool {
	defer stk.reset()()
	stk.int(file)
	call(&stk, "taglib_file_is_valid")
	return stk.getBool()
}

func taglibFileTags(file uint32) []string {
	defer stk.reset()()
	stk.int(file)
	call(&stk, "taglib_file_tags")
	return stk.getStrings()
}

const (
	audioPropertyLengthInMilliseconds = iota
	audioPropertyChannels
	audioPropertySampleRate
	audioPropertyBitrate
	audioPropertyLen
)

func taglibFileAudioProperties(file uint32) []int {
	defer stk.reset()()
	stk.int(file)
	call(&stk, "taglib_file_audioproperties")
	return stk.getInts(audioPropertyLen)
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
	return stk.getBool()
}

var mstk stack

func malloc(size uint64) uint32 {
	defer mstk.reset()()
	mstk.int(uint32(size))
	call(&mstk, "malloc")
	return mstk.getPtr()
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

func (stk *stack) getPtr() uint32        { return uint32(stk.stack[0]) }
func (stk *stack) getInt() int           { return int(stk.stack[0]) }
func (stk *stack) getInts(len int) []int { return readInts(uint32(stk.stack[0]), len) }
func (stk *stack) getBool() bool         { return stk.stack[0] == 1 }
func (stk *stack) getString() string     { return readString(uint32(stk.stack[0])) }
func (stk *stack) getStrings() []string  { return readStrings(uint32(stk.stack[0])) }

// readString reads a null terminated string at ptr
func readString(ptr uint32) string {
	defer func() {
		free(ptr)
	}()

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
	defer func() {
		free(ptr)
	}()

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

// readInts reads a null terminated int array at ptr
func readInts(ptr uint32, len int) []int {
	defer func() {
		free(ptr)
	}()

	ints := make([]int, 0, len)
	for i := range len {
		i, ok := module.Memory().ReadUint32Le(ptr + uint32(4*i))
		if !ok {
			panic("memory error")
		}
		ints = append(ints, int(i))
	}
	return ints
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
