package metrics

import (
	"encoding/binary"
	"testing"
)

func TestDecodeLoadavg(t *testing.T) {
	b := make([]byte, 24)
	for i, v := range []uint32{1024, 2048, 512} {
		binary.LittleEndian.PutUint32(b[i*4:], v)
	}
	binary.LittleEndian.PutUint64(b[16:], 2048)
	l, ok := decodeLoadavg(b)
	if !ok || l != [3]float64{0.5, 1, 0.25} {
		t.Fatalf("%v %v", l, ok)
	}
	if _, ok := decodeLoadavg(b[:20]); ok {
		t.Fatal("short buffer accepted")
	}
}
