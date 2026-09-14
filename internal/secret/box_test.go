package secret

import (
	"path/filepath"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	box, err := Open(dir, "test-data-key", "jwt-secret")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("s3cret!")
	if err != nil {
		t.Fatal(err)
	}
	if sealed == "s3cret!" || sealed == "" {
		t.Fatalf("expected sealed ciphertext, got %q", sealed)
	}
	plain, err := box.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "s3cret!" {
		t.Fatalf("got %q", plain)
	}
	// legacy plaintext
	plain, err = box.Open("legacy-plain")
	if err != nil {
		t.Fatal(err)
	}
	if plain != "legacy-plain" {
		t.Fatalf("got %q", plain)
	}
	// key file persisted
	if _, err := Open(dir, "test-data-key", "jwt-secret"); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Join(dir, rsaFileName)
}

func TestPublicKeyInfo(t *testing.T) {
	box, err := Open(t.TempDir(), "", "jwt")
	if err != nil {
		t.Fatal(err)
	}
	info := box.PublicKeyInfo()
	if info["algorithm"] != "RSA-OAEP" || info["publicKey"] == "" {
		t.Fatalf("%v", info)
	}
}
