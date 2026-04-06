export type ChibiState = 'idle' | 'working' | 'needs_input' | 'error' | 'done';

const VARIANTS: Record<string, { body: string; accent: string; hair: string }> = {
  default: { body: '#89b4fa', accent: '#74c7ec', hair: '#313244' },
  pink:    { body: '#f5c2e7', accent: '#f38ba8', hair: '#45475a' },
  green:   { body: '#a6e3a1', accent: '#94e2d5', hair: '#313244' },
  peach:   { body: '#fab387', accent: '#f9e2af', hair: '#45475a' },
  purple:  { body: '#cba6f7', accent: '#b4befe', hair: '#313244' },
  red:     { body: '#f38ba8', accent: '#eba0ac', hair: '#45475a' },
};

const VARIANT_KEYS = Object.keys(VARIANTS);

export function variantFromName(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) hash = ((hash << 5) - hash + name.charCodeAt(i)) | 0;
  return VARIANT_KEYS[Math.abs(hash) % VARIANT_KEYS.length];
}

interface Props {
  state: ChibiState;
  variant?: string;
  size?: 'sm' | 'md' | 'lg';
}

const SIZES = { sm: 24, md: 40, lg: 64 };

export function ChibiAvatar({ state, variant = 'default', size = 'md' }: Props) {
  const v = VARIANTS[variant] || VARIANTS.default;
  const px = SIZES[size];

  return (
    <div className={`chibi-avatar chibi-${size} chibi-${state}`} style={{ width: px, height: px }}>
      <svg viewBox="0 0 32 32" width={px} height={px}>
        {/* Hair (back + long sides) */}
        <ellipse cx="16" cy="10" rx="9" ry="9" fill={v.hair} />
        {/* Long hair flowing down left */}
        <path d="M7 10 Q5 15 6 22 Q7 25 8 22 Q8 16 9 12Z" fill={v.hair} />
        {/* Long hair flowing down right */}
        <path d="M25 10 Q27 15 26 22 Q25 25 24 22 Q24 16 23 12Z" fill={v.hair} />

        {/* Face */}
        <circle cx="16" cy="11" r="7.5" fill="#f5e0dc" />

        {/* Hair (front bangs) */}
        <path d="M8.5 8 Q12 3 16 5 Q20 3 23.5 8 Q21 6 16 7 Q11 6 8.5 8Z" fill={v.hair} />
        {/* Side strands framing face */}
        <path d="M8.5 9 Q7.5 13 8 16" fill="none" stroke={v.hair} strokeWidth="2" strokeLinecap="round" />
        <path d="M23.5 9 Q24.5 13 24 16" fill="none" stroke={v.hair} strokeWidth="2" strokeLinecap="round" />

        {/* Eyes */}
        <g className="chibi-eyes">
          {state === 'error' ? (
            <>
              {/* X eyes */}
              <g stroke="#45475a" strokeWidth="1.2" strokeLinecap="round">
                <line x1="11" y1="9" x2="13.5" y2="11.5" />
                <line x1="13.5" y1="9" x2="11" y2="11.5" />
                <line x1="18.5" y1="9" x2="21" y2="11.5" />
                <line x1="21" y1="9" x2="18.5" y2="11.5" />
              </g>
            </>
          ) : (
            <>
              <ellipse cx="12.2" cy="10.5" rx="1.8" ry="2" fill="#313244" />
              <ellipse cx="19.8" cy="10.5" rx="1.8" ry="2" fill="#313244" />
              {/* Eye highlights */}
              <circle cx="12.8" cy="9.8" r="0.7" fill="#fff" />
              <circle cx="20.4" cy="9.8" r="0.7" fill="#fff" />
            </>
          )}
        </g>

        {/* Mouth */}
        {state === 'done' ? (
          <path d="M13.5 14 Q16 16.5 18.5 14" fill="none" stroke="#45475a" strokeWidth="0.8" strokeLinecap="round" />
        ) : state === 'error' ? (
          <path d="M13.5 15 Q16 13 18.5 15" fill="none" stroke="#45475a" strokeWidth="0.8" strokeLinecap="round" />
        ) : state === 'needs_input' ? (
          <circle cx="16" cy="14.5" rx="1.2" ry="1" fill="#45475a" />
        ) : (
          <path d="M14 14 Q16 15.5 18 14" fill="none" stroke="#45475a" strokeWidth="0.7" strokeLinecap="round" />
        )}

        {/* Blush */}
        <circle cx="10" cy="13" r="1.5" fill="#f5c2e7" opacity="0.4" />
        <circle cx="22" cy="13" r="1.5" fill="#f5c2e7" opacity="0.4" />

        {/* Body */}
        <g className="chibi-body">
          <path d={`M11 19 L10 27 L22 27 L21 19 Q16 17 11 19Z`} fill={v.body} />
          {/* Collar/accent */}
          <path d="M12 19 Q16 20.5 20 19" fill="none" stroke={v.accent} strokeWidth="1" />
        </g>

        {/* Arms */}
        <g className="chibi-arms">
          {state === 'done' ? (
            <>
              <line x1="10" y1="20" x2="6" y2="15" stroke={v.body} strokeWidth="2.5" strokeLinecap="round" />
              <line x1="22" y1="20" x2="26" y2="15" stroke={v.body} strokeWidth="2.5" strokeLinecap="round" />
            </>
          ) : state === 'needs_input' ? (
            <>
              <line x1="10" y1="20" x2="7" y2="25" stroke={v.body} strokeWidth="2.5" strokeLinecap="round" />
              <line x1="22" y1="20" x2="26" y2="16" stroke={v.body} strokeWidth="2.5" strokeLinecap="round" />
            </>
          ) : (
            <>
              <line x1="10" y1="20" x2="7" y2="25" stroke={v.body} strokeWidth="2.5" strokeLinecap="round" />
              <line x1="22" y1="20" x2="25" y2="25" stroke={v.body} strokeWidth="2.5" strokeLinecap="round" />
            </>
          )}
        </g>

        {/* Sparkles for done state */}
        {state === 'done' && (
          <g className="chibi-sparkles">
            <text x="4" y="10" fontSize="4" className="chibi-sparkle-1">*</text>
            <text x="26" y="8" fontSize="4" className="chibi-sparkle-2">*</text>
          </g>
        )}

        {/* Question mark for needs_input */}
        {state === 'needs_input' && (
          <text x="24" y="10" fontSize="6" fontWeight="bold" fill="#f9e2af" className="chibi-question">?</text>
        )}

        {/* Sweat drop for error */}
        {state === 'error' && (
          <path d="M24 7 Q25 9 24 10 Q23 9 24 7Z" fill="#89b4fa" className="chibi-sweat" />
        )}
      </svg>
    </div>
  );
}
