import { useState, useCallback, type RefObject } from 'react';

export interface AutocompleteCard {
  id: number;
  title: string;
  column: string;
  priority: string;
  cardType: string;
}

interface UseTicketAutocompleteOptions {
  inputRef: RefObject<HTMLInputElement | HTMLTextAreaElement | null>;
  value: string;
  onChange: (value: string) => void;
  cards: AutocompleteCard[];
  enabled?: boolean;
}

// Finds a `#query` trigger ending at the cursor position.
// Returns the query string (after #) or null if no trigger is active.
function getActiveTrigger(value: string, cursorPos: number): { query: string; triggerStart: number } | null {
  const textToCursor = value.slice(0, cursorPos);
  // Look for the last # that starts a word (preceded by space/start/newline)
  const match = textToCursor.match(/(^|[\s\n])(#(\w*))$/);
  if (!match) return null;
  const query = match[3]; // text after #
  const triggerStart = textToCursor.length - match[2].length; // position of #
  return { query, triggerStart };
}

export function useTicketAutocomplete({ inputRef, value, onChange, cards, enabled = true }: UseTicketAutocompleteOptions) {
  const [suggestions, setSuggestions] = useState<AutocompleteCard[]>([]);
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [triggerStart, setTriggerStart] = useState<number | null>(null);

  const dismiss = useCallback(() => {
    setSuggestions([]);
    setSelectedIdx(0);
    setTriggerStart(null);
  }, []);

  const accept = useCallback((card: AutocompleteCard) => {
    if (triggerStart === null || !inputRef.current) return;
    const cursorPos = inputRef.current.selectionStart ?? value.length;
    // Replace from triggerStart (#) to cursor with #id
    const before = value.slice(0, triggerStart);
    const after = value.slice(cursorPos);
    const insertion = `#${card.id} `;
    const newValue = before + insertion + after;
    onChange(newValue);
    dismiss();
    // Move cursor after insertion
    requestAnimationFrame(() => {
      if (inputRef.current) {
        const pos = triggerStart + insertion.length;
        inputRef.current.setSelectionRange(pos, pos);
        inputRef.current.focus();
      }
    });
  }, [triggerStart, value, onChange, dismiss, inputRef]);

  const handleChange = useCallback((e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    const newValue = e.target.value;
    const cursorPos = e.target.selectionStart ?? newValue.length;
    onChange(newValue);

    if (!enabled) return;

    const trigger = getActiveTrigger(newValue, cursorPos);
    if (!trigger) {
      dismiss();
      return;
    }

    setTriggerStart(trigger.triggerStart);
    const q = trigger.query.toLowerCase();
    const filtered = cards
      .filter((c) => {
        if (!q) return true; // show first N on bare #
        return String(c.id).startsWith(q) || c.title.toLowerCase().includes(q);
      })
      .slice(0, 8);
    setSuggestions(filtered);
    setSelectedIdx(0);
  }, [cards, onChange, dismiss]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    if (suggestions.length === 0) return;

    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSelectedIdx((i) => Math.min(i + 1, suggestions.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSelectedIdx((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Tab' || e.key === 'Enter') {
      if (suggestions[selectedIdx]) {
        e.preventDefault();
        accept(suggestions[selectedIdx]);
      }
    } else if (e.key === 'Escape') {
      e.preventDefault();
      dismiss();
    }
  }, [suggestions, selectedIdx, accept, dismiss]);

  const showDropdown = suggestions.length > 0;

  return { showDropdown, suggestions, selectedIdx, handleChange, handleKeyDown, accept, dismiss };
}
