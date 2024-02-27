package llm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

type GGML struct {
	container
	model

	Size int64
}

const (
	fileTypeF32 uint32 = iota
	fileTypeF16
	fileTypeQ4_0
	fileTypeQ4_1
	fileTypeQ4_1_F16
	fileTypeQ8_0 uint32 = iota + 2
	fileTypeQ5_0
	fileTypeQ5_1
	fileTypeQ2_K
	fileTypeQ3_K_S
	fileTypeQ3_K_M
	fileTypeQ3_K_L
	fileTypeQ4_K_S
	fileTypeQ4_K_M
	fileTypeQ5_K_S
	fileTypeQ5_K_M
	fileTypeQ6_K
	fileTypeIQ2_XXS
	fileTypeIQ2_XS
	fileTypeQ2_K_S
	fileTypeQ3_K_XS
	fileTypeIQ3_XXS
)

func fileType(fileType uint32) string {
	switch fileType {
	case fileTypeF32:
		return "F32"
	case fileTypeF16:
		return "F16"
	case fileTypeQ4_0:
		return "Q4_0"
	case fileTypeQ4_1:
		return "Q4_1"
	case fileTypeQ4_1_F16:
		return "Q4_1_F16"
	case fileTypeQ8_0:
		return "Q8_0"
	case fileTypeQ5_0:
		return "Q5_0"
	case fileTypeQ5_1:
		return "Q5_1"
	case fileTypeQ2_K:
		return "Q2_K"
	case fileTypeQ3_K_S:
		return "Q3_K_S"
	case fileTypeQ3_K_M:
		return "Q3_K_M"
	case fileTypeQ3_K_L:
		return "Q3_K_L"
	case fileTypeQ4_K_S:
		return "Q4_K_S"
	case fileTypeQ4_K_M:
		return "Q4_K_M"
	case fileTypeQ5_K_S:
		return "Q5_K_S"
	case fileTypeQ5_K_M:
		return "Q5_K_M"
	case fileTypeQ6_K:
		return "Q6_K"
	case fileTypeIQ2_XXS:
		return "IQ2_XXS"
	case fileTypeIQ2_XS:
		return "IQ2_XS"
	case fileTypeQ2_K_S:
		return "Q2_K_S"
	case fileTypeQ3_K_XS:
		return "Q3_K_XS"
	case fileTypeIQ3_XXS:
		return "IQ3_XXS"
	default:
		return "unknown"
	}
}

type kv map[string]any

func (kv kv) u64(key string) uint64 {
	switch v := kv[key].(type) {
	case uint64:
		return v
	case uint32:
		return uint64(v)
	case float64:
		return uint64(v)
	default:
		return 0
	}
}

func (kv kv) Architecture() string {
	if s, ok := kv["general.architecture"].(string); ok {
		return s
	}

	return "unknown"
}

func (kv kv) ParameterCount() uint64 {
	return kv.u64("general.parameter_count")
}

func (kv kv) FileType() string {
	if u64 := kv.u64("general.file_type"); u64 > 0 {
		return fileType(uint32(u64))
	}

	return "unknown"
}

func (kv kv) BlockCount() uint64 {
	return kv.u64(fmt.Sprintf("%s.block_count", kv.Architecture()))
}

func (kv kv) HeadCount() uint64 {
	return kv.u64(fmt.Sprintf("%s.attention.head_count", kv.Architecture()))
}

func (kv kv) HeadCountKV() uint64 {
	return kv.u64(fmt.Sprintf("%s.attention.head_count_kv", kv.Architecture()))
}

func (kv kv) GQA() uint64 {
	if headCountKV := kv.HeadCountKV(); headCountKV > 0 {
		return kv.HeadCount() / headCountKV
	}

	return 0
}

func (kv kv) EmbeddingLength() uint64 {
	return kv.u64(fmt.Sprintf("%s.embedding_length", kv.Architecture()))
}

func (kv kv) ContextLength() uint64 {
	return kv.u64(fmt.Sprintf("%s.embedding_length", kv.Architecture()))
}

type tensor struct {
	Name string `json:"name"`
	Kind uint32 `json:"kind"`

	// Shape is the number of elements in each dimension
	Shape [4]uint64 `json:"shape"`

	offset uint64
}

