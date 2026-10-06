package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"
	"io"
	"math/big"
)

const (
	dkPrefix      = "dk-"
	dkTotalLen    = 25
	dkPayloadLen  = 18
	dkChecksumLen = 4
	dkChecksumMod = uint32(62 * 62 * 62 * 62)
)

const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func base62Encode(n *big.Int, length int) string {
	base := big.NewInt(62)
	remainder := new(big.Int)
	result := make([]byte, 0, length)

	for len(result) < length {
		n.DivMod(n, base, remainder)
		result = append(result, base62Chars[remainder.Int64()])
	}

	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}

func GenerateVirtualKey() (apikey, hash, prefix string, err error) {
	raw := make([]byte, 14)
	if _, err = io.ReadFull(rand.Reader, raw); err != nil {
		return
	}

	var n big.Int
	n.SetBytes(raw)

	payload := base62Encode(&n, dkPayloadLen)
	prefixPart := dkPrefix + payload
	chksumDigits := crc32.ChecksumIEEE([]byte(prefixPart)) % dkChecksumMod

	n.SetUint64(uint64(chksumDigits))
	checksum := base62Encode(&n, dkChecksumLen)
	apikey = prefixPart + checksum
	prefix = apikey[:11]

	sumhash := sha256.Sum256([]byte(apikey))
	hash = hex.EncodeToString(sumhash[:])
	return
}

func isBase62Char(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func ValidateKeySignature(apiKey string) bool {

	if len(apiKey) != dkTotalLen {
		return false
	}

	if apiKey[:len(dkPrefix)] != dkPrefix {
		return false
	}

	for i := len(dkPrefix); i < dkTotalLen; i++ {
		if !isBase62Char(apiKey[i]) {
			return false
		}
	}

	payload := apiKey[:len(dkPrefix)+dkPayloadLen]
	chksumcal := crc32.ChecksumIEEE([]byte(payload)) % dkChecksumMod
	var n big.Int
	n.SetUint64(uint64(chksumcal))
	return apiKey[len(dkPrefix)+dkPayloadLen:] == base62Encode(&n, dkChecksumLen)
}
