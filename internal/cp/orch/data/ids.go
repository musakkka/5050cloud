// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

// crockford is the lowercase Crockford base32 alphabet used by ULIDs.
const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

var (
	ulidMu   sync.Mutex
	ulidLast [16]byte
)

// newID returns "<prefix>_<26 char ULID, lowercase>" (docs/SPEC.md 6). IDs
// made in the same millisecond are strictly increasing.
func newID(prefix string) string {
	return prefix + "_" + ulid(time.Now())
}

func ulid(t time.Time) string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	var b [16]byte
	ms := uint64(t.UnixMilli())
	binary.BigEndian.PutUint16(b[0:2], uint16(ms>>32))
	binary.BigEndian.PutUint32(b[2:6], uint32(ms))
	if string(b[0:6]) == string(ulidLast[0:6]) {
		// Same millisecond: increment the random part of the last id.
		copy(b[6:], ulidLast[6:])
		for i := 15; i >= 6; i-- {
			b[i]++
			if b[i] != 0 {
				break
			}
		}
	} else if _, err := rand.Read(b[6:]); err != nil {
		panic("data: the system random source failed: " + err.Error())
	}
	ulidLast = b
	return encodeULID(b)
}

func encodeULID(b [16]byte) string {
	// 128 bits as 26 base32 digits: the first digit carries 3 bits.
	var out [26]byte
	hi := binary.BigEndian.Uint64(b[0:8])
	lo := binary.BigEndian.Uint64(b[8:16])
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}
