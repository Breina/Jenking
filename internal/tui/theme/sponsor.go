package theme

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
)

// sponsorPublicKey is the Ed25519 public key used to verify sponsor tokens.
// The corresponding private key is never committed; use generate-key.sh to mint tokens.
// A valid token is hex(ed25519.Sign(privateKey, []byte(lowercase(username)))).
var sponsorPublicKey = ed25519.PublicKey{
	0x25, 0xbc, 0x61, 0x11, 0x95, 0xe1, 0x09, 0xbd,
	0xd0, 0x00, 0x76, 0xce, 0x57, 0xcd, 0x3a, 0xb7,
	0x28, 0x2f, 0x6f, 0xac, 0xa4, 0x65, 0x6e, 0x8c,
	0x47, 0xb1, 0xb2, 0xe3, 0x94, 0x3c, 0x49, 0x15,
}

// IsSponsor reports whether key is a valid sponsor token for the given Jenkins username.
// Generate a token with generate-key.sh (requires the private key).
func IsSponsor(username, key string) bool {
	sig, err := hex.DecodeString(key)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	msg := []byte(strings.ToLower(strings.TrimSpace(username)))
	return ed25519.Verify(sponsorPublicKey, msg, sig)
}
