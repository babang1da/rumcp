package secret_test

import (
	"strings"
	"testing"

	"github.com/babang1da/rumcp/server/internal/secret"
)

func testKey(fill byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill
	}
	return key
}

func TestSealHidesPlaintext(t *testing.T) {
	box, err := secret.NewBox(testKey(1))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}

	sealed, err := box.Seal("yandex-token-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, "yandex-token-1") {
		t.Fatalf("sealed value contains the plaintext: %q", sealed)
	}
	if !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("sealed value has no version prefix: %q", sealed)
	}

	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened != "yandex-token-1" {
		t.Fatalf("Open returned %q", opened)
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	box, _ := secret.NewBox(testKey(2))
	first, _ := box.Seal("same")
	second, _ := box.Seal("same")
	if first == second {
		t.Fatal("two seals of the same value are identical, nonce is not random")
	}
}

func TestOpenWithAnotherKeyFails(t *testing.T) {
	first, _ := secret.NewBox(testKey(3))
	second, _ := secret.NewBox(testKey(4))

	sealed, err := first.Seal("token")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := second.Open(sealed); err == nil {
		t.Fatal("another key opened the value")
	}
}

func TestOpenRejectsGarbage(t *testing.T) {
	box, _ := secret.NewBox(testKey(5))
	for _, value := range []string{"", "token", "v2:abc", "v1:not-base64!!"} {
		if _, err := box.Open(value); err == nil {
			t.Errorf("Open(%q) succeeded, want error", value)
		}
	}
}

func TestNewBoxRequiresAES256Key(t *testing.T) {
	if _, err := secret.NewBox([]byte("short")); err == nil {
		t.Fatal("NewBox accepted a short key")
	}
}
