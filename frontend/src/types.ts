export interface PlayerState {
  seat: number;
  name: string;
  stack: number;
  bet: number;
  cards?: string[]; // e.g., ["2♥", "Q♠"]
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
