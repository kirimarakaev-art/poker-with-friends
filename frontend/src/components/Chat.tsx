import React, { useEffect, useRef, useState } from 'react';
import { ChatMessage } from '../types';

interface ChatProps {
  messages: ChatMessage[];
  onSendMessage: (text: string) => void;
}

export const Chat: React.FC<ChatProps> = ({ messages, onSendMessage }) => {
  const [chatText, setChatText] = useState('');
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const list = listRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [messages.length]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const text = chatText.trim();
    if (!text) return;
    onSendMessage(text);
    setChatText('');
  };

  return (
    <section className="chat-container" aria-label="Чат">
      <div className="chat-messages" ref={listRef}>
        {messages.length === 0 && <div className="chat-empty">Сообщений пока нет</div>}
        {messages.map((m, i) => (
          <div key={i} className="chat-msg">
            <span>{m.sender}:</span> {m.text}
          </div>
        ))}
      </div>
      <form onSubmit={handleSubmit} className="chat-input-row">
        <input
          type="text"
          className="form-control chat-input"
          value={chatText}
          onChange={e => setChatText(e.target.value)}
          placeholder="Сообщение…"
          maxLength={200}
          aria-label="Сообщение в чат"
        />
        <button type="submit" className="btn chat-send" aria-label="Отправить">
          ➤
        </button>
      </form>
    </section>
  );
};
