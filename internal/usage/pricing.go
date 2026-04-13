package usage

import "log/slog"

// ModelPricing holds per-million-token rates for a model.
type ModelPricing struct {
	InputPer1M       float64
	OutputPer1M      float64
	CacheReadPer1M   float64
	CacheCreatePer1M float64
}

// Pricing maps model names to their per-million-token rates.
var Pricing = map[string]ModelPricing{
	"claude-opus-4-6":   {InputPer1M: 15.00, OutputPer1M: 75.00, CacheReadPer1M: 1.50, CacheCreatePer1M: 18.75},
	"claude-sonnet-4-6": {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheCreatePer1M: 3.75},
	"claude-haiku-4-5":  {InputPer1M: 0.80, OutputPer1M: 4.00, CacheReadPer1M: 0.08, CacheCreatePer1M: 1.00},
}

// CalcCost computes the USD cost for given token counts.
// Ollama models and unknown models return $0. Unknown models log a warning.
func CalcCost(model string, input, output, cacheRead, cacheCreate int64) float64 {
	p, ok := Pricing[model]
	if !ok {
		// Ollama models (contain ':') are expected to be free — no warning needed.
		if !isOllamaModel(model) && model != "" {
			slog.Warn("unknown model pricing, defaulting to $0", "model", model)
		}
		return 0
	}
	cost := float64(input)*p.InputPer1M/1e6 +
		float64(output)*p.OutputPer1M/1e6 +
		float64(cacheRead)*p.CacheReadPer1M/1e6 +
		float64(cacheCreate)*p.CacheCreatePer1M/1e6
	return cost
}

// isOllamaModel detects Ollama-style model names (e.g. "gemma3:27b").
func isOllamaModel(model string) bool {
	for _, c := range model {
		if c == ':' {
			return true
		}
	}
	return false
}
