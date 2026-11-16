package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// validChallenge checks the shape of an S256 code challenge.
func validChallenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	for _, r := range challenge {
		if !isUnreserved(r) {
			return false
		}
	}
	return true
}

// validVerifier checks the shape of an RFC 7636 code verifier.
func validVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, r := range verifier {
		if !isUnreserved(r) {
			return false
		}
	}
	return true
}

// verifyPKCE compares the S256 transform of a verifier with the challenge.
func verifyPKCE(challenge, verifier string) bool {
	if !validVerifier(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func isUnreserved(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '-', r == '.', r == '_', r == '~':
		return true
	}
	return false
}
