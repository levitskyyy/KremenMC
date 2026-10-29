package entity_test

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/world"
)

var (
	_ entity.BlockSource = (*world.World)(nil)
	_ entity.BlockSource = (*world.TickContext)(nil)
)

func TestNew(t *testing.T) {
	tests := []struct {
		name          string
		gravity, drag float64
		step          float64
	}{
		{"player", 0.08, 0.98, 0.6},
		{"zombie", 0.08, 0.98, 0.6},
		{"pig", 0.08, 0.98, 0.6},
		{"item", 0.04, 0.98, 0},
		{"arrow", 0.05, 0.99, 0},
		{"minecart", 0.04, 0.95, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ, ok := data.Canonical().EntityByName(tt.name)
			if !ok {
				t.Fatalf("no entity type %s", tt.name)
			}
			e := entity.New(typ)
			if e.Type != typ.ID || e.Width != float64(typ.Width) || e.Height != float64(typ.Height) {
				t.Errorf("type/size = %v %v %v, want %v %v %v", e.Type, e.Width, e.Height, typ.ID, typ.Width, typ.Height)
			}
			if e.Gravity != tt.gravity || e.Drag != tt.drag || e.StepHeight != tt.step {
				t.Errorf("gravity/drag/step = %v %v %v, want %v %v %v", e.Gravity, e.Drag, e.StepHeight, tt.gravity, tt.drag, tt.step)
			}
			if e.UUID[6]>>4 != 4 || e.UUID[8]>>6 != 2 {
				t.Errorf("UUID %x is not version 4 / RFC 4122", e.UUID)
			}
		})
	}
}

func TestNewNilType(t *testing.T) {
	if e := entity.New(nil); e.Type != -1 || e.ID == 0 {
		t.Fatalf("New(nil) = %+v", e)
	}
}

func TestIDsAndUUIDsUnique(t *testing.T) {
	typ, _ := data.Canonical().EntityByName("item")
	ids := map[int32]bool{}
	uuids := map[[16]byte]bool{}
	for range 1000 {
		e := entity.New(typ)
		if e.ID <= 0 || ids[e.ID] || uuids[e.UUID] {
			t.Fatalf("duplicate or invalid id %d / uuid %x", e.ID, e.UUID)
		}
		ids[e.ID], uuids[e.UUID] = true, true
	}
}

func TestAngleByte(t *testing.T) {
	tests := []struct {
		deg  float32
		want int8
	}{
		{0, 0},
		{90, 64},
		{180, -128},
		{270, -64},
		{360, 0},
		{-90, -64},
		{-1, -1},
		{45.5, 32},
		{720 + 90, 64},
		{-3600 - 180, -128},
	}
	for _, tt := range tests {
		if got := entity.AngleByte(tt.deg); got != tt.want {
			t.Errorf("AngleByte(%v) = %v, want %v", tt.deg, got, tt.want)
		}
	}
}

func TestWrapDegrees(t *testing.T) {
	tests := []struct{ in, want float32 }{
		{0, 0},
		{179.5, 179.5},
		{180, -180},
		{-180, -180},
		{270, -90},
		{-270, 90},
		{360, 0},
		{725, 5},
		{-725, -5},
	}
	for _, tt := range tests {
		if got := entity.WrapDegrees(tt.in); got != tt.want {
			t.Errorf("WrapDegrees(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
