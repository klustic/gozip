package gozip

import (
	"bytes"
	"compress/flate"
	"hash"
	"hash/crc32"
)

// A Writer that DEFLATE compresses while computing the CRC32 of the uncompressed data
type CRCCompressorWriter struct {
	buf *bytes.Buffer
	h   hash.Hash32
	w   *flate.Writer
}

func NewCRCCompressor() (*CRCCompressorWriter, error) {
	var temp bytes.Buffer
	w, err := flate.NewWriter(&temp, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	return &CRCCompressorWriter{
		buf: &temp,
		h:   crc32.NewIEEE(),
		w:   w,
	}, nil
}

func (c *CRCCompressorWriter) Write(p []byte) (n int, err error) {
	n, err = c.w.Write(p)
	c.h.Write(p)
	return
}

func (c *CRCCompressorWriter) Finish() (deflatedBuffer []byte, crc uint32) {
	c.w.Flush()
	c.w.Close()
	return c.buf.Bytes(), c.h.Sum32()
}
