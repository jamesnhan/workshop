import type { Notification } from '../hooks/useNotifications';

interface Props {
  notifications: Notification[];
  onClickNotification: (target: string) => void;
  onDismiss: (id: string) => void;
  onClearAll: () => void;
  onClose: () => void;
}

function timeAgo(ts: number): string {
  const diff = Math.floor((Date.now() - ts) / 1000);
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  return `${Math.floor(diff / 3600)}h ago`;
}

const typeIcons: Record<string, string> = {
  'input-needed': '⏳',
  'error': '🔴',
  'complete': '✅',
  'info': 'ℹ️',
};

export function NotificationPanel({ notifications, onClickNotification, onDismiss, onClearAll, onClose }: Props) {
  return (
    <div className="notif-panel">
      <div className="notif-header">
        <span>Notifications</span>
        <div className="notif-actions">
          {notifications.length > 0 && (
            <button className="btn-notif-action" onClick={onClearAll}>Clear all</button>
          )}
          <button className="search-close" onClick={onClose}>x</button>
        </div>
      </div>
      <div className="notif-list">
        {notifications.length === 0 && (
          <div className="notif-empty">No notifications</div>
        )}
        {notifications.map((n) => (
          <div
            key={n.id}
            className={`notif-item${n.read ? '' : ' unread'}`}
            onClick={() => onClickNotification(n.target)}
          >
            <span className="notif-icon">{typeIcons[n.type] || '🔔'}</span>
            <div className="notif-content">
              <div className="notif-message">{n.message}</div>
              <div className="notif-meta">
                <span className="notif-target">{n.target}</span>
                <span className="notif-time">{timeAgo(n.timestamp)}</span>
              </div>
            </div>
            <button
              className="notif-dismiss"
              onClick={(e) => { e.stopPropagation(); onDismiss(n.id); }}
            >
              x
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
