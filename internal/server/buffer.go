package server

import (
	"strings"
	"sync"
)

// OutputBuffer stores terminal output per pane target for searching.
type OutputBuffer struct {
	mu       sync.RWMutex
	buffers  map[string]*ringBuffer
	maxLines int
}

type ringBuffer struct {
	mu       sync.RWMutex // per-buffer lock for concurrent access
	lines    []string     // clean rendered text for searching
	rawLines []string     // original with ANSI for display
	head     int
	count    int
	size     int
}

// SearchResult represents a single search match.
type SearchResult struct {
	Target  string `json:"target"`
	Line    int    `json:"line"`
	Content string `json:"content"`    // ANSI-stripped for matching/display
	Raw     string `json:"raw"`        // original with ANSI for rich rendering
}

func NewOutputBuffer(maxLinesPerPane int) *OutputBuffer {
	return &OutputBuffer{
		buffers:  make(map[string]*ringBuffer),
		maxLines: maxLinesPerPane,
	}
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		lines:    make([]string, size),
		rawLines: make([]string, size),
		size:     size,
	}
}

func (rb *ringBuffer) append(stripped, raw string) {
	rb.lines[rb.head] = stripped
	rb.rawLines[rb.head] = raw
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// Append adds raw terminal output to the buffer for a target.
// Stores both ANSI-stripped and raw versions per line.
func (ob *OutputBuffer) Append(target, data string) {
	rb := ob.getOrCreate(target)

	rb.mu.Lock()
	defer rb.mu.Unlock()

	for _, rawLine := range strings.Split(data, "\n") {
		rawLine = strings.TrimRight(rawLine, "\r")
		stripped := strings.TrimSpace(stripAnsiCodes(rawLine))
		if stripped != "" {
			rb.append(stripped, rawLine)
		}
	}
}

// UpdateFromCapture replaces the buffer for a target with the full scrollback
// from capture-pane -S -. The frontend fetches all lines at search-open time
// via /search/lines, so line numbers are stable within a search session.
func (ob *OutputBuffer) UpdateFromCapture(target, rendered string) {
	rb := ob.getOrCreate(target)

	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Reset and refill from the complete scrollback
	rb.head = 0
	rb.count = 0

	for _, line := range strings.Split(rendered, "\n") {
		cleaned := strings.TrimRight(line, " \t")
		stripped := strings.TrimSpace(stripAnsiCodes(cleaned))
		if stripped != "" {
			rb.append(stripped, cleaned)
		}
	}
}

func (ob *OutputBuffer) getOrCreate(target string) *ringBuffer {
	ob.mu.RLock()
	rb, ok := ob.buffers[target]
	ob.mu.RUnlock()

	if !ok {
		ob.mu.Lock()
		rb, ok = ob.buffers[target]
		if !ok {
			rb = newRingBuffer(ob.maxLines)
			ob.buffers[target] = rb
		}
		ob.mu.Unlock()
	}
	return rb
}

// maxSearchLimit prevents unbounded search requests.
const maxSearchLimit = 500

// Search finds lines containing the query substring (case-insensitive).
// If target is empty, searches all panes.
func (ob *OutputBuffer) Search(query, target string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 100
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	q := strings.ToLower(query)

	// Snapshot the buffer map under a short read lock
	ob.mu.RLock()
	var targets map[string]*ringBuffer
	if target != "" {
		if rb, ok := ob.buffers[target]; ok {
			targets = map[string]*ringBuffer{target: rb}
		}
	} else {
		targets = make(map[string]*ringBuffer, len(ob.buffers))
		for t, rb := range ob.buffers {
			targets[t] = rb
		}
	}
	ob.mu.RUnlock()

	var results []SearchResult

	for t, rb := range targets {
		// Per-buffer read lock — doesn't block Append on other panes
		rb.mu.RLock()
		// Iterate the ring buffer in-place (no copy)
		start := 0
		if rb.count == rb.size {
			start = rb.head
		}
		for i := 0; i < rb.count && len(results) < limit; i++ {
			idx := (start + i) % rb.size
			line := rb.lines[idx]
			if strings.Contains(strings.ToLower(line), q) {
				results = append(results, SearchResult{
					Target:  t,
					Line:    i + 1,
					Content: line,
					Raw:     rb.rawLines[idx],
				})
			}
		}
		rb.mu.RUnlock()
	}

	return results
}

// GetContext returns lines around a specific line number for a target.
func (ob *OutputBuffer) GetContext(target string, line, contextLines int) []string {
	ob.mu.RLock()
	rb, ok := ob.buffers[target]
	ob.mu.RUnlock()

	if !ok {
		return nil
	}

	rb.mu.RLock()
	defer rb.mu.RUnlock()

	// Compute the range within the ring buffer
	start := line - 1 - contextLines
	if start < 0 {
		start = 0
	}
	end := line - 1 + contextLines + 1
	if end > rb.count {
		end = rb.count
	}

	ringStart := 0
	if rb.count == rb.size {
		ringStart = rb.head
	}

	result := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		idx := (ringStart + i) % rb.size
		result = append(result, rb.rawLines[idx])
	}
	return result
}

// SearchJSON returns search results as generic maps (for the API layer).
func (ob *OutputBuffer) SearchJSON(query, target string, limit int) []map[string]any {
	results := ob.Search(query, target, limit)
	out := make([]map[string]any, len(results))
	for i, r := range results {
		out[i] = map[string]any{
			"target":  r.Target,
			"line":    r.Line,
			"content": r.Content,
			"raw":     r.Raw,
		}
	}
	return out
}

// ListAll returns all buffered lines, optionally filtered by target.
func (ob *OutputBuffer) ListAll(target string) []map[string]any {
	ob.mu.RLock()
	var targets map[string]*ringBuffer
	if target != "" {
		if rb, ok := ob.buffers[target]; ok {
			targets = map[string]*ringBuffer{target: rb}
		}
	} else {
		targets = make(map[string]*ringBuffer, len(ob.buffers))
		for t, rb := range ob.buffers {
			targets[t] = rb
		}
	}
	ob.mu.RUnlock()

	var out []map[string]any
	for t, rb := range targets {
		rb.mu.RLock()
		start := 0
		if rb.count == rb.size {
			start = rb.head
		}
		for i := 0; i < rb.count; i++ {
			idx := (start + i) % rb.size
			out = append(out, map[string]any{
				"target":  t,
				"line":    i + 1,
				"content": rb.lines[idx],
				"raw":     rb.rawLines[idx],
			})
		}
		rb.mu.RUnlock()
	}
	return out
}

// Remove deletes the buffer for a target.
func (ob *OutputBuffer) Remove(target string) {
	ob.mu.Lock()
	delete(ob.buffers, target)
	ob.mu.Unlock()
}

// stripAnsiCodes removes ANSI escape sequences from text.
func stripAnsiCodes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
					i++
				}
				if i < len(s) {
					i++
				}
			} else if i < len(s) && s[i] == ']' {
				// OSC sequence — skip until BEL or ST
				i++
				for i < len(s) && s[i] != '\x07' && s[i] != '\x1b' {
					i++
				}
				if i < len(s) && s[i] == '\x07' {
					i++
				}
			}
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}
