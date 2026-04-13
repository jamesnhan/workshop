import { useState, useRef, type ReactNode } from 'react';
import { HoverPreview } from './HoverPreview';
import { get } from '../api/client';

interface LinkPreview {
  title: string;
  description: string;
  favicon: string;
  finalUrl: string;
}

const previewCache = new Map<string, LinkPreview | null>();

const URL_REGEX = /https?:\/\/[^\s<>"{}|\\^`[\]]+/g;

/** Split text into segments of plain text and URLs. */
function splitUrls(text: string): { text: string; isUrl: boolean }[] {
  const parts: { text: string; isUrl: boolean }[] = [];
  let last = 0;
  for (const match of text.matchAll(URL_REGEX)) {
    if (match.index > last) parts.push({ text: text.slice(last, match.index), isUrl: false });
    parts.push({ text: match[0], isUrl: true });
    last = match.index + match[0].length;
  }
  if (last < text.length) parts.push({ text: text.slice(last), isUrl: false });
  return parts;
}

/** Render text with URLs as clickable links that show a preview on hover. */
export function Linkify({ children }: { children: string }): ReactNode {
  const [hover, setHover] = useState<{ url: string; x: number; y: number } | null>(null);
  const [preview, setPreview] = useState<LinkPreview | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const parts = splitUrls(children);
  if (parts.length === 1 && !parts[0].isUrl) return <>{children}</>;

  const onEnter = (url: string, e: React.MouseEvent) => {
    const rect = (e.target as HTMLElement).getBoundingClientRect();
    setHover({ url, x: rect.left, y: rect.bottom });
    // Fetch preview (cached)
    if (previewCache.has(url)) {
      setPreview(previewCache.get(url) ?? null);
    } else {
      timerRef.current = setTimeout(async () => {
        try {
          const data = await get<LinkPreview>(`/link-preview?url=${encodeURIComponent(url)}`);
          previewCache.set(url, data);
          setPreview(data);
        } catch {
          previewCache.set(url, null);
          setPreview(null);
        }
      }, 300);
    }
  };

  const onLeave = () => {
    if (timerRef.current) clearTimeout(timerRef.current);
    setHover(null);
    setPreview(null);
  };

  return (
    <>
      {parts.map((part, i) =>
        part.isUrl ? (
          <a
            key={i}
            href={part.text}
            target="_blank"
            rel="noopener noreferrer"
            className="linkified"
            onMouseEnter={(e) => onEnter(part.text, e)}
            onMouseLeave={onLeave}
          >
            {part.text}
          </a>
        ) : (
          <span key={i}>{part.text}</span>
        ),
      )}
      {hover && preview && preview.title && (
        <HoverPreview x={hover.x} y={hover.y} width={380} className="link-hover-preview">
          <div className="link-preview-header">
            {preview.favicon && <img src={preview.favicon} alt="" className="link-preview-favicon" />}
            <span className="link-preview-title">{preview.title}</span>
          </div>
          {preview.description && (
            <div className="link-preview-desc">{preview.description.slice(0, 200)}{preview.description.length > 200 ? '...' : ''}</div>
          )}
          <div className="link-preview-url">{preview.finalUrl || hover.url}</div>
        </HoverPreview>
      )}
    </>
  );
}
