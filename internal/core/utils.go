package core

import (
	"fmt"
	"strings"
	"time"
)

// parse durations and all related to the core

func ParseDuration(duration string) (time.Duration, error) {
	frequency := strings.ToLower(duration[len(duration)-1:])
	switch frequency {
	case "d":
		days := duration[:len(duration)-1]
		if d, err := time.ParseDuration(days + "h"); err == nil {
			return d * 24, nil
		}
		return 0, fmt.Errorf("invalid duration for days : %s", duration)
	case "w":
		weeks := duration[:len(duration)-1]
		if w, err := time.ParseDuration(weeks + "h"); err == nil {
			return w * 24 * 7, nil
		}
		return 0, fmt.Errorf("invalid duration for week : %s", duration)
	case "m":
		month := duration[:len(duration)-1]
		if m, err := time.ParseDuration(month + "h"); err == nil {
			return m * 24 * 30, nil
		}
		return 0, fmt.Errorf("invalid duration for month : %s", duration)
	case "y":
		year := duration[:len(duration)-1]
		if y, err := time.ParseDuration(year + "h"); err == nil {
			return y * 24 * 365, nil
		}
		return 0, fmt.Errorf("invalid duration for year : %s", duration)
	default:
		return time.ParseDuration(duration)
	}
}
