package browserprofilecrypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTripWithExplicitKey(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, KeySize)
	plaintext := []byte("fixture browser profile archive bytes")

	envelope, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if envelope.Version != EnvelopeVersion {
		t.Fatalf("envelope.Version = %d, want %d", envelope.Version, EnvelopeVersion)
	}
	if envelope.Algorithm != AlgorithmAES256GCM {
		t.Fatalf("envelope.Algorithm = %q, want %q", envelope.Algorithm, AlgorithmAES256GCM)
	}
	if len(envelope.Nonce) == 0 || len(envelope.Ciphertext) == 0 {
		t.Fatalf("envelope nonce/ciphertext lengths = %d/%d, want non-empty", len(envelope.Nonce), len(envelope.Ciphertext))
	}
	if bytes.Contains(envelope.Ciphertext, plaintext) {
		t.Fatalf("ciphertext contains plaintext fixture bytes")
	}

	decrypted, err := Decrypt(key, envelope)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted = %q, want plaintext fixture", decrypted)
	}
}

func TestDecryptRejectsWrongKeyAndTamperedCiphertext(t *testing.T) {
	key := bytes.Repeat([]byte{0x22}, KeySize)
	wrongKey := bytes.Repeat([]byte{0x33}, KeySize)
	envelope, err := Encrypt(key, []byte("fixture profile archive"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	if _, err := Decrypt(wrongKey, envelope); err == nil {
		t.Fatal("Decrypt(wrong key) error = nil, want authentication failure")
	}

	tampered := envelope
	tampered.Ciphertext = append([]byte(nil), envelope.Ciphertext...)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 0x01
	if _, err := Decrypt(key, tampered); err == nil {
		t.Fatal("Decrypt(tampered ciphertext) error = nil, want authentication failure")
	}
}

func TestEncryptDecryptRejectMissingOrInvalidKey(t *testing.T) {
	_, err := Encrypt(nil, []byte("fixture"))
	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("Encrypt(nil key) error = %v, want key rejection", err)
	}

	_, err = Encrypt([]byte("short"), []byte("fixture"))
	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("Encrypt(short key) error = %v, want key rejection", err)
	}

	envelope := Envelope{
		Version:    EnvelopeVersion,
		Algorithm:  AlgorithmAES256GCM,
		Nonce:      bytes.Repeat([]byte{0x44}, NonceSize),
		Ciphertext: []byte("ciphertext"),
	}
	_, err = Decrypt(nil, envelope)
	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("Decrypt(nil key) error = %v, want key rejection", err)
	}
}

func TestEncryptUsesRandomNonce(t *testing.T) {
	key := bytes.Repeat([]byte{0x55}, KeySize)
	plaintext := []byte("same fixture plaintext")

	first, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt(first) error = %v", err)
	}
	second, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt(second) error = %v", err)
	}
	if bytes.Equal(first.Nonce, second.Nonce) {
		t.Fatalf("nonce reused across encryptions: %x", first.Nonce)
	}
	if bytes.Equal(first.Ciphertext, second.Ciphertext) {
		t.Fatalf("ciphertext repeated across random-nonce encryptions")
	}
}
