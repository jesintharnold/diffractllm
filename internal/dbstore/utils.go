package dbstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

func encryptKey(APIKey string, Passkey []byte) (string, error) {
	block, err := aes.NewCipher(Passkey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(APIKey), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptKey(encryptedAPIKey string, Passkey []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encryptedAPIKey)
	if err != nil {
		return "", nil
	}
	block, err := aes.NewCipher(Passkey)
	if err != nil {
		return "", nil
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
