package dao

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func newRuntimeID(prefix string, size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("生成 %s ID: %w", prefix, err)
	}
	return prefix + hex.EncodeToString(value), nil
}
