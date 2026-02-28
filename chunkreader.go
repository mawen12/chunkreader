// Package chunkreader provides an io.Reader wrapper that minimizes IO reads and memory allocations.
package chunkreader

import (
	"io"
)

// ChunkReader is a io.Reader wrapper that minimizes IO reads and memory allocations. It allocates memory in chunks and
// will read as much as will fit in the current buffer in a single call regardless of how large a read is actually
// requested. The memory returned via Next is owned by the caller. This avoids the need for an additional copy.
//
// The downside of this approach is that a large buffer can be pinned in memory even if only a small slice is
// referenced. For example, an entire 4096 byte block could be pinned in memory by even a 1 byte slice. In these rare
// cases it would be advantageous to copy the bytes to another slice.

/*
ChunkReader 一个是 io.Reader 包装器，它最小化了 IO 读取和内存分配。

	它以块为单位来分配内存，并且无论实际请求的读取量有多大，都会在
	一次调用中读取当前缓冲区能够容纳的所有数据。

	r
		负责读取字节的底层实现

	buf
		块单位的存储字节。默认为 4096 长度。

	rp
		读取位置，
		其中从 rp 到 wp 之间的内容，是可读的内容

	wp
		写入位置

	config
		配置
*/
type ChunkReader struct {
	r io.Reader

	buf    []byte
	rp, wp int // buf read position and write position

	config Config
}

// Config contains configuration parameters for ChunkReader.

/*
Config 包含了用于 ChunkReader 的配置参数

	MinBufLen
		最小缓冲区长度
*/
type Config struct {
	MinBufLen int // Minimum buffer length
}

// New creates and returns a new ChunkReader for r with default configuration.

// New 创建并返回一个新的带有默认配置的 r 的 ChunkReader
func New(r io.Reader) *ChunkReader {
	cr, err := NewConfig(r, Config{})
	if err != nil {
		panic("default config can't be bad")
	}

	return cr
}

// NewConfig creates and a new ChunkReader for r configured by config.

// NewConfig 创建一个新的带有指定配置的 r 的 ChunkReader
func NewConfig(r io.Reader, config Config) (*ChunkReader, error) {
	// 默认为 4096
	if config.MinBufLen == 0 {
		config.MinBufLen = 4096
	}

	return &ChunkReader{
		r:      r,
		buf:    make([]byte, config.MinBufLen),
		config: config,
	}, nil
}

// Next returns buf filled with the next n bytes. The caller gains ownership of buf. It is not necessary to make a copy
// of buf. If an error occurs, buf will be nil.

// Next 返回一个填充了接下来 n 个字节的缓冲区。
// 调用者获取 buf 的所有权，无需复制 bug。
// 如果发生错误，buf 将为 nil
func (r *ChunkReader) Next(n int) (buf []byte, err error) {
	// n bytes already in buf
	// buf 中已有 n 个字节数
	if (r.wp - r.rp) >= n {
		// 从 read postion 开始读取 n 字节
		buf = r.buf[r.rp : r.rp+n]
		// read position 向前移动对应 n 个字节
		r.rp += n
		return buf, err
	}

	// available space in buf is less than n
	// buf 中的空间不满足 n，需要对 n 进行扩容
	if len(r.buf) < n {
		// 按照 n 的大小进行扩容，并将内容复制到新的 buf 中
		r.copyBufContents(r.newBuf(n))
	}

	// buf is large enough, but need to shift filled area to start to make enough contiguous space
	// buf 空间足够，但是需要移动填充区域以便形成足够的连续空间，
	// 即出现数据碎片化的问题，buf 整体足够记录 n 字节数据，但是因为 read position 没有位于开头，
	// 导致 read position 之前空间没办法使用，此时需要进行碎片化整理，将 rp 到 wp 之间的数据移动到开头。
	// r.wp - r.rp 是当前已读的内容，此时计算需要再读的数量，才能满足 n 字节
	minReadCount := n - (r.wp - r.rp)
	// 当发现从 write positon 到末尾的空间不够承载要读取的字节数，此时需要扩容
	if (len(r.buf) - r.wp) < minReadCount {
		newBuf := r.newBuf(n)
		r.copyBufContents(newBuf)
	}

	//
	if err := r.appendAtLeast(minReadCount); err != nil {
		return nil, err
	}

	buf = r.buf[r.rp : r.rp+n]
	r.rp += n
	return buf, nil
}

// appendAtLeast 读取至少 fillLen 字节的数据
func (r *ChunkReader) appendAtLeast(fillLen int) error {
	n, err := io.ReadAtLeast(r.r, r.buf[r.wp:], fillLen)
	// 移动 wp
	r.wp += n
	return err
}

// newBuf 基于 Min[size, 1024] 创建一个新的 buf 切片
func (r *ChunkReader) newBuf(size int) []byte {
	if size < r.config.MinBufLen {
		size = r.config.MinBufLen
	}
	return make([]byte, size)
}

// copyBufContents 将当前 buf 中未读的内容复制到新的 buf 中，并重置 read position
//
// 该方法本质上是对数据进行碎片化整理。假设 byte[100]，rp = 50, wp = 60。
// 此时已有数据区域是 [50, 60），[60, 100）是可以读取的，但是 [0, 50] 也是空闲的空间，
// 却没有办法使用，因此需要将 [50, 60) 移动到 [0, 10)，将空间释放出来
func (r *ChunkReader) copyBufContents(dest []byte) {
	// 将从 read postion 到 write postion 这段内容复制到新的 dest 中
	r.wp = copy(dest, r.buf[r.rp:r.wp])
	// 重置 read position
	r.rp = 0
	// 将新的 buf 替换旧的 buf
	r.buf = dest
}
