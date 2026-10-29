package proto

import (
	"bytes"
	"crypto/aes"
	"errors"
	"io"
	"math/rand/v2"
	"testing"
	"testing/iotest"
)

// refCFB8 is a direct, shift-register implementation used to check CFB8.
func refCFB8(key, in []byte, decrypt bool) []byte {
	b, _ := aes.NewCipher(key)
	iv := append([]byte(nil), key...)
	out := make([]byte, len(in))
	var tmp [16]byte
	for i, c := range in {
		b.Encrypt(tmp[:], iv)
		out[i] = c ^ tmp[0]
		fb := out[i]
		if decrypt {
			fb = c
		}
		copy(iv, iv[1:])
		iv[15] = fb
	}
	return out
}

func testKey() []byte { return []byte("0123456789abcdef") }

func TestCFB8MatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	plain := make([]byte, 3*cfb8Window+123)
	for i := range plain {
		plain[i] = byte(rng.Uint32())
	}
	want := refCFB8(testKey(), plain, false)
	enc, _ := NewCFB8Encrypter(testKey())
	got := make([]byte, len(plain))
	for off := 0; off < len(plain); {
		n := min(1+rng.IntN(700), len(plain)-off)
		enc.XORKeyStream(got[off:off+n], plain[off:off+n])
		off += n
	}
	if !bytes.Equal(got, want) {
		t.Fatal("encrypt mismatch with reference")
	}
	dec, _ := NewCFB8Decrypter(testKey())
	back := append([]byte(nil), got...)
	dec.XORKeyStream(back, back)
	if !bytes.Equal(back, plain) {
		t.Fatal("decrypt mismatch")
	}
	if !bytes.Equal(refCFB8(testKey(), want, true), plain) {
		t.Fatal("reference decrypt mismatch")
	}
	if _, err := NewCFB8Encrypter([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}

func packet(n int, seed uint64) []byte {
	rng := rand.New(rand.NewPCG(seed, 7))
	p := make([]byte, n)
	for i := range p {
		if i%3 == 0 {
			p[i] = byte(rng.Uint32())
		}
	}
	if n > 0 {
		p[0] = byte(seed)
	}
	return p
}

func TestFrameRoundTrip(t *testing.T) {
	sizes := []int{1, 2, 63, 64, 255, 256, 257, 4000, 70000, 300000}
	tests := []struct {
		name      string
		threshold int
		encrypt   bool
	}{
		{"plain", -1, false},
		{"compressed", 256, false},
		{"compress all", 0, false},
		{"encrypted", -1, true},
		{"compressed encrypted", 256, true},
	}
	for _, tt := range tests {
		var wire bytes.Buffer
		fw := NewFrameWriter(&wire)
		if tt.threshold >= 0 {
			fw.SetCompression(tt.threshold, 6)
		}
		if tt.encrypt {
			if err := fw.EnableEncryption(testKey()); err != nil {
				t.Fatal(err)
			}
		}
		var want [][]byte
		for i, n := range sizes {
			p := packet(n, uint64(i))
			want = append(want, p)
			if err := fw.WriteFrame(p); err != nil {
				t.Fatalf("%s: WriteFrame(%d): %v", tt.name, n, err)
			}
			if i%3 == 0 {
				if err := fw.Flush(); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := fw.Flush(); err != nil {
			t.Fatal(err)
		}
		readers := map[string]io.Reader{
			"whole":    bytes.NewReader(wire.Bytes()),
			"one byte": iotest.OneByteReader(bytes.NewReader(wire.Bytes())),
			"half":     iotest.HalfReader(bytes.NewReader(wire.Bytes())),
			"data err": iotest.DataErrReader(bytes.NewReader(wire.Bytes())),
		}
		for rname, src := range readers {
			fr := NewFrameReader(src)
			if tt.threshold >= 0 {
				fr.SetCompression(tt.threshold)
			}
			if tt.encrypt {
				if err := fr.EnableEncryption(testKey()); err != nil {
					t.Fatal(err)
				}
			}
			for i, w := range want {
				got, err := fr.ReadFrame()
				if err != nil {
					t.Fatalf("%s/%s: frame %d: %v", tt.name, rname, i, err)
				}
				if !bytes.Equal(got, w) {
					t.Fatalf("%s/%s: frame %d mismatch (len %d vs %d)", tt.name, rname, i, len(got), len(w))
				}
			}
			if _, err := fr.ReadFrame(); err != io.EOF {
				t.Fatalf("%s/%s: trailing read err = %v, want EOF", tt.name, rname, err)
			}
		}
	}
}

func TestFrameUncompressedFormat(t *testing.T) {
	var wire bytes.Buffer
	fw := NewFrameWriter(&wire)
	fw.SetCompression(64, 6)
	fw.WriteFrame([]byte{0x01, 0x02})
	big := packet(100, 3)
	fw.WriteFrame(big)
	fw.Flush()
	b := wire.Bytes()
	if !bytes.Equal(b[:4], []byte{0x03, 0x00, 0x01, 0x02}) {
		t.Fatalf("small frame = % X", b[:4])
	}
	r := NewReader(b[4:])
	l, _ := r.VarInt()
	dl, _ := r.VarInt()
	if int(dl) != len(big) || int(l) != r.Remaining()+VarIntSize(dl) {
		t.Fatalf("compressed header: len %d dataLen %d rem %d", l, dl, r.Remaining())
	}
}

// Enabling decryption must also decrypt bytes already pulled into the buffer.
func TestFrameEncryptionMidStream(t *testing.T) {
	var wire bytes.Buffer
	fw := NewFrameWriter(&wire)
	fw.WriteFrame([]byte("plain"))
	fw.Flush()
	fw.EnableEncryption(testKey())
	fw.WriteFrame([]byte("secret one"))
	fw.WriteFrame([]byte("secret two"))
	fw.Flush()

	fr := NewFrameReader(bytes.NewReader(wire.Bytes()))
	got, err := fr.ReadFrame()
	if err != nil || string(got) != "plain" {
		t.Fatalf("first = %q, %v", got, err)
	}
	if fr.Buffered() == 0 {
		t.Fatal("expected ciphertext to be buffered already")
	}
	if err := fr.EnableEncryption(testKey()); err != nil {
		t.Fatal(err)
	}
	if err := fr.EnableEncryption(testKey()); !errors.Is(err, ErrEncryptionActive) {
		t.Fatalf("second enable err = %v", err)
	}
	for _, want := range []string{"secret one", "secret two"} {
		got, err := fr.ReadFrame()
		if err != nil || string(got) != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
}

func TestFrameErrors(t *testing.T) {
	compressed := func(dataLen int32, body []byte) []byte {
		inner := AppendVarInt(nil, dataLen)
		inner = append(inner, body...)
		return append(AppendVarInt(nil, int32(len(inner))), inner...)
	}
	zdata := func(p []byte) []byte {
		var b bytes.Buffer
		fw := NewFrameWriter(&b)
		fw.SetCompression(0, 6)
		fw.WriteFrame(p)
		fw.Flush()
		r := NewReader(b.Bytes())
		r.VarInt()
		r.VarInt()
		return r.Rest()
	}
	tests := []struct {
		name      string
		in        []byte
		threshold int
		want      error
	}{
		{"length too long", []byte{0x80, 0x80, 0x80, 0x01}, -1, ErrBadFrameLength},
		{"frame too big", AppendVarInt(nil, MaxFrameSize+1), -1, ErrBadFrameLength},
		{"truncated body", []byte{0x05, 0x01, 0x02}, -1, io.ErrUnexpectedEOF},
		{"truncated length", []byte{0x80}, -1, io.ErrUnexpectedEOF},
		{"below threshold", compressed(10, zdata(make([]byte, 10))), 256, ErrBelowThreshold},
		{"too large", compressed(MaxUncompressedSize+1, zdata([]byte{1})), 0, ErrPacketTooBig},
		{"wrong size", compressed(300, zdata(make([]byte, 299))), 256, ErrBadCompression},
		{"extra data", compressed(300, zdata(make([]byte, 301))), 256, ErrBadCompression},
		{"garbage", compressed(300, []byte{1, 2, 3, 4}), 256, ErrBadCompression},
	}
	for _, tt := range tests {
		fr := NewFrameReader(bytes.NewReader(tt.in))
		fr.SetCompression(tt.threshold)
		if _, err := fr.ReadFrame(); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	fw := NewFrameWriter(io.Discard)
	if err := fw.WriteFrame(make([]byte, MaxFrameSize+1)); !errors.Is(err, ErrFrameTooBig) {
		t.Errorf("uncompressed oversize err = %v", err)
	}
	if fw.Buffered() != 0 {
		t.Errorf("failed write left %d bytes buffered", fw.Buffered())
	}
}

func BenchmarkFrameWriteCompressed(b *testing.B) {
	p := packet(16<<10, 9)
	fw := NewFrameWriter(io.Discard)
	fw.SetCompression(256, 6)
	b.SetBytes(int64(len(p)))
	b.ReportAllocs()
	for b.Loop() {
		if err := fw.WriteFrame(p); err != nil {
			b.Fatal(err)
		}
		fw.Flush()
	}
}

func BenchmarkFrameWriteCompressedFast(b *testing.B) {
	p := packet(16<<10, 9)
	fw := NewFrameWriter(io.Discard)
	fw.SetCompression(256, 1)
	b.SetBytes(int64(len(p)))
	b.ReportAllocs()
	for b.Loop() {
		fw.WriteFrame(p)
		fw.Flush()
	}
}

// A chunk-sized payload (~ one Chunk Data and Update Light packet) with compression and encryption.
func BenchmarkFrameWriteChunk(b *testing.B) {
	p := packet(48<<10, 11)
	fw := NewFrameWriter(io.Discard)
	fw.SetCompression(256, 6)
	fw.EnableEncryption(testKey())
	b.SetBytes(int64(len(p)))
	b.ReportAllocs()
	for b.Loop() {
		fw.WriteFrame(p)
		fw.Flush()
	}
}

func BenchmarkFrameWriteSmall(b *testing.B) {
	p := packet(24, 5)
	fw := NewFrameWriter(io.Discard)
	fw.SetCompression(256, 6)
	b.ReportAllocs()
	for b.Loop() {
		fw.WriteFrame(p)
		fw.Flush()
	}
}

type loopReader struct {
	data []byte
	off  int
}

func (l *loopReader) Read(p []byte) (int, error) {
	if l.off == len(l.data) {
		l.off = 0
	}
	n := copy(p, l.data[l.off:])
	l.off += n
	return n, nil
}

func BenchmarkFrameReadCompressed(b *testing.B) {
	var wire bytes.Buffer
	fw := NewFrameWriter(&wire)
	fw.SetCompression(256, 6)
	p := packet(48<<10, 11)
	fw.WriteFrame(p)
	fw.Flush()
	fr := NewFrameReader(&loopReader{data: wire.Bytes()})
	fr.SetCompression(256)
	b.SetBytes(int64(len(p)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fr.ReadFrame(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCFB8(b *testing.B) {
	enc, _ := NewCFB8Encrypter(testKey())
	buf := make([]byte, 16<<10)
	b.SetBytes(int64(len(buf)))
	for b.Loop() {
		enc.XORKeyStream(buf, buf)
	}
}
