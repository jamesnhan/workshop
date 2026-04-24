import { describe, it, expect, beforeEach } from 'vitest';
import { recordBreadcrumb, readStaleBreadcrumbs, timeBreadcrumb } from './telemetry';

describe('breadcrumbs', () => {
  beforeEach(() => {
    localStorage.removeItem('workshop:watchdog-breadcrumbs');
    // Drain the module-scope ring by overwriting it 50x with distinct marker
    for (let i = 0; i < 50; i++) recordBreadcrumb(`__flush_${i}__`);
    localStorage.removeItem('workshop:watchdog-breadcrumbs');
  });

  it('persists each breadcrumb to localStorage synchronously', () => {
    recordBreadcrumb('test.event', { key: 'value' }, 42);
    const stored = JSON.parse(localStorage.getItem('workshop:watchdog-breadcrumbs') || '[]');
    const last = stored[stored.length - 1];
    expect(last.name).toBe('test.event');
    expect(last.meta).toEqual({ key: 'value' });
    expect(last.ms).toBe(42);
    expect(typeof last.ts).toBe('number');
  });

  it('rolls over at MAX_BREADCRUMBS (oldest dropped first)', () => {
    for (let i = 0; i < 60; i++) recordBreadcrumb(`evt.${i}`);
    const stored = JSON.parse(localStorage.getItem('workshop:watchdog-breadcrumbs') || '[]');
    expect(stored).toHaveLength(50);
    // First 10 should have been evicted; buffer now holds evt.10 .. evt.59
    expect(stored[0].name).toBe('evt.10');
    expect(stored[stored.length - 1].name).toBe('evt.59');
  });

  it('timeBreadcrumb records duration and preserves return value', () => {
    const result = timeBreadcrumb('work', () => {
      // small synchronous work
      let sum = 0;
      for (let i = 0; i < 1000; i++) sum += i;
      return sum;
    });
    expect(result).toBe(499500);
    const stored = JSON.parse(localStorage.getItem('workshop:watchdog-breadcrumbs') || '[]');
    const last = stored[stored.length - 1];
    expect(last.name).toBe('work');
    expect(typeof last.ms).toBe('number');
    expect(last.ms).toBeGreaterThanOrEqual(0);
  });

  it('timeBreadcrumb still records on throw, flagged with threw:true', () => {
    expect(() => timeBreadcrumb('boom', () => { throw new Error('nope'); })).toThrow('nope');
    const stored = JSON.parse(localStorage.getItem('workshop:watchdog-breadcrumbs') || '[]');
    const last = stored[stored.length - 1];
    expect(last.name).toBe('boom');
    expect(last.meta?.threw).toBe(true);
  });

  it('readStaleBreadcrumbs returns [] when key is missing', () => {
    localStorage.removeItem('workshop:watchdog-breadcrumbs');
    expect(readStaleBreadcrumbs()).toEqual([]);
  });

  it('readStaleBreadcrumbs parses a stored array', () => {
    localStorage.setItem('workshop:watchdog-breadcrumbs', JSON.stringify([{ ts: 1, name: 'a' }]));
    const result = readStaleBreadcrumbs();
    expect(result).toEqual([{ ts: 1, name: 'a' }]);
  });

  it('readStaleBreadcrumbs returns [] on malformed JSON', () => {
    localStorage.setItem('workshop:watchdog-breadcrumbs', 'not-json');
    expect(readStaleBreadcrumbs()).toEqual([]);
  });
});
