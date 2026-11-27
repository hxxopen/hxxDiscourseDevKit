// Package id implements ULID generation without external dependencies.
package id

import (
	"crypto/rand"
	"errors"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ErrInvalid reports a string that is not a canonical ULID.
var ErrInvalid = errors.New("invalid ULID")

// New returns a new ULID string (26 uppercase Crockford base32 characters).
func New() (string, error) {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		return "", err
	}
	return encode(b), nil
}

// MustNew returns a new ULID and panics only if the system entropy source fails.
func MustNew() string {
	s, err := New()
	if err != nil {
		panic("id: " + err.Error())
	}
	return s
}

// Valid reports whether s is a canonical 26-character ULID.
func Valid(s string) bool {
	if len(s) != 26 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if decodeChar(s[i]) < 0 {
			return false
		}
	}
	// The first character encodes only 3 meaningful timestamp bits.
	if decodeChar(s[0]) > 7 {
		return false
	}
	return true
}

func encode(b [16]byte) string {
	var out [26]byte
	// Timestamp: 48 bits -> 10 characters (50 bits, top two zero).
	ts := uint64(b[0])<<40 | uint64(b[1])<<32 | uint64(b[2])<<24 |
		uint64(b[3])<<16 | uint64(b[4])<<8 | uint64(b[5])
	for i := 9; i >= 0; i-- {
		out[i] = crockford[ts&31]
		ts >>= 5
	}
	// Random: 80 bits -> 16 characters.
	for i := 0; i < 16; i++ {
		var v byte
		for j := 0; j < 5; j++ {
			bit := i*5 + j
			byteIdx := 6 + bit/8
			bitOff := 7 - uint(bit%8)
			v = (v << 1) | ((b[byteIdx] >> bitOff) & 1)
		}
		out[10+i] = crockford[v]
	}
	return string(out[:])
}

func decodeChar(c byte) int {
	// ULIDs are case-insensitive on input; generated values are uppercase.
	if c >= 'a' && c <= 'z' {
		c -= 'a' - 'A'
	}
	for i := 0; i < len(crockford); i++ {
		if crockford[i] == c {
			return i
		}
	}
	return -1
}
