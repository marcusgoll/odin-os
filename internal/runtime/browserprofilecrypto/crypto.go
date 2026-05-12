package browserprofilecrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

const (
	EnvelopeVersion    = 1
	AlgorithmAES256GCM = "AES-256-GCM"
	KeySize            = 32
	NonceSize          = 12
)

type Envelope struct {
	Version    int    `json:"version"`
	Algorithm  string `json:"algorithm"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func Encrypt(key []byte, plaintext []byte) (Envelope, error) {
	aead, err := aeadFromKey(key)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Envelope{}, fmt.Errorf("browser profile crypto nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	return Envelope{
		Version:    EnvelopeVersion,
		Algorithm:  AlgorithmAES256GCM,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}, nil
}

func Decrypt(key []byte, envelope Envelope) ([]byte, error) {
	if envelope.Version != EnvelopeVersion {
		return nil, fmt.Errorf("browser profile crypto unsupported envelope version: %d", envelope.Version)
	}
	if envelope.Algorithm != AlgorithmAES256GCM {
		return nil, fmt.Errorf("browser profile crypto unsupported algorithm: %s", envelope.Algorithm)
	}
	aead, err := aeadFromKey(key)
	if err != nil {
		return nil, err
	}
	if len(envelope.Nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("browser profile crypto nonce size = %d, want %d", len(envelope.Nonce), aead.NonceSize())
	}
	plaintext, err := aead.Open(nil, envelope.Nonce, envelope.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("browser profile crypto authentication failed")
	}
	return plaintext, nil
}

func aeadFromKey(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("browser profile crypto key must be %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("browser profile crypto cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("browser profile crypto gcm: %w", err)
	}
	if aead.NonceSize() != NonceSize {
		return nil, fmt.Errorf("browser profile crypto nonce size = %d, want %d", aead.NonceSize(), NonceSize)
	}
	return aead, nil
}
