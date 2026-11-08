package utils

import (
	"fmt"
	"strconv"

	"github.com/google/uuid"
)

func (q *QueryParams) GetUUIDs(name string) ([]uuid.UUID, error) {
	parts := q.GetSlice(name)
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]uuid.UUID, 0, len(parts))
	for _, part := range parts {
		id, err := uuid.Parse(part)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a valid uuid", name, part)
		}
		out = append(out, id)
	}
	return out, nil
}

func (q *QueryParams) GetInt(name string) (int, bool, error) {
	raw := q.Values.Get(name)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, true, fmt.Errorf("%s: %q is not an integer", name, raw)
	}
	return v, true, nil
}

func ParseInt(val string, defaultVal int) (int, error) {
	if val == "" {
		return defaultVal, nil
	}
	return strconv.Atoi(val)
}

func AesEncrypt(plainText string, masterKey []byte) ([]byte, error) {
	return aesEncryptProphet(plainText, masterKey)
}

func AesDecrypt(cipherText []byte, masterKey []byte) (string, error) {
	return aesDecryptProphet(cipherText, masterKey)
}
