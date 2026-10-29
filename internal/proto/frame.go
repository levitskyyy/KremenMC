package proto

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	MaxFrameSize = 2097151
	// MaxUncompressedSize is the vanilla limit for a decompressed packet.
	MaxUncompressedSize = 8388608
)

var (
	ErrFrameTooBig      = errors.New("proto: frame too big")
	ErrBadFrameLength   = errors.New("proto: bad frame length")
	ErrBadCompression   = errors.New("proto: bad compressed packet")
	ErrBelowThreshold   = errors.New("proto: compressed packet below threshold")
	ErrPacketTooBig     = errors.New("proto: decompressed packet too big")
	ErrEncryptionActive = errors.New("proto: encryption already enabled")
)

var zrPool sync.Pool

type zlibReader struct {
	src bytes.Reader
	zr  io.ReadCloser
}

func getZlibReader(p []byte) (*zlibReader, error) {
	z, _ := zrPool.Get().(*zlibReader)
	if z == nil {
		z = &zlibReader{}
	}
	z.src.Reset(p)
	if z.zr == nil {
		zr, err := zlib.NewReader(&z.src)
		if err != nil {
			return nil, err
		}
		z.zr = zr
		return z, nil
	}
	if err := z.zr.(zlib.Resetter).Reset(&z.src, nil); err != nil {
		zrPool.Put(z)
		return nil, err
	}
	return z, nil
}

type sliceWriter struct{ b []byte }

func (s *sliceWriter) Write(p []byte) (int, error) {
	s.b = append(s.b, p...)
	return len(p), nil
}

type zlibWriter struct {
	out sliceWriter
	zw  *zlib.Writer
}

var zwPools [10]sync.Pool

func getZlibWriter(level int) *zlibWriter {
	if level < 0 || level > 9 {
		level = 6
	}
	z, _ := zwPools[level].Get().(*zlibWriter)
	if z == nil {
		z = &zlibWriter{}
		z.zw, _ = zlib.NewWriterLevel(&z.out, level)
		return z
	}
	z.zw.Reset(&z.out)
	return z
}

func putZlibWriter(level int, z *zlibWriter) {
	if level < 0 || level > 9 {
		level = 6
	}
	z.out.b = nil
	zwPools[level].Put(z)
}

// FrameReader reads length-prefixed packet frames, handling decryption and
// decompression. Returned slices are valid until the next ReadFrame call.
type FrameReader struct {
	r         io.Reader
	buf       []byte
	rpos      int
	rend      int
	dec       *CFB8
	threshold int
	frame     []byte
	out       []byte
}

func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{r: r, buf: make([]byte, 16<<10), threshold: -1}
}

// SetCompression enables compression for subsequent frames; threshold < 0 disables it.
func (f *FrameReader) SetCompression(threshold int) { f.threshold = threshold }

// EnableEncryption decrypts all bytes not yet consumed, including ones already buffered.
func (f *FrameReader) EnableEncryption(secret []byte) error {
	if f.dec != nil {
		return ErrEncryptionActive
	}
	d, err := NewCFB8Decrypter(secret)
	if err != nil {
		return err
	}
	f.dec = d
	pending := f.buf[f.rpos:f.rend]
	d.XORKeyStream(pending, pending)
	return nil
}

// Buffered returns the number of bytes read from the source but not yet consumed.
func (f *FrameReader) Buffered() int { return f.rend - f.rpos }

func (f *FrameReader) fill() error {
	if f.rpos > 0 {
		n := copy(f.buf, f.buf[f.rpos:f.rend])
		f.rpos, f.rend = 0, n
	}
	n, err := f.r.Read(f.buf[f.rend:])
	if n > 0 {
		if f.dec != nil {
			f.dec.XORKeyStream(f.buf[f.rend:f.rend+n], f.buf[f.rend:f.rend+n])
		}
		f.rend += n
		return nil
	}
	if err == nil {
		err = io.ErrNoProgress
	}
	return err
}

func (f *FrameReader) readLength() (int, error) {
	var v uint32
	for i := 0; i < 3; i++ {
		for f.rpos == f.rend {
			if err := f.fill(); err != nil {
				if i > 0 && err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return 0, err
			}
		}
		b := f.buf[f.rpos]
		f.rpos++
		v |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int(v), nil
		}
	}
	return 0, ErrBadFrameLength
}

