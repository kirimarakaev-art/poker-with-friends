import { describe, expect, test } from 'vitest';
import { parseCard } from './cards';
import { closeOutcome, reconnectDelay } from './connection';
import { seatPoint, stepToward, sideStep, clearDistance, isWatching } from './seats';
import { diffStates } from './animations';
import { adminMessage, parseStack } from './admin';
import { PlayerState, TableState } from '../types';

describe('parseCard', () => {
  test.each([
    ['A♠', 'A', '♠', false],
    ['T♥', '10', '♥', true],
    ['2♦', '2', '♦', true],
    ['K♣', 'K', '♣', false],
  ])('%s', (input, rank, suit, isRed) => {
    expect(parseCard(input)).toEqual({ rank, suit, isRed });
  });

  test('rejects malformed cards', () => {
    expect(parseCard('')).toBeNull();
    expect(parseCard('A')).toBeNull();
    expect(parseCard('X♠')).toBeNull();
    expect(parseCard('A?')).toBeNull();
  });
});

describe('closeOutcome', () => {
  test.each([
    [4001, 'replaced'],
    [4002, 'left'],
    [1008, 'token-invalid'],
    [4004, 'room-gone'],
    [1006, 'reconnect'],
    [1000, 'reconnect'],
    [1001, 'reconnect'],
  ] as const)('code %i → %s', (code, want) => {
    expect(closeOutcome(code)).toBe(want);
  });

  test('reconnect delays grow and stop at 10 s', () => {
    expect([0, 1, 2, 3, 4, 50].map(reconnectDelay)).toEqual([1000, 2000, 5000, 10000, 10000, 10000]);
  });
});

describe('seatPoint', () => {
  test('you always sit at the bottom centre', () => {
    for (const total of [2, 3, 6, 9]) {
      for (let you = 0; you < total; you++) {
        const p = seatPoint(you, total, you);
        expect(p.x).toBeCloseTo(0.5);
        expect(p.y).toBeCloseTo(1);
      }
    }
  });

  test('heads-up opponent sits at the top', () => {
    const p = seatPoint(1, 2, 0);
    expect(p.x).toBeCloseTo(0.5);
    expect(p.y).toBeCloseTo(0);
  });

  test('seats lie on the rail and never share a spot', () => {
    for (const total of [2, 3, 6, 9]) {
      const points = Array.from({ length: total }, (_, s) => seatPoint(s, total, 0));
      for (const p of points) {
        expect((p.x - 0.5) ** 2 / 0.25 + (p.y - 0.5) ** 2 / 0.25).toBeCloseTo(1);
      }
      const keys = new Set(points.map(p => `${p.x.toFixed(3)},${p.y.toFixed(3)}`));
      expect(keys.size).toBe(total);
    }
  });

  test('stepToward walks a fixed distance and never overshoots', () => {
    expect(stepToward({ x: 0, y: 100 }, { x: 0, y: 0 }, 30)).toEqual({ x: 0, y: 70 });
    expect(stepToward({ x: 0, y: 0 }, { x: 40, y: 30 }, 25)).toEqual({ x: 20, y: 15 });
    expect(stepToward({ x: 0, y: 0 }, { x: 10, y: 0 }, 99)).toEqual({ x: 10, y: 0 });
    expect(stepToward({ x: 5, y: 5 }, { x: 5, y: 5 }, 10)).toEqual({ x: 5, y: 5 });
  });

  test('clearDistance leaves the item outside the box in any direction', () => {
    const box = { hw: 60, hh: 40 };
    const item = { hw: 20, hh: 10 };
    expect(clearDistance({ x: 0, y: 0 }, { x: 0, y: -100 }, box, item, 5)).toBeCloseTo(40 + 10 + 5);
    expect(clearDistance({ x: 0, y: 0 }, { x: 100, y: 0 }, box, item, 5)).toBeCloseTo(60 + 20 + 5);
    for (const angle of [0.3, 0.8, 1.2, 2.5, 4]) {
      const to = { x: Math.cos(angle) * 500, y: Math.sin(angle) * 500 };
      const d = clearDistance({ x: 0, y: 0 }, to, box, item, 0);
      const c = stepToward({ x: 0, y: 0 }, to, d);
      const separated = Math.abs(c.x) >= box.hw + item.hw - 1e-6 || Math.abs(c.y) >= box.hh + item.hh - 1e-6;
      expect(separated).toBe(true);
    }
  });

  test('sideStep goes to the right when facing the target', () => {
    // From the bottom seat looking up at the centre, right is +x.
    const p = sideStep({ x: 0, y: 100 }, { x: 0, y: 0 }, { x: 0, y: 70 }, 30);
    expect(p.x).toBeCloseTo(30);
    expect(p.y).toBeCloseTo(70);
  });
});