func (t tensor) blockSize() uint64 {
	switch {
	case t.Kind < 2:
		return 1
	case t.Kind < 10:
		return 32
	default:
		return 256
	}
}

func (t tensor) typeSize() uint64 {
	blockSize := t.blockSize()

	switch t.Kind {
	case 0: // FP32
		return 4
	case 1: // FP16
		return 2
	case 2: // Q4_0
		return 2 + blockSize/2
	case 3: // Q4_1
		return 2 + 2 + blockSize/2
	case 6: // Q5_0
		return 2 + 4 + blockSize/2
	case 7: // Q5_1
		return 2 + 2 + 4 + blockSize/2
	case 8: // Q8_0
		return 2 + blockSize
	case 9: // Q8_1
		return 4 + 4 + blockSize
	case 10: // Q2_K
		return blockSize/16 + blockSize/4 + 2 + 2
	case 11: // Q3_K
		return blockSize/8 + blockSize/4 + 12 + 2
	case 12: // Q4_K
		return 2 + 2 + 12 + blockSize/2
	case 13: // Q5_K
		return 2 + 2 + 12 + blockSize/8 + blockSize/2
	case 14: // Q6_K
		return blockSize/2 + blockSize/4 + blockSize/16 + 2
	case 15: // Q8_K
		return 2 + blockSize + 2*blockSize/16
	case 16: // IQ2_XXS
		return 2 + 2*blockSize/8
	case 17: // IQ2_XS
		return 2 + 2*blockSize/8 + blockSize/32
	case 18: // IQ3_XXS
		return 2 + 3*blockSize/8
	default:
		return 0
	}
}

func (t tensor) parameters() uint64 {
	return t.Shape[0] * t.Shape[1] * t.Shape[2] * t.Shape[3]
}

func (t tensor) size() uint64 {
	return t.parameters() * t.typeSize() / t.blockSize()
}

type model interface {
	KV() kv
	Tensor() []tensor
}

type container interface {
	Name() string
	Decode(*readSeekOffset) (model, error)
}

const (
	// Magic constant for `ggml` files (unversioned).
	FILE_MAGIC_GGML = 0x67676d6c
	// Magic constant for `ggml` files (versioned, ggmf).
	FILE_MAGIC_GGMF = 0x67676d66
	// Magic constant for `ggml` files (versioned, ggjt).
	FILE_MAGIC_GGJT = 0x67676a74
	// Magic constant for `ggla` files (LoRA adapter).
	FILE_MAGIC_GGLA = 0x67676C61
	// Magic constant for `gguf` files (versioned, gguf)
	FILE_MAGIC_GGUF_LE = 0x46554747
	FILE_MAGIC_GGUF_BE = 0x47475546
)

var ErrUnsupportedFormat = errors.New("unsupported model format")

func DecodeGGML(r io.ReadSeeker) (*GGML, error) {
	ro := readSeekOffset{ReadSeeker: r}

	var magic uint32
	if err := binary.Read(&ro, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}

	var c container
	switch magic {
	case FILE_MAGIC_GGML, FILE_MAGIC_GGMF, FILE_MAGIC_GGJT:
		return nil, ErrUnsupportedFormat
	case FILE_MAGIC_GGLA:
		c = &containerGGLA{}
	case FILE_MAGIC_GGUF_LE:
		c = &containerGGUF{bo: binary.LittleEndian}
	case FILE_MAGIC_GGUF_BE:
		c = &containerGGUF{bo: binary.BigEndian}
	default:
		return nil, errors.New("invalid file magic")
	}

	model, err := c.Decode(&ro)
	if err != nil {
		return nil, err
	}

	// final model type
	return &GGML{
		container: c,
		model:     model,
		Size:      ro.offset,
	}, nil
}

type readSeekOffset struct {
	io.ReadSeeker
	offset int64
}

func (rso *readSeekOffset) Seek(offset int64, whence int) (int64, error) {
	offset, err := rso.ReadSeeker.Seek(offset, whence)
	if err != nil {
		return 0, err
	}

	rso.offset = offset
	return offset, nil
}

func (rso *readSeekOffset) Read(p []byte) (int, error) {
	n, err := rso.ReadSeeker.Read(p)
	rso.offset += int64(n)
	return n, err
}
