import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

// First test in the repo — proves Vitest + RTL + jsdom are wired up.
describe('frontend test infra smoke', () => {
  it('renders a component and matches text', () => {
    render(<div>hello workshop</div>);
    expect(screen.getByText('hello workshop')).toBeInTheDocument();
  });

  it('persists to localStorage within a test', () => {
    localStorage.setItem('workshop:smoke', 'ok');
    expect(localStorage.getItem('workshop:smoke')).toBe('ok');
  });
});
