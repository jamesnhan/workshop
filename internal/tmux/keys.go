package tmux

import "fmt"

// escapeToTmuxKey maps xterm.js escape sequences to tmux key names.
var escapeToTmuxKey = map[string]string{
	// Arrow keys
	"\x1b[A": "Up",
	"\x1b[B": "Down",
	"\x1b[C": "Right",
	"\x1b[D": "Left",

	// Ctrl+Arrow
	"\x1b[1;5A": "C-Up",
	"\x1b[1;5B": "C-Down",
	"\x1b[1;5C": "C-Right",
	"\x1b[1;5D": "C-Left",

	// Alt+Arrow
	"\x1b[1;3A": "M-Up",
	"\x1b[1;3B": "M-Down",
	"\x1b[1;3C": "M-Right",
	"\x1b[1;3D": "M-Left",

	// Shift+Arrow
	"\x1b[1;2A": "S-Up",
	"\x1b[1;2B": "S-Down",
	"\x1b[1;2C": "S-Right",
	"\x1b[1;2D": "S-Left",

	// Home/End
	"\x1b[H":    "Home",
	"\x1b[F":    "End",
	"\x1bOH":    "Home",
	"\x1bOF":    "End",
	"\x1b[1;5H": "C-Home",
	"\x1b[1;5F": "C-End",

	// Page Up/Down
	"\x1b[5~": "PageUp",
	"\x1b[6~": "PageDown",

	// Insert/Delete
	"\x1b[2~": "IC", // Insert
	"\x1b[3~": "DC", // Delete

	// Function keys
	"\x1bOP":   "F1",
	"\x1bOQ":   "F2",
	"\x1bOR":   "F3",
	"\x1bOS":   "F4",
	"\x1b[15~": "F5",
	"\x1b[17~": "F6",
	"\x1b[18~": "F7",
	"\x1b[19~": "F8",
	"\x1b[20~": "F9",
	"\x1b[21~": "F10",
	"\x1b[23~": "F11",
	"\x1b[24~": "F12",

	// Shift+Tab (backtab)
	"\x1b[Z": "BTab",

	// Alt+Backspace
	"\x1b\x7f": "M-BSpace",
}

// controlToTmuxKey maps single control characters to tmux key names.
var controlToTmuxKey = map[byte]string{
	'\x09': "Tab",
	'\x0d': "Enter",
	'\x1b': "Escape",
	'\x7f': "BSpace",
}

// SendInput sends xterm.js terminal input to a tmux pane, translating
// escape sequences to tmux key names for reliable delivery.
func (b *ExecBridge) SendInput(target, data string) error {
	// Try to match the entire input as a known escape sequence
	if key, ok := escapeToTmuxKey[data]; ok {
		if out, err := b.run("send-keys", "-t", target, key); err != nil {
			return fmt.Errorf("send-keys %s: %w: %s", key, err, out)
		}
		return nil
	}

	// Single byte
	if len(data) == 1 {
		ch := data[0]

		// Named control keys
		if key, ok := controlToTmuxKey[ch]; ok {
			if out, err := b.run("send-keys", "-t", target, key); err != nil {
				return fmt.Errorf("send-keys %s: %w: %s", key, err, out)
			}
			return nil
		}

		// Ctrl+A through Ctrl+Z (0x01-0x1a)
		if ch >= 0x01 && ch <= 0x1a {
			key := "C-" + string(rune('a'+ch-1))
			if out, err := b.run("send-keys", "-t", target, key); err != nil {
				return fmt.Errorf("send-keys %s: %w: %s", key, err, out)
			}
			return nil
		}

		// Regular printable character — send literal
		if out, err := b.run("send-keys", "-t", target, "-l", data); err != nil {
			return fmt.Errorf("send-keys -l: %w: %s", err, out)
		}
		return nil
	}

	// Multi-char: unknown escape sequence or pasted text
	if data[0] == '\x1b' {
		// Unknown escape — send via hex as fallback
		hexStr := fmt.Sprintf("%x", []byte(data))
		return b.SendKeysHex(target, hexStr)
	}

	// Pasted text or fast typing — send literal
	if out, err := b.run("send-keys", "-t", target, "-l", data); err != nil {
		return fmt.Errorf("send-keys -l: %w: %s", err, out)
	}
	return nil
}
