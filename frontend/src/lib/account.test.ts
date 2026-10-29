import { describe, expect, test } from 'vitest';
import { apiMessage, validatePassword, validateUsername } from './account';
import { buyInBounds, clampBuyIn, normalizeRoomCode, validateRoomSettings } from './rooms';

describe('validateUsername', () => {
  test.each(['Kerim', 'abc', 'Керим_1', 'a.b-c_d', 'x'.repeat(20), 'Ёжик'])('%j is fine', name => {
    expect(validateUsername(name)).toBeNull();
  });

  test.each(['', 'ab', 'x'.repeat(21), 'with space', 'bad!', 'emoji🙂ok', 'a/b'])('%j is rejected', name => {
    expect(validateUsername(name)).toContain('Логин');
  });

  test('counts letters, not UTF-16 units', () => {
    expect(validateUsername('Ж'.repeat(20))).toBeNull();
  });
});

describe('validatePassword', () => {
  test('needs at least 8 characters', () => {
    expect(validatePassword('1234567')).toContain('8');
    expect(validatePassword('12345678')).toBeNull();
  });

  test('allows at most 72 bytes', () => {
    expect(validatePassword('x'.repeat(72))).toBeNull();
    expect(validatePassword('x'.repeat(73))).toContain('длинный');
    // Cyrillic letters take 2 bytes each in UTF-8.
    expect(validatePassword('ж'.repeat(36))).toBeNull();
    expect(validatePassword('ж'.repeat(37))).toContain('длинный');
  });
});

describe('apiMessage', () => {
  test.each([
    [400, 'username must be 3 to 20 characters: letters, digits, dots, dashes, underscores', 'Логин'],
    [400, 'password must be 8 to 72 bytes', 'Пароль'],
    [409, 'username is taken', 'Этот логин уже занят'],
    [401, 'wrong username or password', 'Неверный логин или пароль'],
    [401, 'log in first', 'Войдите'],
    [409, 'bonus is not available', 'Бонус'],
    [429, 'too many attempts, try again in a minute', 'Подождите минуту'],
    [400, 'need 0 < smallBlind <= bigBlind <= minBuyIn <= maxBuyIn <= 1000000', 'блайнд'],
    [400, 'buy-in must be minBuyIn to maxBuyIn', 'Бай-ин'],
    [404, 'room not found', 'Комната не найдена'],
    [409, 'not enough chips', 'Не хватает фишек'],
    [409, 'table is full', 'За столом нет мест'],
  ])('%i %s', (status, error, want) => {
    expect(apiMessage(status, error)).toContain(want);
  });

  test('falls back to the status', () => {
    expect(apiMessage(404, '')).toBe('Комната не найдена');
    expect(apiMessage(429, '')).toContain('Подождите');
    expect(apiMessage(500, 'boom')).toBe('Ошибка сервера (500)');
    expect(apiMessage(0, '')).toBe('Нет связи с сервером');
  });
});

describe('buyInBounds', () => {
  const room = { minBuyIn: 200, maxBuyIn: 1000 };

  test('rich player: the whole range, max by default', () => {
    expect(buyInBounds(room, 10_000)).toEqual({ min: 200, max: 1000, initial: 1000, canAfford: true });
  });

  test('balance caps the range', () => {
    expect(buyInBounds(room, 600)).toEqual({ min: 200, max: 600, initial: 600, canAfford: true });
    expect(buyInBounds(room, 200)).toEqual({ min: 200, max: 200, initial: 200, canAfford: true });
  });

  test('not enough chips', () => {
    expect(buyInBounds(room, 199).canAfford).toBe(false);
    expect(buyInBounds(room, 0).canAfford).toBe(false);
  });

  test('clampBuyIn keeps whole numbers inside the bounds', () => {
    const b = buyInBounds(room, 600);
    expect(clampBuyIn(50, b)).toBe(200);
    expect(clampBuyIn(5000, b)).toBe(600);
    expect(clampBuyIn(333.7, b)).toBe(333);
    expect(clampBuyIn(NaN, b)).toBe(200);
  });
});

describe('validateRoomSettings', () => {
  const ok = { smallBlind: 5, bigBlind: 10, minBuyIn: 200, maxBuyIn: 1000 };

  test('accepts the defaults and equal values', () => {
    expect(validateRoomSettings(ok)).toBeNull();
    expect(validateRoomSettings({ smallBlind: 10, bigBlind: 10, minBuyIn: 10, maxBuyIn: 10 })).toBeNull();
  });

  test.each([
    { smallBlind: 0 },
    { bigBlind: 4 },
    { minBuyIn: 5 },
    { maxBuyIn: 100 },
    { maxBuyIn: 1_000_001 },
    { smallBlind: 2.5 },
    { bigBlind: NaN },
  ])('rejects %o', over => {
    expect(validateRoomSettings({ ...ok, ...over })).not.toBeNull();
  });
});

describe('normalizeRoomCode', () => {
  test.each([
    ['k7mq2x', 'K7MQ2X'],
    [' K7MQ2X ', 'K7MQ2X'],
  ])('%j → %s', (input, want) => {
    expect(normalizeRoomCode(input)).toBe(want);
  });

  test.each(['', 'K7MQ2', 'K7MQ2XY', 'K7MQ0X', 'K7MQIX', 'K7-Q2X'])('%j is not a code', input => {
    expect(normalizeRoomCode(input)).toBeNull();
  });
});
