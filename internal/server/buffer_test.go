package server

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- OutputBuffer.Append ---

func TestOutputBuffer_Append_StoresLines(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("alpha:1.1", "line one\nline two\nline three")

	results := ob.Search("line", "alpha:1.1", 10)
	require.Len(t, results, 3)
	assert.Equal(t, "line one", results[0].Content)
	assert.Equal(t, "line two", results[1].Content)
	assert.Equal(t, "line three", results[2].Content)
}

func TestOutputBuffer_Append_StripsANSI(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("alpha:1.1", "\x1b[31mred text\x1b[0m")

	results := ob.Search("red text", "alpha:1.1", 10)
	require.Len(t, results, 1)
	assert.Equal(t, "red text", results[0].Content)
	// Raw should preserve the original ANSI
	assert.Contains(t, results[0].Raw, "\x1b[31m")
}

func TestOutputBuffer_Append_SkipsBlankLines(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("alpha:1.1", "hello\n\n\nworld")

	results := ob.Search("", "alpha:1.1", 100) // won't match blank since they're skipped
	// Only "hello" and "world" should be stored
	assert.Len(t, results, 2)
}

func TestOutputBuffer_Append_TrimsCR(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("alpha:1.1", "hello\r\nworld\r")

	results := ob.Search("hello", "alpha:1.1", 10)
	require.Len(t, results, 1)
	assert.Equal(t, "hello", results[0].Content)
}

// --- Ring buffer wrapping ---

func TestOutputBuffer_RingBuffer_Wraps(t *testing.T) {
	ob := NewOutputBuffer(3) // only 3 lines max
	ob.Append("t:1.1", "one\ntwo\nthree\nfour\nfive")

	// Oldest lines (one, two) should be evicted
	results := ob.Search("one", "t:1.1", 10)
	assert.Empty(t, results)

	results = ob.Search("two", "t:1.1", 10)
	assert.Empty(t, results)

	// Newest lines should remain
	results = ob.Search("three", "t:1.1", 10)
	assert.Len(t, results, 1)
	results = ob.Search("four", "t:1.1", 10)
	assert.Len(t, results, 1)
	results = ob.Search("five", "t:1.1", 10)
	assert.Len(t, results, 1)
}

// --- UpdateFromCapture ---

func TestOutputBuffer_UpdateFromCapture_ReplacesBuffer(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("t:1.1", "old line one\nold line two")

	ob.UpdateFromCapture("t:1.1", "new line alpha\nnew line beta\nnew line gamma")

	// Old lines should be gone
	results := ob.Search("old", "t:1.1", 10)
	assert.Empty(t, results)

	// New lines should be present
	results = ob.Search("new line", "t:1.1", 10)
	assert.Len(t, results, 3)
}

func TestOutputBuffer_UpdateFromCapture_SkipsBlanksAndTrimsWhitespace(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.UpdateFromCapture("t:1.1", "  hello   \t\n\n   \n  world  \t")

	results := ob.ListAll("t:1.1")
	require.Len(t, results, 2)
	// Stripped content should be trimmed
	assert.Equal(t, "hello", results[0]["content"])
	assert.Equal(t, "world", results[1]["content"])
}

// --- Search ---

func TestOutputBuffer_Search_CaseInsensitive(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("t:1.1", "Hello World\nGOODBYE WORLD")

	results := ob.Search("hello", "t:1.1", 10)
	require.Len(t, results, 1)
	assert.Equal(t, "Hello World", results[0].Content)

	results = ob.Search("WORLD", "t:1.1", 10)
	assert.Len(t, results, 2)
}

func TestOutputBuffer_Search_DefaultLimit(t *testing.T) {
	ob := NewOutputBuffer(200)
	// Write 150 lines that all match
	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, "match line")
	}
	ob.Append("t:1.1", strings.Join(lines, "\n"))

	// With limit=0, should default to 100
	results := ob.Search("match", "t:1.1", 0)
	assert.Len(t, results, 100)
}

func TestOutputBuffer_Search_MaxLimit(t *testing.T) {
	ob := NewOutputBuffer(10)
	ob.Append("t:1.1", "x")

	// Limit > maxSearchLimit should be capped
	results := ob.Search("x", "t:1.1", 9999)
	assert.Len(t, results, 1) // only 1 line exists, but limit was capped to 500
}

