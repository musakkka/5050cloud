// SPDX-License-Identifier: AGPL-3.0-only

// Package ids makes the prefixed identifiers of docs/SPEC.md section 6:
// "<prefix>_<26 char ULID, lowercase>", and the random secrets used for
// tokens, codes and generated names.
package ids

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
	"strings"
	"sync"
	"time"
)

// Prefixes used by core. The orchestrator uses its own (dep_, rep_, hst_, ...).
const (
	User         = "usr"
	Session      = "ses"
	Token        = "tok"
	Invite       = "inv"
	Project      = "prj"
	Environment  = "env"
	Service      = "svc"
	Change       = "chg"
	Commit       = "cmt"
	Domain       = "dom"
	TCPProxy     = "tcp"
	Volume       = "vol"
	Webhook      = "whk"
	Audit        = "aud"
	Activity     = "act"
	Group        = "grp"
	Upload       = "upl"
	Variable     = "var"
	Notification = "ntf"
	Monitor      = "mon"
	Import       = "imp"
	Registry     = "reg"
	Delivery     = "dlv"
)

// crockford is the ULID alphabet, lowercase.
const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

var (
	mu       sync.Mutex
	lastMs   uint64
	lastRand [10]byte
)

// New returns "<prefix>_<ulid>". IDs made in the same millisecond are
// monotonic, so sorting by id sorts by creation.
func New(prefix string) string {
	return prefix + "_" + ULID(time.Now())
}

// ULID returns a lowercase 26 character ULID for t.
func ULID(t time.Time) string {
	ms := uint64(t.UnixMilli())
	mu.Lock()
	var r [10]byte
	if ms == lastMs {
		r = lastRand
		// Increment the 80-bit random part.
		for i := 9; i >= 0; i-- {
			r[i]++
			if r[i] != 0 {
				break
			}
		}
	} else {
		if _, err := rand.Read(r[:]); err != nil {
			mu.Unlock()
			panic("ids: the system random source failed: " + err.Error())
		}
	}
	lastMs, lastRand = ms, r
	mu.Unlock()

	var b [16]byte
	binary.BigEndian.PutUint16(b[0:2], uint16(ms>>32))
	binary.BigEndian.PutUint32(b[2:6], uint32(ms))
	copy(b[6:], r[:])
	return encode(b)
}

func encode(b [16]byte) string {
	// 128 bits -> 26 base32 characters (the first carries 3 bits).
	var out [26]byte
	n := new(big.Int).SetBytes(b[:])
	base := big.NewInt(32)
	mod := new(big.Int)
	for i := 25; i >= 0; i-- {
		n.DivMod(n, base, mod)
		out[i] = crockford[mod.Int64()]
	}
	return string(out[:])
}

// Has reports whether id carries prefix.
func Has(id, prefix string) bool { return strings.HasPrefix(id, prefix+"_") }

// Alphabets for Random.
const (
	Alnum      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	LowerAlnum = "abcdefghijklmnopqrstuvwxyz0123456789"
	// Unambiguous upper-case letters and digits for codes people type.
	CodeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

// Random returns n characters drawn uniformly from alphabet.
func Random(n int, alphabet string) string {
	if n <= 0 || alphabet == "" {
		return ""
	}
	max := big.NewInt(int64(len(alphabet)))
	out := make([]byte, n)
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic("ids: the system random source failed: " + err.Error())
		}
		out[i] = alphabet[v.Int64()]
	}
	return string(out)
}

// RandomInt returns a uniform integer in [min, max].
func RandomInt(min, max int64) int64 {
	if max <= min {
		return min
	}
	v, err := rand.Int(rand.Reader, big.NewInt(max-min+1))
	if err != nil {
		panic("ids: the system random source failed: " + err.Error())
	}
	return min + v.Int64()
}

var adjectives = []string{
	"sturdy", "brave", "calm", "eager", "fancy", "gentle", "happy", "jolly", "keen", "lively",
	"merry", "nimble", "proud", "quick", "quiet", "rapid", "shiny", "silent", "smart", "sunny",
	"swift", "tidy", "vivid", "warm", "wise", "bold", "bright", "clever", "cosmic", "crisp",
	"daring", "fluffy", "golden", "humble", "icy", "lucky", "mellow", "mighty", "noble", "polite",
}

var animals = []string{
	"otter", "badger", "falcon", "heron", "koala", "lynx", "marten", "narwhal", "ocelot", "panda",
	"quokka", "raven", "seal", "tapir", "urchin", "walrus", "yak", "zebra", "beaver", "bison",
	"crane", "dingo", "eagle", "ferret", "gecko", "ibis", "jaguar", "kiwi", "lemur", "moose",
	"newt", "owl", "puffin", "robin", "salmon", "tiger", "vole", "wombat", "alpaca", "camel",
}

// TwoWordName returns a generated project name such as "sturdy-otter".
func TwoWordName() string {
	a := adjectives[RandomInt(0, int64(len(adjectives)-1))]
	b := animals[RandomInt(0, int64(len(animals)-1))]
	return a + "-" + b
}
