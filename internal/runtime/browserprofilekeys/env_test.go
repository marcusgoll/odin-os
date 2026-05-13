package browserprofilekeys

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"odin-os/internal/runtime/browserprofilecrypto"
)

func TestLoadFromEnvLoadsValidExplicitKey(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, browserprofilecrypto.KeySize)
	t.Setenv(EnvKeyB64, base64.StdEncoding.EncodeToString(key))

	material, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if !bytes.Equal(material.Bytes(), key) {
		t.Fatalf("material.Bytes() = %x, want explicit env key", material.Bytes())
	}
	if material.Ref != "env:"+EnvKeyB64 {
		t.Fatalf("material.Ref = %q, want env reference", material.Ref)
	}
}

func TestLoadFromEnvMissingKeyFailsClosed(t *testing.T) {
	t.Setenv(EnvKeyB64, "")

	_, err := LoadFromEnv()
	if err == nil || !strings.Contains(err.Error(), EnvKeyB64) {
		t.Fatalf("LoadFromEnv() error = %v, want missing %s rejection", err, EnvKeyB64)
	}
}

func TestLoadFromEnvRejectsInvalidBase64(t *testing.T) {
	t.Setenv(EnvKeyB64, "not-base64")

	_, err := LoadFromEnv()
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("LoadFromEnv() error = %v, want base64 rejection", err)
	}
}

func TestLoadFromEnvRejectsWrongLength(t *testing.T) {
	t.Setenv(EnvKeyB64, base64.StdEncoding.EncodeToString([]byte("short")))

	_, err := LoadFromEnv()
	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("LoadFromEnv() error = %v, want key length rejection", err)
	}
}

func TestLoadFromEnvKeyRefDoesNotExposeSecret(t *testing.T) {
	key := bytes.Repeat([]byte{0x7a}, browserprofilecrypto.KeySize)
	encoded := base64.StdEncoding.EncodeToString(key)
	t.Setenv(EnvKeyB64, encoded)

	material, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if strings.Contains(material.Ref, encoded) {
		t.Fatalf("material.Ref exposes base64 key: %q", material.Ref)
	}
	if strings.Contains(material.Ref, string(key)) {
		t.Fatalf("material.Ref exposes raw key bytes: %q", material.Ref)
	}
}

func TestMaterialDoesNotExposeKeyAsExportedField(t *testing.T) {
	materialType := reflect.TypeOf(Material{})
	for i := 0; i < materialType.NumField(); i++ {
		field := materialType.Field(i)
		if field.PkgPath == "" && field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Uint8 {
			t.Fatalf("Material exposes key-like exported field %s", field.Name)
		}
	}
}
