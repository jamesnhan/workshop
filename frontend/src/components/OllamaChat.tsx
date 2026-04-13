import { useState, useEffect, useRef, useCallback } from 'react';
import { get } from '../api/client';
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

interface ChatMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
  model?: string;
  stats?: string;
}

const HISTORY_KEY = 'workshop:ollama-history';
const MODEL_KEY = 'workshop:ollama-model';

function loadHistory(): ChatMessage[] {
  try { return JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]'); } catch { return []; }
}
function saveHistory(msgs: ChatMessage[]) {
  localStorage.setItem(HISTORY_KEY, JSON.stringify(msgs.slice(-200)));
}

export function OllamaChat() {
  const [models, setModels] = useState<Model[]>([]);
  const [endpoints, setEndpoints] = useState<EndpointStatus[]>([]);
  const [selectedModel, setSelectedModel] = useState(() => localStorage.getItem(MODEL_KEY) || '');
  const [messages, setMessages] = useState<ChatMessage[]>(loadHistory);
  const [input, setInput] = useState('');
  const [systemPrompt, setSystemPrompt] = useState('');
  const [showSystem, setShowSystem] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [streamContent, setStreamContent] = useState('');
  const [error, setError] = useState('');
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, []);

  useEffect(() => { scrollToBottom(); }, [messages, streamContent, scrollToBottom]);

  // Load models + health on mount
  useEffect(() => {
    get<Model[]>('/ollama/models').then(setModels).catch(() => {});
    get<EndpointStatus[]>('/ollama/health').then(setEndpoints).catch(() => {});
  }, []);

  // Auto-select first model if none chosen
  useEffect(() => {
    if (!selectedModel && models.length > 0) {
      const m = models[0].name;
      setSelectedModel(m);
      localStorage.setItem(MODEL_KEY, m);
    }
  }, [models, selectedModel]);

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

    const abort = new AbortController();
    abortRef.current = abort;

    try {
      const apiMessages: { role: string; content: string }[] = [];
      if (systemPrompt.trim()) {
        apiMessages.push({ role: 'system', content: systemPrompt.trim() });
      }
      // Include last 20 messages for context
      for (const m of updated.slice(-20)) {
        apiMessages.push({ role: m.role, content: m.content });
      }

      const resp = await fetch('/api/v1/ollama/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          model: selectedModel,
          messages: apiMessages,
          stream: true,
          think: false,
        }),
        signal: abort.signal,
      });

      if (!resp.ok) {
        const err = await resp.text();
        throw new Error(err);
      }

      const reader = resp.body?.getReader();
      if (!reader) throw new Error('No stream reader');

      const decoder = new TextDecoder();
      let fullContent = '';
      let stats = '';
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
              fullContent += chunk.message.content;
              setStreamContent(fullContent);
            }
            if (chunk.done && chunk.eval_count && chunk.eval_duration) {
              const tokPerSec = chunk.eval_count / (chunk.eval_duration / 1e9);
              stats = `${chunk.eval_count} tokens, ${tokPerSec.toFixed(1)} tok/s`;
            }
          } catch (e) {
            if (e instanceof Error && e.message !== line) throw e;
          }
        }
      }

      const assistantMsg: ChatMessage = {
        role: 'assistant',
        content: fullContent,
        model: selectedModel,
        stats,
      };
      const final = [...updated, assistantMsg];
      setMessages(final);
      saveHistory(final);
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        setError((e as Error).message);
      }
    } finally {
      setGenerating(false);
      setStreamContent('');
      abortRef.current = null;
    }
  };

  const stop = () => {
    abortRef.current?.abort();
  };

  const clearChat = () => {
    setMessages([]);
    localStorage.removeItem(HISTORY_KEY);
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
      <div className="ollama-header">
        <div className="ollama-header-row">
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
          <button className="ollama-btn-small" onClick={clearChat} title="Clear chat history">
            Clear
          </button>
        </div>
        {showSystem && (
          <textarea
            className="ollama-system-input"
            value={systemPrompt}
            onChange={(e) => setSystemPrompt(e.target.value)}
            placeholder="System prompt (optional) — sets the model's behavior for this conversation..."
            rows={3}
          />
        )}
      </div>

      <div className="ollama-messages">
        {messages.length === 0 && !generating && (
          <div className="ollama-empty">
            <p>No messages yet</p>
            <p className="muted">Pick a model and start chatting. {models.length} models across {onlineEndpoints.length} endpoint{onlineEndpoints.length !== 1 ? 's' : ''}.</p>
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
              <span className="ollama-msg-stats generating">generating...</span>
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
  );
}
