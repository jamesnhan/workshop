package usage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalcCost_opus(t *testing.T) {
	// 1M input tokens at $15/M = $15.00
	cost := CalcCost("claude-opus-4-6", 1_000_000, 0, 0, 0)
	assert.InDelta(t, 15.0, cost, 0.001)

	// 1M output tokens at $75/M = $75.00
	cost = CalcCost("claude-opus-4-6", 0, 1_000_000, 0, 0)
	assert.InDelta(t, 75.0, cost, 0.001)

	// Mixed: 10k input + 5k output + 50k cache read + 1k cache create
	cost = CalcCost("claude-opus-4-6", 10_000, 5_000, 50_000, 1_000)
	expected := 10_000*15.0/1e6 + 5_000*75.0/1e6 + 50_000*1.5/1e6 + 1_000*18.75/1e6
	assert.InDelta(t, expected, cost, 0.0001)
}

func TestCalcCost_sonnet(t *testing.T) {
	cost := CalcCost("claude-sonnet-4-6", 1_000_000, 0, 0, 0)
	assert.InDelta(t, 3.0, cost, 0.001)
}

func TestCalcCost_haiku(t *testing.T) {
	cost := CalcCost("claude-haiku-4-5", 1_000_000, 0, 0, 0)
	assert.InDelta(t, 0.80, cost, 0.001)
}

func TestCalcCost_ollama(t *testing.T) {
	// Ollama models (contain ':') should be $0
	cost := CalcCost("gemma3:27b", 100_000, 50_000, 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestCalcCost_unknown(t *testing.T) {
	// Unknown models default to $0 (with a log warning)
	cost := CalcCost("some-future-model", 100_000, 50_000, 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestCalcCost_empty(t *testing.T) {
	cost := CalcCost("", 0, 0, 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestIsOllamaModel(t *testing.T) {
	assert.True(t, isOllamaModel("gemma3:27b"))
	assert.True(t, isOllamaModel("stheno:8b"))
	assert.False(t, isOllamaModel("claude-opus-4-6"))
	assert.False(t, isOllamaModel(""))
}
