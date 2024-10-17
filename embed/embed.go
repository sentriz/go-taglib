package embed

import (
	"context"
	_ "embed"

	"go.senan.xyz/taglib-wasm"
)

//go:embed taglib.wasm
var binary []byte

func init() {
	taglib.LoadBinary(context.Background(), binary)
}
