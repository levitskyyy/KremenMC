package item

import (
	"encoding/binary"
	"hash/crc32"
	"math"
	"slices"
	"unicode/utf16"
)

// Component hashes reproduce vanilla's HashOps (CRC32C over a tagged,
// order-independent encoding of the codec form). They are used by the
// click packet to report the client's view of changed slots.

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

const (
	hEmpty     = 1
	hMapStart  = 2
	hMapEnd    = 3
	hListStart = 4
	hListEnd   = 5
	hByte      = 6
	hShort     = 7
	hInt       = 8
	hLong      = 9
	hFloat     = 10
	hDouble    = 11
	hString    = 12
	hBool      = 13
	hBytesS    = 14
	hBytesE    = 15
	hIntsS     = 16
	hIntsE     = 17
	hLongsS    = 18
	hLongsE    = 19
)

func crc(b []byte) uint32 { return crc32.Checksum(b, castagnoli) }

func le32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }
func le64(b []byte, v uint64) []byte { return binary.LittleEndian.AppendUint64(b, v) }

func hashTagged(tag byte, payload []byte) uint32 {
	var buf [16]byte
	b := append(buf[:0], tag)
	return crc(append(b, payload...))
}

func HashEmpty() uint32      { return crc([]byte{hEmpty}) }
func HashBool(v bool) uint32 { return crc([]byte{hBool, b2u(v)}) }
func HashByte(v int8) uint32 { return crc([]byte{hByte, byte(v)}) }
func HashShort(v int16) uint32 {
	return crc(binary.LittleEndian.AppendUint16([]byte{hShort}, uint16(v)))
}
func HashInt(v int32) uint32     { return crc(le32([]byte{hInt}, uint32(v))) }
func HashLong(v int64) uint32    { return crc(le64([]byte{hLong}, uint64(v))) }
func HashFloat(v float32) uint32 { return crc(le32([]byte{hFloat}, math.Float32bits(v))) }
func HashDouble(v float64) uint32 {
	return crc(le64([]byte{hDouble}, math.Float64bits(v)))
}

func HashString(s string) uint32 {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, 5+2*len(u))
	b = append(b, hString)
	b = le32(b, uint32(len(u)))
	for _, c := range u {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return crc(b)
}

func b2u(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func hashList(elems []uint32) uint32 {
	b := make([]byte, 0, 2+4*len(elems))
	b = append(b, hListStart)
	for _, e := range elems {
		b = le32(b, e)
	}
	return crc(append(b, hListEnd))
}

type kv struct{ k, v uint32 }

func hashMap(es []kv) uint32 {
	slices.SortFunc(es, func(a, b kv) int {
		if a.k != b.k {
			if a.k < b.k {
				return -1
			}
			return 1
		}
		if a.v < b.v {
			return -1
		}
		if a.v > b.v {
			return 1
		}
		return 0
	})
	b := make([]byte, 0, 2+8*len(es))
	b = append(b, hMapStart)
	for _, e := range es {
		b = le32(le32(b, e.k), e.v)
	}
	return crc(append(b, hMapEnd))
}

func hashBytes(v []byte) uint32 {
	b := append([]byte{hBytesS}, v...)
	return crc(append(b, hBytesE))
}

func hashInts(v []int32) uint32 {
	b := []byte{hIntsS}
	for _, x := range v {
		b = le32(b, uint32(x))
	}
	return crc(append(b, hIntsE))
}

func hashLongs(v []int64) uint32 {
	b := []byte{hLongsS}
	for _, x := range v {
		b = le64(b, uint64(x))
	}
	return crc(append(b, hLongsE))
}

// mapB accumulates a codec map; nil-able fields are simply not added.
type mapB struct{ es []kv }

func (m *mapB) put(k string, v uint32) *mapB {
	m.es = append(m.es, kv{HashString(k), v})
	return m
}

func (m *mapB) sum() uint32 { return hashMap(m.es) }

var hashEmptyMap = hashMap(nil)
