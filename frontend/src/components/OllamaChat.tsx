import { useState, useEffect, useRef, useCallback } from 'react';
import { get, post, put, del } from '../api/client';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

interface Model {
  name: string;
  size: number;
  endpoint: string;
  modifiedAt: string;
}

interface EndpointStatus {
  name: string;
  url: string;
  default?: boolean;
  online: boolean;
  error?: string;
}

interface Conversation {
  id: number;
  title: string;
  model: string;
  systemPrompt: string;
  createdAt: string;
  updatedAt: string;
}

interface DbMessage {
  id: number;
  conversationId: number;
  role: string;
  content: string;
  model: string;
  stats: string;
  createdAt: string;
}

interface ChatMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
  model?: string;
  stats?: string;
}

const MODEL_KEY = 'workshop:ollama-model';
const TARGET_WORDS_KEY = 'workshop:ollama-target-words';
const MAX_CONTINUE_ROUNDS = 10;
const CONTINUE_PROMPT = 'Continue writing seamlessly from where you left off. Do not repeat any previous text. Do not add a preamble or transition — pick up mid-sentence if needed.';

function countWords(text: string): number {
  return text.split(/\s+/).filter(Boolean).length;
}

function hasRepetitionLoop(text: string): boolean {
  if (text.length < 500) return false;
  const tail40 = text.slice(-40);
  const charCounts = new Map<string, number>();
  for (const c of tail40) charCounts.set(c, (charCounts.get(c) || 0) + 1);
  if (Math.max(...charCounts.values()) / 40 > 0.85) return true;
  const words = text.split(/\s+/).filter(Boolean);
  if (words.length < 60) return false;
  const tail60 = words.slice(-60);
  const unique = new Set(tail60.map(w => w.toLowerCase().replace(/[^a-z]/g, '')).filter(Boolean));
  if (unique.size < 6) return true;
  if (words.length >= 120) {
    const tail = words.slice(-120).map(w => w.toLowerCase().replace(/[^a-z]/g, ''));
    const trigrams = new Map<string, number>();
    for (let i = 0; i < tail.length - 2; i++) {
      const key = `${tail[i]} ${tail[i+1]} ${tail[i+2]}`;
      trigrams.set(key, (trigrams.get(key) || 0) + 1);
    }
    for (const count of trigrams.values()) {
      if (count >= 5) return true;
    }
  }
  return false;
}

function trimRepetitionTail(text: string): string {
  let trimmed = text;
  trimmed = trimmed.replace(/(.)\1{4,}$/u, '');
  trimmed = trimmed.replace(/(\b\w+\b[\s,]*)\1{3,}$/i, '');
  trimmed = trimmed.replace(/(.{2,40}?)\1{2,}$/u, '');
  const words = trimmed.split(/\s+/).filter(Boolean);
  if (words.length >= 60) {
    const tail = words.slice(-60);
    const uniq = new Set(tail.map(w => w.toLowerCase().replace(/[^a-z]/g, '')).filter(Boolean));
    if (uniq.size < 6) {
      for (let i = words.length - 20; i > words.length * 0.5; i -= 10) {
        const window = words.slice(i, i + 20);
        const uq = new Set(window.map(w => w.toLowerCase().replace(/[^a-z]/g, '')).filter(Boolean));
        if (uq.size >= 10) {
          trimmed = words.slice(0, i + 20).join(' ');
          break;
        }
      }
    }
  }
  const lastPunct = Math.max(
    trimmed.lastIndexOf('. '),
    trimmed.lastIndexOf('."'),
    trimmed.lastIndexOf('! '),
    trimmed.lastIndexOf('? '),
  );
  if (lastPunct > trimmed.length * 0.5) {
    trimmed = trimmed.slice(0, lastPunct + 1);
  }
  return trimmed.trimEnd();
}

function toChat(m: DbMessage): ChatMessage {
  return { role: m.role as ChatMessage['role'], content: m.content, model: m.model, stats: m.stats };
}

