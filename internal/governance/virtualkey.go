package governance

import (
	"crypto/rand"
	"crypto/sha256"
	"diffractllm/internal/core"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"maps"
	"math/big"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type VirtualKey struct {
	Key           string
	ClientID      string
	BudgetID      string // always set — use an unlimited budget if no cap needed
	IsActive      bool
	ExpiresAt     *time.Time
	Mode          core.VKMode
	AllowedModels map[core.ModelKey]struct{}
	ModelPools    map[string]struct{}
}

func ToModelKeySet(refs []string) map[core.ModelKey]struct{} {
	if len(refs) == 0 {
		return nil
	}
	s := make(map[core.ModelKey]struct{}, len(refs))
	for _, r := range refs {

		idx := strings.IndexByte(r, '/')
		if idx <= 0 || idx == len(r)-1 {
			continue
		}

		s[core.ModelKey{Provider: core.Provider(r[:idx]), ModelName: r[idx+1:]}] = struct{}{}
	}
	return s
}

type VirtualKeyMap map[string]*VirtualKey

type VirtualkeyCache struct {
	virtual atomic.Pointer[VirtualKeyMap]
	logger  *zap.Logger
}

func (vk *VirtualkeyCache) Lookup(key string) (*VirtualKey, bool) {
	v := vk.virtual.Load()
	if v == nil {
		return nil, false
	}
	entry, exists := (*v)[key]
	return entry, exists
}

func (vk *VirtualkeyCache) Swap(newMap VirtualKeyMap) {
	vk.virtual.Store(&newMap)
}

func (vk *VirtualkeyCache) Upsert(key string, entry *VirtualKey) {
	old := vk.virtual.Load()
	var oldMap VirtualKeyMap
	if old != nil {
		oldMap = *old
	}
	size := len(oldMap)
	if _, exists := oldMap[key]; !exists {
		size++
	}
	newMap := make(VirtualKeyMap, size)
	maps.Copy(newMap, oldMap)
	newMap[key] = entry
	vk.virtual.Store(&newMap)
}

func (vk *VirtualkeyCache) Delete(key string) error {
	old := vk.virtual.Load()
	if old == nil {
		return fmt.Errorf("virtual key store not found: %s", key)
	}
	oldMap := *old
	if _, exists := oldMap[key]; !exists {
		return fmt.Errorf("virtual key not found: %s", key)
	}
	newMap := make(VirtualKeyMap, len(oldMap)-1)
	maps.Copy(newMap, oldMap)
	delete(newMap, key)
	vk.virtual.Store(&newMap)
	return nil
}

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
