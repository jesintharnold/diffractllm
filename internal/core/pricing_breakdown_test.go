package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 1284 in (512 of them cache reads) + 902 out: three lines that sum to the bill.
func TestCostBreakdownSumsToTheBill(t *testing.T) {
	in, out, cached := 2.5e-6, 10e-6, 1.25e-6
	p := Pricing{InputCostPerToken: &in, OutputCostPerToken: &out, CacheReadInputTokenCost: &cached}
	u := Usage{InputTokens: 1284, OutputTokens: 902, CachedInputTokens: 512}

	lines := CostBreakdown(p, u)
	require.Len(t, lines, 3)
	assert.Equal(t, CostLine{Item: "input", Quantity: 772, Unit: "token", UnitCost: in, CostUSD: 772 * in}, lines[0])
	assert.Equal(t, "output", lines[1].Item)
	assert.Equal(t, "input_cache_read", lines[2].Item)

	var sum float64
	for _, l := range lines {
		sum += l.CostUSD
	}
	assert.InDelta(t, CalculateCost(p, u), sum, 1e-15)
}
