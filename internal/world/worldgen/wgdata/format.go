package wgdata

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
)

// Container layout:
//
//	"WGD\x01"
//	uvarint n, n bytes: deflated index
//	segments: concatenated raw-deflate streams
//
// Index (inflated): uvarint segment count, then per segment uvarint
// compressed and raw length; uvarint entry count, then per entry (sorted by
// path) uvarint shared-prefix length with the previous path, uvarint suffix
// length, suffix, uvarint segment, uvarint offset, uvarint length.
const magic = "WGD\x01"

// SegmentSize is the raw size at which the encoder starts a new segment.
// Deflate's 32 KiB window makes larger segments gain almost nothing.
const SegmentSize = 256 << 10

type entry struct {
	seg, off, n int
}

type segment struct {
	off, clen, rlen int
}

// Encode packs files (path → contents) into the container format. The output
// depends only on the map contents.
func Encode(files map[string][]byte) ([]byte, error) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var segs [][]byte
	var cur bytes.Buffer
	ents := make([]entry, len(paths))
	for i, p := range paths {
		if cur.Len() >= SegmentSize {
			segs = append(segs, bytes.Clone(cur.Bytes()))
			cur.Reset()
		}
		ents[i] = entry{seg: len(segs), off: cur.Len(), n: len(files[p])}
		cur.Write(files[p])
	}
	if cur.Len() > 0 || len(segs) == 0 {
		segs = append(segs, bytes.Clone(cur.Bytes()))
	}

	comp := make([][]byte, len(segs))
	for i, s := range segs {
		c, err := deflate(s)
		if err != nil {
			return nil, err
		}
		comp[i] = c
	}

	var idx []byte
	idx = binary.AppendUvarint(idx, uint64(len(segs)))
	for i := range segs {
		idx = binary.AppendUvarint(idx, uint64(len(comp[i])))
		idx = binary.AppendUvarint(idx, uint64(len(segs[i])))
	}
	idx = binary.AppendUvarint(idx, uint64(len(paths)))
	prev := ""
	for i, p := range paths {
		k := commonPrefix(prev, p)
		idx = binary.AppendUvarint(idx, uint64(k))
		idx = binary.AppendUvarint(idx, uint64(len(p)-k))
		idx = append(idx, p[k:]...)
		idx = binary.AppendUvarint(idx, uint64(ents[i].seg))
		idx = binary.AppendUvarint(idx, uint64(ents[i].off))
		idx = binary.AppendUvarint(idx, uint64(ents[i].n))
		prev = p
	}
	cidx, err := deflate(idx)
	if err != nil {
		return nil, err
	}

	out := []byte(magic)
	out = binary.AppendUvarint(out, uint64(len(cidx)))
	out = append(out, cidx...)
	for _, c := range comp {
		out = append(out, c...)
	}
	return out, nil
}

func deflate(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func inflate(b []byte, n int) ([]byte, error) {
	out := make([]byte, n)
	r := flate.NewReader(bytes.NewReader(b))
	defer r.Close()
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

func commonPrefix(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

var errCorrupt = errors.New("wgdata: corrupt container")

type reader struct {
	b []byte
	i int
}

func (r *reader) uv() int {
	if r.i < 0 {
		return 0
	}
	v, n := binary.Uvarint(r.b[r.i:])
	if n <= 0 || v > 1<<40 {
		r.i = -1
		return 0
	}
	r.i += n
	return int(v)
}

func (r *reader) bytes(n int) []byte {
	if r.i < 0 || n > len(r.b)-r.i {
		r.i = -1
		return nil
	}
	s := r.b[r.i : r.i+n]
	r.i += n
	return s
}

// decodeIndex parses the container header and returns the sorted paths,
// their entries and the segment table.
func decodeIndex(blob []byte) ([]string, []entry, []segment, error) {
	if len(blob) < len(magic) || string(blob[:len(magic)]) != magic {
		return nil, nil, nil, errCorrupt
	}
	hr := &reader{b: blob, i: len(magic)}
	ci := hr.bytes(hr.uv())
	if hr.i < 0 {
		return nil, nil, nil, errCorrupt
	}
	base := hr.i

	var raw bytes.Buffer
	fr := flate.NewReader(bytes.NewReader(ci))
	if _, err := raw.ReadFrom(fr); err != nil {
		return nil, nil, nil, fmt.Errorf("wgdata: index: %w", err)
	}
	r := &reader{b: raw.Bytes()}

	nseg := r.uv()
	if nseg > len(blob) {
		return nil, nil, nil, errCorrupt
	}
	segs := make([]segment, nseg)
	off := base
	for i := range segs {
		segs[i] = segment{off: off, clen: r.uv(), rlen: r.uv()}
		off += segs[i].clen
	}
	if r.i < 0 || off > len(blob) {
		return nil, nil, nil, errCorrupt
	}

	nent := r.uv()
	if nent > len(r.b) {
		return nil, nil, nil, errCorrupt
	}
	paths := make([]string, nent)
	ents := make([]entry, nent)
	prev := ""
	for i := range paths {
		k := r.uv()
		suf := r.bytes(r.uv())
		if r.i < 0 || k > len(prev) {
			return nil, nil, nil, errCorrupt
		}
		p := prev[:k] + string(suf)
		e := entry{seg: r.uv(), off: r.uv(), n: r.uv()}
		if r.i < 0 || e.seg >= nseg || e.off+e.n > segs[e.seg].rlen {
			return nil, nil, nil, errCorrupt
		}
		paths[i], ents[i], prev = p, e, p
	}
	return paths, ents, segs, nil
}
