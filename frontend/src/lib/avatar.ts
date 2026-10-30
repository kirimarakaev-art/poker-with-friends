const COLORS = ['#c9524a', '#3f83c4', '#8e55bd', '#2c9a72', '#c98a2e', '#b8437a', '#3a9aa8', '#7d8a35'];

// avatarColor gives each name its own stable colour.
export function avatarColor(name: string): string {
  let hash = 0;
  for (const ch of name) hash = (hash * 31 + (ch.codePointAt(0) ?? 0)) % 1_000_003;
  return COLORS[hash % COLORS.length];
}

// avatarLetter is the first character of the name, whole even if it is an emoji.
export function avatarLetter(name: string): string {
  const [first] = name;
  return first ? first.toUpperCase() : '?';
}
