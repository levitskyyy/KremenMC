// Package anvil reads and writes vanilla region files (.mca) and converts
// chunk NBT to and from world.Chunk.
package anvil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	CompressionGzip   byte = 1
	CompressionZlib   byte = 2
	CompressionNone   byte = 3
	CompressionLZ4    byte = 4
	CompressionCustom byte = 127
	externalFlag      byte = 0x80

	sectorSize     = 4096
	maxSectorCount = 255
	maxChunkBytes  = 64 << 20
)

var (
	ErrCorrupt  = errors.New("anvil: corrupt region data")
	ErrReadOnly = errors.New("anvil: region opened read-only")
)

// Region is one r.X.Z.mca file. Methods are safe for concurrent use.
type Region struct {
	mu    sync.Mutex
	f     *os.File
	dir   string
	X, Z  int32
	locs  [1024]uint32
	times [1024]uint32
	used  []bool

	readOnly bool
}

func RegionPath(dir string, rx, rz int32) string {
	return filepath.Join(dir, fmt.Sprintf("r.%d.%d.mca", rx, rz))
}

// OpenRegion opens or creates the region file for region coordinates rx,rz in dir.
func OpenRegion(dir string, rx, rz int32, create bool) (*Region, error) {
	path := RegionPath(dir, rx, rz)
	flag := os.O_RDWR
	if create {
		flag |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return nil, err
	}
	r := &Region{f: f, dir: dir, X: rx, Z: rz}
	if err := r.init(); err != nil {
		f.Close()
		return nil, fmt.Errorf("anvil: %s: %w", path, err)
	}
	return r, nil
}

// OpenRegionReadOnly opens an existing region file without ever writing to
// it, e.g. one owned by a running vanilla server. Write and Delete fail.
func OpenRegionReadOnly(dir string, rx, rz int32) (*Region, error) {
	path := RegionPath(dir, rx, rz)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &Region{f: f, dir: dir, X: rx, Z: rz, readOnly: true}
	if err := r.init(); err != nil {
		f.Close()
		return nil, fmt.Errorf("anvil: %s: %w", path, err)
	}
	return r, nil
}

func (r *Region) init() error {
	st, err := r.f.Stat()
	if err != nil {
		return err
	}
	size := st.Size()
	if r.readOnly {
		if size < 2*sectorSize {
			return nil // empty file: no chunks
		}
		size += (sectorSize - size%sectorSize) % sectorSize // the last sector may be unpadded
	}
	if size < 2*sectorSize {
		hdr := make([]byte, 2*sectorSize)
		if _, err := r.f.WriteAt(hdr, 0); err != nil {
			return err
		}
		size = 2 * sectorSize
	}
	if size%sectorSize != 0 {
		pad := sectorSize - size%sectorSize
		if _, err := r.f.WriteAt(make([]byte, pad), size); err != nil {
			return err
		}
		size += pad
	}
	hdr := make([]byte, 2*sectorSize)
	if _, err := r.f.ReadAt(hdr, 0); err != nil {
		return err
	}
	r.used = make([]bool, size/sectorSize)
	r.used[0], r.used[1] = true, true
	for i := range 1024 {
		r.locs[i] = binary.BigEndian.Uint32(hdr[i*4:])
		r.times[i] = binary.BigEndian.Uint32(hdr[sectorSize+i*4:])
		off, n := int(r.locs[i]>>8), int(r.locs[i]&0xFF)
		if r.locs[i] == 0 {
			continue
		}
		if off < 2 || n == 0 || off+n > len(r.used) {
			r.locs[i] = 0 // drop out-of-file entries, as vanilla does
			continue
		}
		for s := off; s < off+n; s++ {
			r.used[s] = true
		}
	}
	return nil
}

func (r *Region) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

func localIndex(lx, lz int) int { return (lx & 31) | (lz&31)<<5 }

func (r *Region) externalPath(lx, lz int) string {
	return filepath.Join(r.dir, fmt.Sprintf("c.%d.%d.mcc", r.X<<5+int32(lx&31), r.Z<<5+int32(lz&31)))
}

// Has reports whether the chunk at local coordinates is present.
func (r *Region) Has(lx, lz int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.locs[localIndex(lx, lz)] != 0
}

// Timestamp returns the last-write epoch seconds of a chunk.
func (r *Region) Timestamp(lx, lz int) uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.times[localIndex(lx, lz)]
}

// Read returns the decompressed chunk NBT, or nil if absent.
func (r *Region) Read(lx, lz int) ([]byte, error) {
	r.mu.Lock()
	loc := r.locs[localIndex(lx, lz)]
	if loc == 0 || r.f == nil {
		r.mu.Unlock()
		return nil, nil
	}
	off, n := int64(loc>>8)*sectorSize, int(loc&0xFF)*sectorSize
	buf := make([]byte, n)
	_, err := r.f.ReadAt(buf, off)
	r.mu.Unlock()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(buf) < 5 {
		return nil, ErrCorrupt
	}
	length := int(binary.BigEndian.Uint32(buf))
	typ := buf[4]
	if typ&externalFlag != 0 {
		ext, err := os.ReadFile(r.externalPath(lx, lz))
		if err != nil {
			return nil, fmt.Errorf("anvil: external chunk: %w", err)
		}
		return decompress(typ&^externalFlag, ext)
	}
	if length < 1 || 4+length > len(buf) {
		return nil, ErrCorrupt
	}
	return decompress(typ, buf[5:4+length])
}

