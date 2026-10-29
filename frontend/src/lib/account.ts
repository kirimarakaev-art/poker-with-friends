// Account rules and server error texts, mirroring docs/API.md ("Аккаунты", "Столы").

// Letters (any alphabet), digits, _ - . — 3 to 20 of them.
const USERNAME_RE = /^[\p{L}\p{N}_.-]{3,20}$/u;
const PASSWORD_MIN = 8;
const PASSWORD_MAX_BYTES = 72;

export function validateUsername(name: string): string | null {
  return USERNAME_RE.test(name) ? null : 'Логин: от 3 до 20 символов — буквы, цифры, _ - .';
}

export function validatePassword(password: string): string | null {
  if ([...password].length < PASSWORD_MIN) return `Пароль: не короче ${PASSWORD_MIN} символов`;
  if (new TextEncoder().encode(password).length > PASSWORD_MAX_BYTES) return 'Пароль слишком длинный';
  return null;
}

// Known server errors, matched by the start of the text.
const MESSAGES: [string, string][] = [
  ['username must be', 'Логин: от 3 до 20 символов — буквы, цифры, _ - .'],
  ['password must be', 'Пароль: от 8 символов, но не слишком длинный'],
  ['username is taken', 'Этот логин уже занят'],
  ['wrong username or password', 'Неверный логин или пароль'],
  ['log in first', 'Войдите в аккаунт'],
  ['bonus is not available', 'Бонус сейчас недоступен'],
  ['too many attempts', 'Слишком много попыток. Подождите минуту'],
  ['need 0 <', 'Неверные параметры: нужно 0 < малый блайнд ≤ большой блайнд ≤ мин. бай-ин ≤ макс. бай-ин ≤ 1 000 000'],
  ['buy-in must be', 'Бай-ин должен быть в пределах стола'],
  ['room not found', 'Комната не найдена'],
  ['not enough chips', 'Не хватает фишек'],
  ['table is full', 'За столом нет мест'],
];

// apiMessage turns a failed API call into text for the player; status 0 means no network.
export function apiMessage(status: number, error: string): string {
  const known = MESSAGES.find(([prefix]) => error.startsWith(prefix));
  if (known) return known[1];
  switch (status) {
    case 0:
      return 'Нет связи с сервером';
    case 401:
      return 'Войдите в аккаунт';
    case 404:
      return 'Комната не найдена';
    case 429:
      return 'Слишком много попыток. Подождите минуту';
  }
  return `Ошибка сервера (${status})`;
}
