// Package id generates short, opaque, URL-safe identifiers for stored rows.
//
// IDs are 8 characters from a 31-symbol lowercase alphabet with the ambiguous
// glyphs removed (no 0/o, 1/l/i), so they're easy to read aloud and type — and
// case never matters, since there's only one case. That's ~31^8 ≈ 853 billion
// possibilities; the store retries on the astronomically rare collision against
// a column's UNIQUE constraint.
package id

import "crypto/rand"

const (
	alphabet = "23456789abcdefghjkmnpqrstuvwxyz" // 31 symbols
	length   = 8
)

// New returns a fresh 8-character id. It samples uniformly (mask + reject) so
// no symbol is favoured by modulo bias.
func New() string {
	out := make([]byte, length)
	buf := make([]byte, length)
	i := 0
	for i < length {
		if _, err := rand.Read(buf); err != nil {
			panic("id: out of randomness: " + err.Error())
		}
		for _, b := range buf {
			v := b & 0x1f // 0..31
			if int(v) >= len(alphabet) {
				continue // reject 31 to keep the distribution uniform
			}
			out[i] = alphabet[v]
			i++
			if i == length {
				break
			}
		}
	}
	return string(out)
}
