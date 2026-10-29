package rng

import (
	"crypto/md5"
	"encoding/binary"
	"math/bits"
	"unicode/utf16"

	"github.com/levitskyyy/KremenMC/internal/world/worldgen/mth"
)

const (
	GoldenRatio64 int64 = -7046029254386353131
	SilverRatio64 int64 = 7640891576956012809
)

// Seed128 is a 128-bit Xoroshiro seed (reference RandomSupport.Seed128bit).
type Seed128 struct{ Lo, Hi int64 }

func (s Seed128) Xor(lo, hi int64) Seed128 { return Seed128{s.Lo ^ lo, s.Hi ^ hi} }

func (s Seed128) Mixed() Seed128 { return Seed128{MixStafford13(s.Lo), MixStafford13(s.Hi)} }

// MixStafford13 is David Stafford's "Mix13" 64-bit finalizer.
func MixStafford13(z int64) int64 {
	u := uint64(z)
	u = (u ^ u>>30) * 0xbf58476d1ce4e5b9
	u = (u ^ u>>27) * 0x94d049bb133111eb
	return int64(u ^ u>>31)
}

func UpgradeSeedTo128bitUnmixed(seed int64) Seed128 {
	lo := seed ^ SilverRatio64
	return Seed128{lo, lo + GoldenRatio64}
}

func UpgradeSeedTo128bit(seed int64) Seed128 { return UpgradeSeedTo128bitUnmixed(seed).Mixed() }

// SeedFromHashOf is the MD5 of the UTF-8 name read as two big-endian longs.
func SeedFromHashOf(name string) Seed128 {
	h := md5.Sum([]byte(name))
	return Seed128{int64(binary.BigEndian.Uint64(h[:8])), int64(binary.BigEndian.Uint64(h[8:]))}
}

// JavaStringHash is java.lang.String.hashCode over UTF-16 code units.
func JavaStringHash(s string) int32 {
	var h int32
	for _, r := range s {
		if r >= 0x10000 {
			a, b := utf16.EncodeRune(r)
			h = 31*h + a
			h = 31*h + b
			continue
		}
		h = 31*h + r
	}
	return h
}

// GetSeed is the reference positional hash (Mth.getSeed).
func GetSeed(x, y, z int32) int64 { return mth.GetSeed(x, y, z) }

func rotl(x uint64, k int) uint64 { return bits.RotateLeft64(x, k) }
