package metricsengine

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func parseVersion(path string) (int, error) {
	base := filepath.Base(path)
	parts := strings.SplitN(base, "_", 2)

	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid migration filename %s (expected 0001_name.sql)", base)
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("migration %s must start with a number", base)
	}

	return version, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func nullCheck(s string) driver.Value {
	if s == "" {
		return nil
	}
	return s
}

func nz[T int | int32 | int64](v T) driver.Value {
	if v == 0 {
		return nil
	}
	return int64(v)
}

func rawJSON(b []byte) driver.Value {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

func bodyJSON(b []byte) driver.Value {
	if len(b) == 0 {
		return nil
	}
	if json.Valid(b) {
		return json.RawMessage(b)
	}
	return string(b)
}