func decompress(typ byte, p []byte) ([]byte, error) {
	var rd io.Reader
	switch typ {
	case CompressionNone:
		return append([]byte(nil), p...), nil
	case CompressionGzip:
		z, err := gzip.NewReader(bytes.NewReader(p))
		if err != nil {
			return nil, fmt.Errorf("anvil: gzip: %w", err)
		}
		rd = z
	case CompressionZlib:
		z, err := zlib.NewReader(bytes.NewReader(p))
		if err != nil {
			return nil, fmt.Errorf("anvil: zlib: %w", err)
		}
		rd = z
	case CompressionLZ4:
		return lz4Decompress(p)
	default:
		return nil, fmt.Errorf("anvil: unsupported compression %d", typ)
	}
	out, err := io.ReadAll(io.LimitReader(rd, maxChunkBytes+1))
	if err != nil {
		return nil, fmt.Errorf("anvil: decompress: %w", err)
	}
	if len(out) > maxChunkBytes {
		return nil, fmt.Errorf("anvil: chunk exceeds %d bytes", maxChunkBytes)
	}
	return out, nil
}

func compress(typ byte, p []byte) ([]byte, error) {
	var b bytes.Buffer
	switch typ {
	case CompressionNone:
		return p, nil
	case CompressionLZ4:
		return lz4Compress(nil, p), nil
	case CompressionGzip:
		w := gzip.NewWriter(&b)
		if _, err := w.Write(p); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	case CompressionZlib:
		w := zlib.NewWriter(&b)
		if _, err := w.Write(p); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("anvil: unsupported compression %d", typ)
	}
	return b.Bytes(), nil
}

// Write compresses and stores chunk NBT. Data that does not fit in 255
// sectors goes to an external c.X.Z.mcc file like vanilla.
func (r *Region) Write(lx, lz int, nbtData []byte, typ byte) error {
	if r.readOnly {
		return ErrReadOnly
	}
	payload, err := compress(typ, nbtData)
	if err != nil {
		return err
	}
	ext := r.externalPath(lx, lz)
	var rec []byte
	if 5+len(payload) > maxSectorCount*sectorSize {
		if err := writeFileAtomic(ext, payload); err != nil {
			return fmt.Errorf("anvil: external chunk: %w", err)
		}
		rec = []byte{0, 0, 0, 1, typ | externalFlag}
	} else {
		rec = make([]byte, 5, 5+len(payload))
		binary.BigEndian.PutUint32(rec, uint32(len(payload)+1))
		rec[4] = typ
		rec = append(rec, payload...)
		_ = os.Remove(ext)
	}
	n := (len(rec) + sectorSize - 1) / sectorSize
	if pad := n*sectorSize - len(rec); pad > 0 {
		rec = append(rec, make([]byte, pad)...)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return os.ErrClosed
	}
	i := localIndex(lx, lz)
	old := r.locs[i]
	// free old sectors first so the new data may reuse them only if they
	// don't overlap the copy being replaced
	start := r.alloc(n, old)
	if _, err := r.f.WriteAt(rec, int64(start)*sectorSize); err != nil {
		return err
	}
	loc := uint32(start)<<8 | uint32(n)
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], loc)
	if _, err := r.f.WriteAt(hdr[:], int64(i*4)); err != nil {
		return err
	}
	ts := uint32(time.Now().Unix())
	binary.BigEndian.PutUint32(hdr[:], ts)
	if _, err := r.f.WriteAt(hdr[:], int64(sectorSize+i*4)); err != nil {
		return err
	}
	if old != 0 {
		for s := int(old >> 8); s < int(old>>8)+int(old&0xFF); s++ {
			r.used[s] = false
		}
	}
	for s := start; s < start+n; s++ {
		r.used[s] = true
	}
	r.locs[i], r.times[i] = loc, ts
	return nil
}

// alloc finds n free sectors, never overlapping the live copy in old.
func (r *Region) alloc(n int, old uint32) int {
	oStart, oEnd := int(old>>8), int(old>>8)+int(old&0xFF)
	run := 0
	for s := 2; s < len(r.used); s++ {
		if r.used[s] || (s >= oStart && s < oEnd) {
			run = 0
			continue
		}
		run++
		if run == n {
			return s - n + 1
		}
	}
	start := len(r.used) - run
	for len(r.used) < start+n {
		r.used = append(r.used, false)
	}
	return start
}

// Delete removes a chunk entry.
func (r *Region) Delete(lx, lz int) error {
	if r.readOnly {
		return ErrReadOnly
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	i := localIndex(lx, lz)
	old := r.locs[i]
	if old == 0 || r.f == nil {
		return nil
	}
	var z [4]byte
	if _, err := r.f.WriteAt(z[:], int64(i*4)); err != nil {
		return err
	}
	for s := int(old >> 8); s < int(old>>8)+int(old&0xFF); s++ {
		r.used[s] = false
	}
	r.locs[i] = 0
	_ = os.Remove(r.externalPath(lx, lz))
	return nil
}

func (r *Region) Sync() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil || r.readOnly {
		return nil
	}
	return r.f.Sync()
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
