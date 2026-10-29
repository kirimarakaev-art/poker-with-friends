import React, { useState } from 'react';
import { User } from '../types';
import { api } from '../lib/api';
import { validatePassword, validateUsername } from '../lib/account';

interface AuthScreenProps {
  onAuth: (user: User) => void;
  notice?: string;
}

// AuthScreen is one card with two tabs: log in and sign up.
export const AuthScreen: React.FC<AuthScreenProps> = ({ onAuth, notice }) => {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const switchTo = (next: 'login' | 'register') => {
    setMode(next);
    setError(null);
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const name = username.trim();
    const problem = validateUsername(name) ?? validatePassword(password);
    if (problem) {
      setError(problem);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const user = mode === 'login' ? await api.login(name, password) : await api.register(name, password);
      onAuth(user);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Нет связи с сервером');
      setLoading(false);
    }
  };

  const isLogin = mode === 'login';
  return (
    <div className="app-container">
      <form className="lobby-card" onSubmit={submit}>
        <h1>♠ Покер с друзьями ♥</h1>
        <div className="auth-tabs" role="tablist">
          <button type="button" role="tab" aria-selected={isLogin} className={isLogin ? 'active' : ''} onClick={() => switchTo('login')}>
            Вход
          </button>
          <button type="button" role="tab" aria-selected={!isLogin} className={!isLogin ? 'active' : ''} onClick={() => switchTo('register')}>
            Регистрация
          </button>
        </div>

        {notice && !error && <div className="form-notice">{notice}</div>}
        {error && <div className="form-error">{error}</div>}

        <div className="form-group">
          <label htmlFor="auth-username">Логин</label>
          <input
            id="auth-username"
            type="text"
            className="form-control"
            value={username}
            onChange={e => setUsername(e.target.value)}
            autoComplete="username"
            maxLength={20}
            required
          />
          {!isLogin && <div className="form-hint">3–20 символов: буквы, цифры, _ - .</div>}
        </div>
        <div className="form-group">
          <label htmlFor="auth-password">Пароль</label>
          <input
            id="auth-password"
            type="password"
            className="form-control"
            value={password}
            onChange={e => setPassword(e.target.value)}
            autoComplete={isLogin ? 'current-password' : 'new-password'}
            required
          />
          {!isLogin && <div className="form-hint">Не короче 8 символов</div>}
        </div>
        <button className="btn" type="submit" disabled={loading}>
          {loading ? 'Подождите…' : isLogin ? 'Войти' : 'Создать аккаунт'}
        </button>
        {!isLogin && <p className="form-hint auth-gift">При регистрации вы получите 10 000 🪙</p>}
      </form>
    </div>
  );
};
