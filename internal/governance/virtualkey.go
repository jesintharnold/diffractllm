package governance

import (
	"crypto/rand"
	"crypto/sha256"
	"diffractllm/internal/core"
	"encoding/hex"
	"hash/crc32"
	"io"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	rkPrefix      = "rk-"
	rkTotalLen    = 25
	rkPayloadLen  = 18
	rkChecksumLen = 4
	rkChecksumMod = uint32(62 * 62 * 62 * 62)
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

	payload := base62Encode(&n, rkPayloadLen)
	prefixPart := rkPrefix + payload
	chksumDigits := crc32.ChecksumIEEE([]byte(prefixPart)) % rkChecksumMod

	n.SetUint64(uint64(chksumDigits))
	checksum := base62Encode(&n, rkChecksumLen)
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

	if len(apiKey) != rkTotalLen {
		return false
	}

	if apiKey[:len(rkPrefix)] != rkPrefix {
		return false
	}

	for i := len(rkPrefix); i < rkTotalLen; i++ {
		if !isBase62Char(apiKey[i]) {
			return false
		}
	}

	payload := apiKey[:len(rkPrefix)+rkPayloadLen]
	chksumcal := crc32.ChecksumIEEE([]byte(payload)) % rkChecksumMod
	var n big.Int
	n.SetUint64(uint64(chksumcal))
	return apiKey[len(rkPrefix)+rkPayloadLen:] == base62Encode(&n, rkChecksumLen)
}

type VirtualKeyMap map[string]*core.VirtualKey

type VirtualkeyCache struct {
	virtual  atomic.Pointer[VirtualKeyMap]
	mu       sync.Mutex
	LastSync time.Time
	logger   *zap.Logger
}

func (vk *VirtualkeyCache) LookupVkey(key string) (*core.VirtualKey, bool) {
	v := vk.virtual.Load()
	if v == nil {
		return nil, false
	}
	entry, exists := (*v)[key]
	return entry, exists
}

func (vk *VirtualkeyCache) LoadVirtualKeys(vdata []*core.VirtualKey) {
	tempVkey := make(VirtualKeyMap, len(vdata))
	for _, vkey := range vdata {
		tempVkey[vkey.Key] = vkey
	}
	vk.mu.Lock()
	defer vk.mu.Unlock()
	vk.virtual.Store(&tempVkey)
	vk.LastSync = time.Now()
	vk.logger.Debug("virtual key cache hot-swapped", zap.Int("keys", len(tempVkey)))
}
