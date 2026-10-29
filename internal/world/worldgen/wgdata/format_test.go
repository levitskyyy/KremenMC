package wgdata

import (
	"bytes"
	"fmt"
	"testing"
)

func TestEncodeRoundTrip(t *testing.T) {
	big := bytes.Repeat([]byte("0123456789abcdef"), SegmentSize/8)
	tests := []struct {
		name  string
		files map[string][]byte
	}{
		{"empty", map[string][]byte{}},
		{"one", map[string][]byte{"a": []byte("x")}},
		{"empty file", map[string][]byte{"a": nil, "b": []byte("y")}},
		{"shared prefixes", map[string][]byte{
			"data/minecraft/worldgen/biome/plains.json":       []byte(`{"a":1}`),
			"data/minecraft/worldgen/biome/plains_extra.json": []byte(`{"b":2}`),
			"data/minecraft/worldgen/biome.json":              []byte(`{}`),
			"reports/registries.json":                         []byte(`{"c":3}`),
		}},
		{"multi segment", map[string][]byte{"a": big, "b": big[:10], "c": big, "d": []byte("tail")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := Encode(tt.files)
			if err != nil {
				t.Fatal(err)
			}
			b2, _ := Encode(tt.files)
			if !bytes.Equal(b, b2) {
				t.Fatal("encode is not deterministic")
			}
			p, err := Open(b)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Paths()) != len(tt.files) {
				t.Fatalf("got %d paths, want %d", len(p.Paths()), len(tt.files))
			}
			for path, want := range tt.files {
				got, ok := p.File(path)
				if !ok || !bytes.Equal(got, want) {
					t.Errorf("File(%q) = %d bytes,%v want %d bytes", path, len(got), ok, len(want))
				}
			}
			if _, ok := p.File("missing"); ok {
				t.Error("File(missing) ok")
			}
		})
	}
}

func TestOpenCorrupt(t *testing.T) {
	files := map[string][]byte{}
	for i := range 50 {
		files[fmt.Sprintf("data/minecraft/loot_table/x%d.json", i)] = bytes.Repeat([]byte{byte(i)}, 1000)
	}
	good, err := Encode(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 3, 4, 5, 10, 40} {
		if _, err := Open(good[:n]); err == nil {
			t.Errorf("Open(truncated %d) succeeded", n)
		}
	}
	p, err := Open(good[:len(good)-20])
	if err != nil {
		return
	}
	for _, path := range p.Paths() {
		p.File(path)
	}
}
