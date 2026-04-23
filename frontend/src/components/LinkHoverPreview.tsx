import { useEffect, useState } from 'react';
import { get } from '../api/client';
import { HoverPreview } from './HoverPreview';

interface LinkPreview {
  title: string;
  description: string;
  favicon: string;
  finalUrl: string;
}

interface Props {
  url: string;
  x: number;
  y: number;
  pinned?: boolean;
}

const previewCache = new Map<string, LinkPreview | null>();

export function LinkHoverPreview({ url, x, y, pinned }: Props) {
  const [preview, setPreview] = useState<LinkPreview | null | undefined>(previewCache.get(url));

  useEffect(() => {
    if (previewCache.has(url)) {
      setPreview(previewCache.get(url));
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      get<LinkPreview>(`/link-preview?url=${encodeURIComponent(url)}`)
        .then((p) => {
          previewCache.set(url, p);
          if (!cancelled) setPreview(p);
        })
        .catch(() => {
          previewCache.set(url, null);
          if (!cancelled) setPreview(null);
        });
    }, 200);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [url]);

  if (!preview?.title) return null;

  return (
    <HoverPreview x={x} y={y} width={380} className={`link-hover-preview${pinned ? ' hover-pinned-inline' : ''}`}>
      <div className="link-preview-header">
        {preview.favicon && <img src={preview.favicon} alt="" className="link-preview-favicon" />}
        <span className="link-preview-title">{preview.title}</span>
      </div>
      {preview.description && (
        <div className="link-preview-desc">
          {preview.description.slice(0, 200)}{preview.description.length > 200 ? '...' : ''}
        </div>
      )}
      <div className="link-preview-url">{preview.finalUrl || url}</div>
    </HoverPreview>
  );
}