// ReadFrame returns the next packet (ID followed by data), decompressed.
func (f *FrameReader) ReadFrame() ([]byte, error) {
	n, err := f.readLength()
	if err != nil {
		return nil, err
	}
	var frame []byte
	if f.rend-f.rpos >= n {
		frame = f.buf[f.rpos : f.rpos+n]
		f.rpos += n
	} else {
		if cap(f.frame) < n {
			f.frame = make([]byte, n)
		}
		frame = f.frame[:n]
		got := copy(frame, f.buf[f.rpos:f.rend])
		f.rpos = f.rend
		for got < n {
			if err := f.fill(); err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return nil, err
			}
			c := copy(frame[got:], f.buf[f.rpos:f.rend])
			f.rpos += c
			got += c
		}
	}
	if f.threshold < 0 {
		return frame, nil
	}
	return f.inflate(frame)
}

func (f *FrameReader) inflate(frame []byte) ([]byte, error) {
	r := Reader{B: frame}
	dl, err := r.VarInt()
	if err != nil {
		return nil, ErrBadCompression
	}
	if dl == 0 {
		return r.Rest(), nil
	}
	if int(dl) < f.threshold {
		return nil, ErrBelowThreshold
	}
	if dl < 0 || dl > MaxUncompressedSize {
		return nil, ErrPacketTooBig
	}
	z, err := getZlibReader(r.Rest())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadCompression, err)
	}
	defer zrPool.Put(z)
	if cap(f.out) < int(dl) {
		f.out = make([]byte, dl)
	}
	out := f.out[:dl]
	if _, err := io.ReadFull(z.zr, out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadCompression, err)
	}
	var probe [1]byte
	if n, err := z.zr.Read(probe[:]); n != 0 || err != io.EOF {
		return nil, ErrBadCompression
	}
	return out, nil
}

// FrameWriter frames packets into an internal buffer; Flush writes them out.
type FrameWriter struct {
	w         io.Writer
	buf       []byte
	enc       *CFB8
	threshold int
	level     int
}

func NewFrameWriter(w io.Writer) *FrameWriter {
	return &FrameWriter{w: w, buf: make([]byte, 0, 16<<10), threshold: -1, level: 6}
}

// SetCompression enables compression for packets of at least threshold bytes.
// threshold < 0 disables compression; level is a zlib level (-1 = default).
func (f *FrameWriter) SetCompression(threshold, level int) {
	f.threshold = threshold
	f.level = level
}

func (f *FrameWriter) EnableEncryption(secret []byte) error {
	if f.enc != nil {
		return ErrEncryptionActive
	}
	e, err := NewCFB8Encrypter(secret)
	if err != nil {
		return err
	}
	f.enc = e
	return nil
}

// Buffered returns the number of framed bytes waiting for Flush.
func (f *FrameWriter) Buffered() int { return len(f.buf) }

// WriteFrame frames one packet (ID followed by data) into the buffer.
func (f *FrameWriter) WriteFrame(packet []byte) error {
	start := len(f.buf)
	switch {
	case f.threshold < 0:
		if len(packet) > MaxFrameSize {
			return ErrFrameTooBig
		}
		f.buf = AppendVarInt(f.buf, int32(len(packet)))
		f.buf = append(f.buf, packet...)
	case len(packet) < f.threshold:
		if len(packet)+1 > MaxFrameSize {
			return ErrFrameTooBig
		}
		f.buf = AppendVarInt(f.buf, int32(len(packet)+1))
		f.buf = append(f.buf, 0)
		f.buf = append(f.buf, packet...)
	default:
		if len(packet) > MaxUncompressedSize {
			return ErrPacketTooBig
		}
		// Reserve the maximum 3-byte length so compressed data lands in place.
		f.buf = append(f.buf, 0, 0, 0)
		f.buf = AppendVarInt(f.buf, int32(len(packet)))
		z := getZlibWriter(f.level)
		z.out.b = f.buf
		_, err := z.zw.Write(packet)
		if err == nil {
			err = z.zw.Close()
		}
		f.buf = z.out.b
		putZlibWriter(f.level, z)
		if err != nil {
			f.buf = f.buf[:start]
			return fmt.Errorf("compress: %w", err)
		}
		n := len(f.buf) - start - 3
		if n > MaxFrameSize {
			f.buf = f.buf[:start]
			return ErrFrameTooBig
		}
		ls := VarIntSize(int32(n))
		if ls < 3 {
			copy(f.buf[start+ls:], f.buf[start+3:])
			f.buf = f.buf[:len(f.buf)-(3-ls)]
		}
		AppendVarInt(f.buf[start:start], int32(n))
	}
	if f.enc != nil {
		seg := f.buf[start:]
		f.enc.XORKeyStream(seg, seg)
	}
	return nil
}

// Flush writes all buffered frames to the underlying writer.
func (f *FrameWriter) Flush() error {
	if len(f.buf) == 0 {
		return nil
	}
	_, err := f.w.Write(f.buf)
	f.buf = f.buf[:0]
	return err
}
