package proto

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

const cfb8Window = 4096

// CFB8 is AES/CFB8 as used by the Minecraft protocol (key == IV == shared secret).
// The shift register lives in a sliding window so each byte costs one block
// encryption and no 16-byte shift.
type CFB8 struct {
	b       cipher.Block
	sr      [cfb8Window + 16]byte
	pos     int
	tmp     [16]byte
	decrypt bool
}

var _ cipher.Stream = (*CFB8)(nil)

func newCFB8(key []byte, decrypt bool) (*CFB8, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cfb8: %w", err)
	}
	if len(key) != 16 {
		return nil, fmt.Errorf("cfb8: key must be 16 bytes, got %d", len(key))
	}
	c := &CFB8{b: b, decrypt: decrypt}
	copy(c.sr[:16], key)
	return c, nil
}

func NewCFB8Encrypter(key []byte) (*CFB8, error) { return newCFB8(key, false) }
func NewCFB8Decrypter(key []byte) (*CFB8, error) { return newCFB8(key, true) }

func (c *CFB8) XORKeyStream(dst, src []byte) {
	if len(dst) < len(src) {
		panic("cfb8: output smaller than input")
	}
	sr := c.sr[:]
	pos := c.pos
	for i, in := range src {
		c.b.Encrypt(c.tmp[:], sr[pos:pos+16])
		out := in ^ c.tmp[0]
		if c.decrypt {
			sr[pos+16] = in
		} else {
			sr[pos+16] = out
		}
		dst[i] = out
		pos++
		if pos == cfb8Window {
			copy(sr[:16], sr[pos:pos+16])
			pos = 0
		}
	}
	c.pos = pos
}
