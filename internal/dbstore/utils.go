package dbstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

func optKey(k string) *string {
	if k == "" {
		return nil
	}
	return &k
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func encryptKey(APIKey *string, Passkey []byte) (*string, error) {
	block, err := aes.NewCipher(Passkey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(*APIKey), nil)
	ans := base64.StdEncoding.EncodeToString(ciphertext)
	return &ans, nil
}

func decryptKey(encryptedAPIKey *string, Passkey []byte) (*string, error) {
	data, err := base64.StdEncoding.DecodeString(*encryptedAPIKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(Passkey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	ans := string(plaintext)
	return &ans, nil
}
