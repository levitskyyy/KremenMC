package item

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/proto"
)

func benchStack() Stack {
	id, _ := IDOf("diamond_sword")
	return Stack{Item: id, Count: 1}.WithDamage(10)
}

func BenchmarkAppendStack(b *testing.B) {
	s := benchStack()
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	for b.Loop() {
		buf = AppendStack(buf[:0], s)
	}
}

func BenchmarkReadStack(b *testing.B) {
	raw := AppendStack(nil, benchStack())
	b.ReportAllocs()
	for b.Loop() {
		r := proto.Reader{B: raw}
		if _, err := ReadStack(&r); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAppendStackNoAlloc(t *testing.T) {
	s := benchStack()
	buf := make([]byte, 0, 64)
	if n := testing.AllocsPerRun(100, func() { buf = AppendStack(buf[:0], s) }); n != 0 {
		t.Fatalf("AppendStack allocates %v times", n)
	}
}
