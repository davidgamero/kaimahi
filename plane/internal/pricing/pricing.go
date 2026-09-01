// Package pricing turns token usage into ledger cents. Ported from
// tomte-old's llm/pricing.go cost math; the bundled price table was NOT
// ported — the board forbids invented prices, so every price row is
// operator-configured (see internal/config), and a model without a row is
// "unpriced", which the proxy's priced-pair gate fails closed on when a
// cents budget is in force.
package pricing

// Price is USD cents per 1,000,000 tokens as fixed-point integers:
// 75 means $0.75 per 1M.
type Price struct {
	InCentsPer1M  int `json:"in_cents_per_1m"`
	OutCentsPer1M int `json:"out_cents_per_1m"`
}

// CostCents returns the whole-cent (floored) cost of the given usage.
// Math ported from tomte-old: sum the input and output numerators before
// dividing, so we floor once on the combined total instead of flooring
// input and output separately (which can throw away up to 2 cents per
// call). int64 keeps token counts in the hundreds of millions safe.
func CostCents(p Price, inputTokens, outputTokens int64) int64 {
	numerator := inputTokens*int64(p.InCentsPer1M) + outputTokens*int64(p.OutCentsPer1M)
	return numerator / 1_000_000
}
