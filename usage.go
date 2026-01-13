package langai

import "fmt"

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int

	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

// Price is an amount in 1/1_000_000_000 of a cent (1e-9 cent).
//
// I.e. $2 per 1M tokens = $0.002 per 1K tokens = Price(200_000) per token.
type Price int64

func (p Price) String() string {
	return fmt.Sprintf("$%0.2f", float64(p)/100_000_000_000)
}
