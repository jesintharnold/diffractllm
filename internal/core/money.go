package core

import "math"

const NanoUSD = 1_000_000_000
const BudgetUnitNanoUSD = "nanodollars"
const maxNanoUSD = math.MaxInt64

func ToNanoUSD(usd float64) int64 {
	if math.IsNaN(usd) || usd <= 0 {
		return 0
	}
	nano := math.Round(usd * NanoUSD)
	if math.IsInf(nano, 0) || nano >= maxNanoUSD {
		return maxNanoUSD
	}
	return int64(nano)
}

func FromNanoUSD(nano int64) float64 {
	return float64(nano) / NanoUSD
}
