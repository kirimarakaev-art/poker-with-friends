export interface PlayerState {
  seat: number;
  name: string;
  stack: number;
  bet: number;
  cards?: string[]; // e.g., ["2♥", "Q♠"]
  handName?: string; // e.g., "Pair"
  inHand: boolean;
  folded: boolean;
  allIn: boolean;
  connected: boolean;
  won?: number;
}

export interface GameOptions {
  canCheck: boolean;
  callAmount: number;
  canRaise: boolean;
  minRaiseTo: number;
  maxRaiseTo: number;
}

export interface TableState {
  hand: number;
  street: string; // "Waiting" | "Preflop" | "Flop" | "Turn" | "River" | "Showdown"
  board: string[]; // e.g., ["10♥", "7♠", "2♦"]
  pot: number;
  currentBet: number;
  button: number;
  toAct: number;
  deadline?: number;
  you: number;
  players: (PlayerState | null)[]; // null is a free seat
  options?: GameOptions;
}

export interface ChatMessage {
  sender: string;
  text: string;
}

export interface User {
  id: number;
  username: string;
  balance: number; // chips outside tables
  isAdmin: boolean;
  bonusAvailable: boolean;
  stats: { hands: number; handsWon: number };
}

export interface RoomInfo {
  code: string;
  owner: string;
  public: boolean;
  smallBlind: number;
  bigBlind: number;
  minBuyIn: number;
  maxBuyIn: number;
  players: number;
  maxPlayers: number;
  seated: boolean; // whether the asking player sits here
}