func TestOutputBuffer_Search_AllPanes(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("a:1.1", "shared keyword here")
	ob.Append("b:1.1", "shared keyword there")

	// Search all panes (empty target)
	results := ob.Search("shared keyword", "", 10)
	assert.Len(t, results, 2)
}

func TestOutputBuffer_Search_NonexistentTarget(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("a:1.1", "data")

	results := ob.Search("data", "nonexistent:1.1", 10)
	assert.Empty(t, results)
}

func TestOutputBuffer_Search_IncludesLineNumbers(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("t:1.1", "alpha\nbeta\ngamma")

	results := ob.Search("beta", "t:1.1", 10)
	require.Len(t, results, 1)
	assert.Equal(t, 2, results[0].Line) // 1-indexed
	assert.Equal(t, "t:1.1", results[0].Target)
}

// --- GetContext ---

func TestOutputBuffer_GetContext_ReturnsLinesAround(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("t:1.1", "line1\nline2\nline3\nline4\nline5")

	ctx := ob.GetContext("t:1.1", 3, 1)
	require.Len(t, ctx, 3) // lines 2, 3, 4
}

func TestOutputBuffer_GetContext_ClampsAtBounds(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("t:1.1", "first\nsecond\nthird")

	// Context around line 1 with 5 context lines — should clamp to start
	ctx := ob.GetContext("t:1.1", 1, 5)
	require.True(t, len(ctx) >= 1)
	require.True(t, len(ctx) <= 3)
}

func TestOutputBuffer_GetContext_NonexistentTarget(t *testing.T) {
	ob := NewOutputBuffer(100)
	ctx := ob.GetContext("ghost:1.1", 1, 1)
	assert.Nil(t, ctx)
}

// --- SearchJSON ---

func TestOutputBuffer_SearchJSON_ReturnsMapSlice(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("t:1.1", "findme")

	results := ob.SearchJSON("findme", "t:1.1", 10)
	require.Len(t, results, 1)
	assert.Equal(t, "t:1.1", results[0]["target"])
	assert.Equal(t, "findme", results[0]["content"])
	assert.Equal(t, 1, results[0]["line"])
}

// --- ListAll ---

func TestOutputBuffer_ListAll_SingleTarget(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("t:1.1", "alpha\nbeta")

	all := ob.ListAll("t:1.1")
	require.Len(t, all, 2)
	assert.Equal(t, "alpha", all[0]["content"])
	assert.Equal(t, "beta", all[1]["content"])
}

func TestOutputBuffer_ListAll_AllTargets(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("a:1.1", "fromA")
	ob.Append("b:1.1", "fromB")

	all := ob.ListAll("")
	assert.Len(t, all, 2)
}

func TestOutputBuffer_ListAll_NonexistentTarget(t *testing.T) {
	ob := NewOutputBuffer(100)
	all := ob.ListAll("ghost:1.1")
	assert.Nil(t, all)
}

// --- Remove ---

func TestOutputBuffer_Remove_DeletesBuffer(t *testing.T) {
	ob := NewOutputBuffer(100)
	ob.Append("t:1.1", "data")
	ob.Remove("t:1.1")

	results := ob.Search("data", "t:1.1", 10)
	assert.Empty(t, results)
}

// --- stripAnsiCodes ---

func TestStripAnsiCodes_CSI(t *testing.T) {
	assert.Equal(t, "hello", stripAnsiCodes("\x1b[1mhello\x1b[0m"))
}

func TestStripAnsiCodes_OSC(t *testing.T) {
	assert.Equal(t, "text", stripAnsiCodes("\x1b]0;title\x07text"))
}

func TestStripAnsiCodes_NoSequences(t *testing.T) {
	assert.Equal(t, "plain text", stripAnsiCodes("plain text"))
}

func TestStripAnsiCodes_Color256(t *testing.T) {
	// ESC[38;5;196m  (256-color red)
	assert.Equal(t, "red", stripAnsiCodes("\x1b[38;5;196mred\x1b[0m"))
}

// --- Concurrent safety ---

func TestOutputBuffer_ConcurrentAccess(t *testing.T) {
	ob := NewOutputBuffer(100)
	var wg sync.WaitGroup

	// Concurrent writes to different targets
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ob.Append("t:1.1", "data from goroutine")
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ob.Search("data", "", 10)
		}()
	}

	wg.Wait()
	// No panic = success
}
