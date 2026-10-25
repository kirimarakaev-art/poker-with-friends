export interface ParsedCard {
  rank: string;
  suit: string;
  isRed: boolean;
}

const RANKS = '23456789TJQKA';
const SUITS = '♣♦♥♠';

// parseCard turns a server card like "T♥" into display parts ("10", "♥").
export function parseCard(card: string): ParsedCard | null {
  const chars = [...card];
  if (chars.length !== 2 || !RANKS.includes(chars[0]) || !SUITS.includes(chars[1])) {
    return null;
  }
  const [rank, suit] = chars;
  return {
    rank: rank === 'T' ? '10' : rank,
    suit,
    isRed: suit === '♥' || suit === '♦',
  };
}
