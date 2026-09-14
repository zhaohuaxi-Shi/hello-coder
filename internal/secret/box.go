package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	storagePrefix = "hc1:"
	rsaBits       = 2048
	rsaFileName   = "transport_rsa.pem"
)

// Box handles transport (RSA-OAEP) and at-rest (AES-GCM) secrecy for connection passwords.
type Box struct {
	aesKey    []byte
	private   *rsa.PrivateKey
	publicB64 string // SPKI (PKIX) base64 for browsers
}

// Open loads or creates the RSA keypair under dataDir and derives the AES key.
// dataKey empty → AES key derived from jwtSecret (dev-friendly; set HELLO_CODER_DATA_KEY in production).
func Open(dataDir, dataKey, jwtSecret string) (*Box, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	priv, err := loadOrCreateRSA(filepath.Join(dataDir, rsaFileName))
	if err != nil {
		return nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	return &Box{
		aesKey:    deriveAESKey(dataKey, jwtSecret),
		private:   priv,
		publicB64: base64.StdEncoding.EncodeToString(pubDER),
	}, nil
}

func deriveAESKey(dataKey, jwtSecret string) []byte {
	material := dataKey
	if material == "" {
		material = "hello-coder-data:" + jwtSecret
	}
	sum := sha256.Sum256([]byte(material))
	return sum[:]
}

func loadOrCreateRSA(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("invalid RSA PEM: %s", path)
		}
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			pkcs8, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err2 != nil {
				return nil, fmt.Errorf("parse RSA key: %w", err)
			}
			var ok bool
			key, ok = pkcs8.(*rsa.PrivateKey)
			if !ok {
				return nil, errors.New("PEM is not an RSA private key")
			}
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, fmt.Errorf("generate RSA: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write RSA key: %w", err)
	}
	return key, nil
}

// PublicKeyInfo is returned to the browser for Web Crypto RSA-OAEP.
func (b *Box) PublicKeyInfo() map[string]string {
	return map[string]string{
		"algorithm": "RSA-OAEP",
		"hash":      "SHA-256",
		"publicKey": b.publicB64,
	}
}

// UnwrapTransport decrypts a base64 RSA-OAEP ciphertext from the client.
// Empty input stays empty. Non-RSA payloads are treated as legacy plaintext (compat).
func (b *Box) UnwrapTransport(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(raw) != b.private.Size() {
		return value, nil
	}
	plain, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, b.private, raw, nil)
	if err != nil {
		return "", fmt.Errorf("invalid encrypted password")
	}
	return string(plain), nil
}

// Seal encrypts a secret for SQLite storage (AES-GCM). Empty stays empty.
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if strings.HasPrefix(plain, storagePrefix) {
		return plain, nil
	}
	block, err := aes.NewCipher(b.aesKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return storagePrefix + base64.StdEncoding.EncodeToString(out), nil
}

// Open decrypts a sealed secret. Legacy plaintext (no hc1: prefix) is returned as-is.
func (b *Box) Open(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, storagePrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, storagePrefix))
	if err != nil {
		return "", fmt.Errorf("corrupt sealed secret: %w", err)
	}
	block, err := aes.NewCipher(b.aesKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("corrupt sealed secret: too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt sealed secret: %w", err)
	}
	return string(plain), nil
}