function player(seat: number, over: Partial<PlayerState> = {}): PlayerState {
  return { seat, name: `p${seat}`, stack: 1000, bet: 0, inHand: true, folded: false, allIn: false, connected: true, ...over };
}

function state(over: Partial<TableState> = {}): TableState {
  return {
    hand: 1, street: 'Preflop', board: [], pot: 15, currentBet: 10, button: 0, toAct: 0, you: 0,
    players: [player(0, { bet: 5, cards: ['A♠', 'K♠'] }), player(1, { bet: 10 }), player(2)],
    ...over,
  };
}

describe('diffStates', () => {
  test('no animations for the first state after (re)connecting', () => {
    const e = diffStates(null, state());
    expect(e.newHand).toBe(false);
    expect(e.newBoardCards).toEqual([]);
  });

  test('a new hand deals cards', () => {
    const e = diffStates(state({ hand: 1, street: 'Showdown' }), state({ hand: 2 }));
    expect(e.newHand).toBe(true);
    expect(e.wins).toEqual([]);
    expect(e.betsToPot).toEqual([]);
  });

  test('flop, then turn and river all at once', () => {
    const flop = state({ street: 'Flop', board: ['2♣', '7♦', 'J♠'] });
    expect(diffStates(state(), flop).newBoardCards).toEqual([0, 1, 2]);
    const river = state({ street: 'Showdown', board: ['2♣', '7♦', 'J♠', '9♥', 'Q♣'] });
    expect(diffStates(flop, river).newBoardCards).toEqual([3, 4]);
  });

  test('a bet moves chips from the seat; a new street sweeps bets to the pot', () => {
    const prev = state();
    const raised = state({ players: [player(0, { bet: 40 }), player(1, { bet: 10 }), player(2)] });
    expect(diffStates(prev, raised).betsPlaced).toEqual([{ seat: 0, amount: 35 }]);

    const flop = state({ street: 'Flop', board: ['2♣', '7♦', 'J♠'], players: [player(0), player(1), player(2)] });
    expect(diffStates(raised, flop).betsToPot).toEqual([{ seat: 0, amount: 40 }, { seat: 1, amount: 10 }]);
  });

  test('folds, reveals and wins', () => {
    const prev = state({ street: 'River', board: ['2♣', '7♦', 'J♠', '9♥', 'Q♣'] });
    const folded = state({ ...prev, players: [prev.players[0], prev.players[1], player(2, { folded: true })] });
    expect(diffStates(prev, folded).foldedSeats).toEqual([2]);

    const showdown = state({
      ...folded,
      street: 'Showdown',
      players: [
        player(0, { cards: ['A♠', 'K♠'], won: 30 }),
        player(1, { cards: ['2♥', '3♥'] }),
        player(2, { folded: true }),
      ],
    });
    const e = diffStates(folded, showdown);
    expect(e.revealedSeats).toEqual([1]);
    expect(e.wins).toEqual([{ seat: 0, amount: 30 }]);
  });

  test('a seat freed or taken between states is not treated as anything', () => {
    const prev = state();
    const left = state({ players: [prev.players[0], null, prev.players[2]] });
    expect(diffStates(prev, left)).toEqual({ ...diffStates(prev, prev) });
    const back = state({ players: [prev.players[0], player(1, { inHand: false }), prev.players[2]] });
    const e = diffStates(left, back);
    expect(e.foldedSeats).toEqual([]);
    expect(e.betsPlaced).toEqual([]);
  });

  test('a player joining mid-hand is not treated as anything', () => {
    const prev = state();
    const next = state({ players: [...prev.players, player(3, { inHand: false })] });
    const e = diffStates(prev, next);
    expect(e.foldedSeats).toEqual([]);
    expect(e.betsPlaced).toEqual([]);
    expect(e.revealedSeats).toEqual([]);
  });
});

describe('isWatching', () => {
  test.each([
    [{ stack: 0, inHand: false }, 'Preflop', true],
    [{ stack: 0, inHand: true }, 'Showdown', true],
    [{ stack: 0, inHand: true }, 'River', false], // all-in, still playing
    [{ stack: 50, inHand: false }, 'Preflop', false],
  ] as const)('%o on %s → %s', (p, street, want) => {
    expect(isWatching(player(0, p), street)).toBe(want);
  });
});

describe('admin helpers', () => {
  test.each([
    ['1500', 1500],
    [' 0 ', 0],
    ['1000000', 1000000],
    ['1000001', null],
    ['-5', null],
    ['12.5', null],
    ['', null],
    ['abc', null],
  ])('parseStack(%j) → %s', (input, want) => {
    expect(parseStack(input)).toBe(want);
  });

  test('messages for admin API failures', () => {
    expect(adminMessage(401)).toBe('Неверный пароль');
    expect(adminMessage(429)).toContain('Подождите');
    expect(adminMessage(500)).toBe('Ошибка сервера (500)');
  });
});
