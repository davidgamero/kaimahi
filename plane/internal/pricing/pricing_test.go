package pricing_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gambtho/kaimahi/plane/internal/pricing"
)

func TestCostCentsFloorsOnceOnCombinedTotal(t *testing.T) {
	// 0.75c input + 0.75c output must floor to 1, not 0+0.
	p := pricing.Price{InCentsPer1M: 75, OutCentsPer1M: 75}
	require.Equal(t, int64(1), pricing.CostCents(p, 10_000, 10_000))
}

func TestCostCentsZeroUsage(t *testing.T) {
	p := pricing.Price{InCentsPer1M: 300, OutCentsPer1M: 1500}
	require.Equal(t, int64(0), pricing.CostCents(p, 0, 0))
}

func TestCostCentsLargeCounts(t *testing.T) {
	// 300M input tokens at $3/1M = $900 = 90000 cents; no overflow.
	p := pricing.Price{InCentsPer1M: 300, OutCentsPer1M: 1500}
	require.Equal(t, int64(90_000), pricing.CostCents(p, 300_000_000, 0))
}