function timeAgo(dateStr: string): string {
  const d = new Date(dateStr);
  const now = Date.now();
  const sec = Math.floor((now - d.getTime()) / 1000);
  if (sec < 60) return 'just now';
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`;
  return `${Math.floor(sec / 86400)}d ago`;
}

export function OllamaChat() {
  const [models, setModels] = useState<Model[]>([]);
  const [endpoints, setEndpoints] = useState<EndpointStatus[]>([]);
  const [selectedModel, setSelectedModel] = useState(() => localStorage.getItem(MODEL_KEY) || '');
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeConvId, setActiveConvId] = useState<number | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [systemPrompt, setSystemPrompt] = useState('');
  const [showSystem, setShowSystem] = useState(false);
  const [showSidebar, setShowSidebar] = useState(true);
  const [generating, setGenerating] = useState(false);
  const [streamContent, setStreamContent] = useState('');
  const [error, setError] = useState('');
  const [targetWords, setTargetWords] = useState(() => {
    const saved = localStorage.getItem(TARGET_WORDS_KEY);
    return saved ? parseInt(saved, 10) : 0;
  });
  const [genProgress, setGenProgress] = useState('');
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, []);

  useEffect(() => { scrollToBottom(); }, [messages, streamContent, scrollToBottom]);

  // Load models, health, and conversations on mount
  useEffect(() => {
    get<Model[]>('/ollama/models').then(setModels).catch(() => {});
    get<EndpointStatus[]>('/ollama/health').then(setEndpoints).catch(() => {});
    loadConversations();
  }, []);

  const loadConversations = async () => {
    try {
      const convs = await get<Conversation[]>('/ollama/conversations');
      setConversations(convs || []);
    } catch { /* ignore */ }
  };

  // Auto-select first model if none chosen
  useEffect(() => {
    if (!selectedModel && models.length > 0) {
      const m = models[0].name;
      setSelectedModel(m);
      localStorage.setItem(MODEL_KEY, m);
    }
  }, [models, selectedModel]);

  // Load conversation messages when active conversation changes
  useEffect(() => {
    if (!activeConvId) {
      setMessages([]);
      setSystemPrompt('');
      return;
    }
    (async () => {
      try {
        const data = await get<{ conversation: Conversation; messages: DbMessage[] }>(`/ollama/conversations/${activeConvId}`);
        setMessages((data.messages || []).map(toChat));
        setSystemPrompt(data.conversation.systemPrompt || '');
        if (data.conversation.model) {
          setSelectedModel(data.conversation.model);
          localStorage.setItem(MODEL_KEY, data.conversation.model);
        }
      } catch {
        setMessages([]);
      }
    })();
  }, [activeConvId]);

  const newChat = () => {
    setActiveConvId(null);
    setMessages([]);
    setSystemPrompt('');
    setError('');
  };

  const selectConversation = (id: number) => {
    if (generating) return;
    setActiveConvId(id);
    setError('');
  };

  const deleteConversation = async (id: number) => {
    try {
      await del(`/ollama/conversations/${id}`);
      setConversations((prev) => prev.filter((c) => c.id !== id));
      if (activeConvId === id) newChat();
    } catch { /* ignore */ }
  };

  // Ensure a conversation exists (create if needed), return its ID
  const ensureConversation = async (firstUserMsg: string): Promise<number> => {
    if (activeConvId) {
      // Update model/system prompt on existing conversation
      await put(`/ollama/conversations/${activeConvId}`, {
        model: selectedModel,
        systemPrompt: systemPrompt.trim(),
      }).catch(() => {});
      return activeConvId;
    }
    // Create new conversation with auto-title from first message
    const title = firstUserMsg.slice(0, 80) || 'New Chat';
    const conv = await post<Conversation>('/ollama/conversations', {
      title,
      model: selectedModel,
      systemPrompt: systemPrompt.trim(),
    });
    setActiveConvId(conv.id);
    setConversations((prev) => [conv, ...prev]);
    return conv.id;
  };

  const saveMessage = async (convId: number, msg: ChatMessage) => {
    await post(`/ollama/conversations/${convId}/messages`, {
      role: msg.role,
      content: msg.content,
      model: msg.model || '',
      stats: msg.stats || '',
    }).catch(() => {});
  };

  const streamOnce = async (
    apiMessages: { role: string; content: string }[],
    contentSoFar: string,
    signal: AbortSignal,
  ): Promise<{ content: string; tokens: number; duration: number; doneReason: string }> => {
    const token = localStorage.getItem('workshop:api-key') || '';
    const hdrs: Record<string, string> = { 'Content-Type': 'application/json' };
    if (token) hdrs['Authorization'] = `Bearer ${token}`;
    const resp = await fetch('/api/v1/ollama/chat', {
      method: 'POST',
      headers: hdrs,
      body: JSON.stringify({
        model: selectedModel,
        messages: apiMessages,
        stream: true,
        think: false,
        options: { repeat_penalty: 1.2, temperature: 0.9 },
      }),
      signal,
    });

    if (!resp.ok) {
      const err = await resp.text();
      throw new Error(err);
    }

    const reader = resp.body?.getReader();
    if (!reader) throw new Error('No stream reader');

    const decoder = new TextDecoder();
    let chunkContent = '';
    let tokens = 0;
    let duration = 0;
    let doneReason = '';
    let buf = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buf += decoder.decode(value, { stream: true });
      const lines = buf.split('\n');
      buf = lines.pop() || '';

      for (const line of lines) {
        if (!line.trim()) continue;
        try {
          const chunk = JSON.parse(line);
          if (chunk.error) throw new Error(chunk.error);
          if (chunk.message?.content) {
            chunkContent += chunk.message.content;
            const combined = contentSoFar + chunkContent;
            setStreamContent(combined);
            if (hasRepetitionLoop(combined)) {
              reader.cancel();
              doneReason = 'repetition';
              return { content: chunkContent, tokens, duration, doneReason };
            }
          }
          if (chunk.done) {
            tokens = chunk.eval_count || 0;
            duration = chunk.eval_duration || 0;
            doneReason = chunk.done_reason || '';
          }
        } catch (e) {
          if (e instanceof Error && e.message !== line) throw e;
        }
      }
    }

    return { content: chunkContent, tokens, duration, doneReason };
  };

  const send = async () => {
    const text = input.trim();
    if (!text || generating) return;

    setInput('');
    setError('');
    const userMsg: ChatMessage = { role: 'user', content: text };
    const updated = [...messages, userMsg];
    setMessages(updated);
    setGenerating(true);
    setStreamContent('');
    setGenProgress('');

    const abort = new AbortController();
    abortRef.current = abort;
    let fullContent = '';
    let convId: number | null = null;

    try {
      convId = await ensureConversation(text);
      await saveMessage(convId, userMsg);

      const baseMessages: { role: string; content: string }[] = [];
      if (systemPrompt.trim()) {
        baseMessages.push({ role: 'system', content: systemPrompt.trim() });
      }
      for (const m of updated.slice(-20)) {
        baseMessages.push({ role: m.role, content: m.content });
      }

      let totalTokens = 0;
      let totalDuration = 0;
      let round = 0;

      const first = await streamOnce(baseMessages, '', abort.signal);
      fullContent = trimRepetitionTail(first.content);
      totalTokens = first.tokens;
      totalDuration = first.duration;
      round = 1;

      if (targetWords > 0) {
        let consecutiveEmpty = 0;
        while (
          round < MAX_CONTINUE_ROUNDS &&
          countWords(fullContent) < targetWords &&
          !abort.signal.aborted &&
          consecutiveEmpty < 2
        ) {
          const words = countWords(fullContent);
          setGenProgress(`${words} / ${targetWords} words (round ${round + 1})`);

          const contMessages: { role: string; content: string }[] = [];
          if (systemPrompt.trim()) {
            contMessages.push({ role: 'system', content: systemPrompt.trim() });
          }
          contMessages.push({ role: 'user', content: text });
          contMessages.push({ role: 'assistant', content: fullContent });
          contMessages.push({ role: 'user', content: CONTINUE_PROMPT });

          const cont = await streamOnce(contMessages, fullContent, abort.signal);
          totalTokens += cont.tokens;
          totalDuration += cont.duration;
          round++;

          const before = countWords(fullContent);
          fullContent = trimRepetitionTail(fullContent + (cont.content || ''));
          const after = countWords(fullContent);

          if (!cont.content.trim() || after - before < 20) {
            consecutiveEmpty++;
          } else {
            consecutiveEmpty = 0;
          }

          if (cont.doneReason === 'length') break;
        }
      }

      const tokPerSec = totalDuration > 0 ? totalTokens / (totalDuration / 1e9) : 0;
      const words = countWords(fullContent);
      let stats = `${totalTokens} tokens, ${tokPerSec.toFixed(1)} tok/s`;
      if (round > 1) stats += `, ${round} rounds, ${words} words`;

      fullContent = trimRepetitionTail(fullContent);

      const assistantMsg: ChatMessage = {
        role: 'assistant',
        content: fullContent,
        model: selectedModel,
        stats,
      };
      const final = [...updated, assistantMsg];
      setMessages(final);
      await saveMessage(convId, assistantMsg);
      loadConversations(); // Refresh sidebar (updated_at changed)
    } catch (e) {
      const isAbort = (e as Error).name === 'AbortError';
      if (!isAbort) {
        setError((e as Error).message);
      }
      // Preserve whatever was generated before abort/error
      if (fullContent.trim()) {
        fullContent = trimRepetitionTail(fullContent);
        const partialMsg: ChatMessage = {
          role: 'assistant',
          content: fullContent,
          model: selectedModel,
          stats: isAbort ? `stopped (${countWords(fullContent)} words)` : `error (${countWords(fullContent)} words)`,
        };
        const final = [...updated, partialMsg];
        setMessages(final);
        if (convId) {
          saveMessage(convId, partialMsg).catch(() => {});
          loadConversations();
        }
      }
    } finally {
      setGenerating(false);
      setStreamContent('');
      setGenProgress('');
      abortRef.current = null;
    }
  };

  const stop = () => {
    abortRef.current?.abort();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  };

  const onlineEndpoints = endpoints.filter((e) => e.online);
  const groupedModels = new Map<string, Model[]>();
  for (const m of models) {
    const arr = groupedModels.get(m.endpoint) || [];
    arr.push(m);
    groupedModels.set(m.endpoint, arr);
  }

  return (
    <div className="ollama-chat">
      {/* Conversation sidebar */}
      {showSidebar && (
        <div className="ollama-sidebar">
          <div className="ollama-sidebar-header">
            <button className="ollama-btn-small" onClick={newChat}>+ New Chat</button>
            <button className="ollama-btn-small" onClick={() => setShowSidebar(false)} title="Hide sidebar">
              &laquo;
            </button>
          </div>
          <div className="ollama-conv-list">
            {conversations.map((c) => (
              <div
                key={c.id}
                className={`ollama-conv-item ${c.id === activeConvId ? 'active' : ''}`}
                onClick={() => selectConversation(c.id)}
              >
                <div className="ollama-conv-title">{c.title}</div>
                <div className="ollama-conv-meta">
                  <span>{c.model ? c.model.split('/').pop()?.split(':')[0] : ''}</span>
                  <span>{timeAgo(c.updatedAt)}</span>
                </div>
                <button
                  className="ollama-conv-delete"
                  onClick={(e) => { e.stopPropagation(); deleteConversation(c.id); }}
                  title="Delete conversation"
                >
                  &times;
                </button>
              </div>
            ))}
            {conversations.length === 0 && (
              <div className="ollama-conv-empty">No conversations yet</div>
            )}
          </div>
        </div>
      )}

      {/* Main chat area */}
      <div className="ollama-main">
        <div className="ollama-header">
          <div className="ollama-header-row">
            {!showSidebar && (
              <button className="ollama-btn-small" onClick={() => setShowSidebar(true)} title="Show sidebar">
                &raquo;
              </button>
            )}
            <select
              className="ollama-model-select"
              value={selectedModel}
              onChange={(e) => { setSelectedModel(e.target.value); localStorage.setItem(MODEL_KEY, e.target.value); }}
            >
              {[...groupedModels.entries()].map(([ep, epModels]) => (
                <optgroup key={ep} label={`${ep} ${onlineEndpoints.find((e) => e.name === ep) ? '' : '(offline)'}`}>
                  {epModels.map((m) => (
                    <option key={`${ep}:${m.name}`} value={m.name}>
                      {m.name} ({(m.size / 1e9).toFixed(1)} GB)
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
            <div className="ollama-endpoints">
              {endpoints.map((ep) => (
                <span key={ep.name} className={`ollama-endpoint-badge ${ep.online ? 'online' : 'offline'}`} title={ep.error || ep.url}>
                  {ep.name}
                </span>
              ))}
            </div>
            <button className="ollama-btn-small" onClick={() => setShowSystem((s) => !s)} title="System prompt">
              {showSystem ? 'Hide System' : 'System'}
            </button>
          </div>
          {showSystem && (
            <div className="ollama-system-row">
              <textarea
                className="ollama-system-input"
                value={systemPrompt}
                onChange={(e) => setSystemPrompt(e.target.value)}
                placeholder="System prompt (optional) — sets the model's behavior for this conversation..."
                rows={3}
              />
              <div className="ollama-target-words">
                <label title="Target word count for auto-continue. Set to 0 to disable.">
                  Target words:
                  <input
                    type="number"
                    min={0}
                    step={500}
                    value={targetWords}
                    onChange={(e) => {
                      const v = Math.max(0, parseInt(e.target.value, 10) || 0);
                      setTargetWords(v);
                      localStorage.setItem(TARGET_WORDS_KEY, String(v));
                    }}
                    className="ollama-target-input"
                  />
                </label>
                {targetWords > 0 && <span className="ollama-target-hint">auto-continue until {targetWords} words</span>}
              </div>
            </div>
          )}
        </div>

        <div className="ollama-messages">
          {messages.length === 0 && !generating && (
            <div className="ollama-empty">
              <p>{activeConvId ? 'No messages in this conversation' : 'Start a new chat'}</p>
              <p className="muted">{models.length} models across {onlineEndpoints.length} endpoint{onlineEndpoints.length !== 1 ? 's' : ''}.</p>
            </div>
          )}
          {messages.map((msg, i) => (
            <div key={i} className={`ollama-msg ollama-msg-${msg.role}`}>
              <div className="ollama-msg-header">
                <span className="ollama-msg-role">{msg.role === 'user' ? 'You' : msg.model || 'Assistant'}</span>
                {msg.stats && <span className="ollama-msg-stats">{msg.stats}</span>}
              </div>
              <div className="ollama-msg-content">
                {msg.role === 'assistant' ? (
                  <Markdown remarkPlugins={[remarkGfm]}>{msg.content}</Markdown>
                ) : (
                  <p>{msg.content}</p>
                )}
              </div>
            </div>
          ))}
          {generating && streamContent && (
            <div className="ollama-msg ollama-msg-assistant">
              <div className="ollama-msg-header">
                <span className="ollama-msg-role">{selectedModel}</span>
                <span className="ollama-msg-stats generating">
                  {genProgress || 'generating...'}
                </span>
              </div>
              <div className="ollama-msg-content">
                <Markdown remarkPlugins={[remarkGfm]}>{streamContent}</Markdown>
              </div>
            </div>
          )}
          {generating && !streamContent && (
            <div className="ollama-msg ollama-msg-assistant">
              <div className="ollama-msg-header">
                <span className="ollama-msg-role">{selectedModel}</span>
              </div>
              <div className="ollama-msg-content ollama-thinking">Thinking...</div>
            </div>
          )}
          {error && <div className="ollama-error">{error}</div>}
          <div ref={messagesEndRef} />
        </div>

        <div className="ollama-input-bar">
          <textarea
            className="ollama-input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={generating ? 'Generating...' : `Message ${selectedModel || 'model'}...`}
            disabled={generating}
            rows={1}
          />
          {generating ? (
            <button className="ollama-send-btn stop" onClick={stop}>Stop</button>
          ) : (
            <button className="ollama-send-btn" onClick={send} disabled={!input.trim()}>Send</button>
          )}
        </div>
      </div>
    </div>
  );
}
