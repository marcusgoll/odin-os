package browserprofilekeys

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"odin-os/internal/runtime/browserprofilecrypto"
)

const EnvKeyB64 = "ODIN_BROWSER_PROFILE_KEY_B64"

type Material struct {
	key []byte
	Ref string
}

func (m Material) Bytes() []byte {
	return append([]byte(nil), m.key...)
}

func LoadFromEnv() (Material, error) {
	raw := strings.TrimSpace(os.Getenv(EnvKeyB64))
	if raw == "" {
		return Material{}, fmt.Errorf("%s is required for browser profile encryption", EnvKeyB64)
	}
	key, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil {
		return Material{}, fmt.Errorf("%s must be base64 encoded: %w", EnvKeyB64, err)
	}
	if len(key) != browserprofilecrypto.KeySize {
		return Material{}, fmt.Errorf("%s must decode to %d bytes", EnvKeyB64, browserprofilecrypto.KeySize)
	}
	return Material{
		key: append([]byte(nil), key...),
		Ref: "env:" + EnvKeyB64,
	}, nil
}
