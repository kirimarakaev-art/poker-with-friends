package engine

import (
	"errors"
	"slices"
	"testing"
)

func eval(t *testing.T, s string) HandValue {
	t.Helper()
	v, err := Evaluate(cards(t, s))
	if err != nil {
		t.Fatalf("Evaluate(%s) error: %v", s, err)
	}
	return v
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"category wins", "2c 2d 5h 7s 9c", "Ac Kd Qh Js 9c", 1},
		{"flush beats straight", "2h 5h 7h 9h Jh", "Tc Jd Qh Ks Ac", 1},
		{"wheel loses to six-high straight", "Ac 2d 3h 4s 5c", "2c 3d 4h 5s 6c", -1},
		{"higher pair", "Kc Kd 2h 3s 4c", "Qc Qd Ah Ks Jc", 1},
		{"pair fourth kicker", "9c 9d Ah Ks 3c", "9h 9s Ad Kc 2h", 1},
		{"two pair second pair", "Ac Ad 8h 8s 2c", "Ah As 7d 7c Kh", 1},
		{"two pair kicker", "Ac Ad 8h 8s 3c", "Ah As 8d 8c 2h", 1},
		{"full house trips first", "3c 3d 3h 2s 2c", "2d 2h 2s Ac Ad", 1},
		{"flush last card", "Ah Kh 9h 6h 3h", "As Ks 9s 6s 2s", 1},
		{"exact tie", "Ac Kd Qh Js 9c", "Ad Kh Qs Jc 9d", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := eval(t, tt.a), eval(t, tt.b)
			if got := a.Compare(b); got != tt.want {
				t.Errorf("Compare(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if got := b.Compare(a); got != -tt.want {
				t.Errorf("Compare(%s, %s) = %d, want %d", tt.b, tt.a, got, -tt.want)
			}
		})
	}
}

func TestWinners(t *testing.T) {
	tests := []struct {
		name  string
		board string
		holes []string
		want  []int
	}{
		{"single winner", "2c 7d 9h Js Kc", []string{"Ah Ad", "Kh Qd", "3s 4s"}, []int{0}},
		{"kicker decides", "Ac 7d 9h Js 2c", []string{"Ah Kd", "Ad Qh"}, []int{0}},
		{"board plays, split", "Ts Js Qs Ks As", []string{"2c 3d", "4h 5c", "Ah Ad"}, []int{0, 1, 2}},
		{"two of three split", "Ac Kd 7h 7s 2c", []string{"Ah 3d", "As 4c", "Kh Qd"}, []int{0, 1}},
		{"last player wins", "2c 2d 5h 9s Jc", []string{"Ah Kd", "Qh Qd", "5c 5d"}, []int{2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			board := cards(t, tt.board)
			holes := make([][]Card, len(tt.holes))
			for i, h := range tt.holes {
				holes[i] = cards(t, h)
			}
			got, err := Winners(board, holes)
			if err != nil {
				t.Fatalf("Winners error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Winners = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWinnersDoesNotChangeBoard(t *testing.T) {
	full := cards(t, "2c 7d 9h Js Kc 3d")
	board := full[:5]
	if _, err := Winners(board, [][]Card{cards(t, "Ah Ad")}); err != nil {
		t.Fatalf("Winners error: %v", err)
	}
	if full[5] != (Card{Rank: Three, Suit: Diamonds}) {
		t.Errorf("Winners overwrote memory after the board: %v", full[5])
	}
}

func TestWinnersErrors(t *testing.T) {
	board := cards(t, "2c 7d 9h Js Kc")
	tests := []struct {
		name  string
		holes [][]Card
		want  error
	}{
		{"no players", nil, ErrNoPlayers},
		{"same card in two hands", [][]Card{cards(t, "Ah Ad"), cards(t, "Ah Kd")}, ErrInvalidHand},
		{"hole card on board", [][]Card{cards(t, "2c Ad")}, ErrInvalidHand},
		{"too many hole cards", [][]Card{cards(t, "Ah Ad As")}, ErrInvalidHand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Winners(board, tt.holes)
			if !errors.Is(err, tt.want) {
				t.Errorf("Winners error = %v, want %v", err, tt.want)
			}
			if got != nil {
				t.Errorf("Winners returned %v on error", got)
			}
		})
	}
}

// Сверяет Evaluate для 7 карт с перебором всех 21 пятёрки: проверяет, что из
// семи карт выбирается лучшая пятёрка. Значения пятёрок проверяют TestEvaluate и TestCompare.
func TestEvaluateMatchesBestOfFive(t *testing.T) {
	rng := testRNG(3)
	for range 20000 {
		d := NewDeck()
		d.Shuffle(rng)
		hand := d[:7]
		got, err := Evaluate(hand)
		if err != nil {
			t.Fatalf("Evaluate(%v) error: %v", hand, err)
		}
		var best HandValue
		for skip1 := 0; skip1 < 7; skip1++ {
			for skip2 := skip1 + 1; skip2 < 7; skip2++ {
				five := make([]Card, 0, 5)
				for i, c := range hand {
					if i != skip1 && i != skip2 {
						five = append(five, c)
					}
				}
				v, err := Evaluate(five)
				if err != nil {
					t.Fatalf("Evaluate(%v) error: %v", five, err)
				}
				if v.Compare(best) > 0 {
					best = v
				}
			}
		}
		if got != best {
			t.Fatalf("Evaluate(%v) = %v %v, best of five is %v %v", hand, got.Category, got.Ranks, best.Category, best.Ranks)
		}
	}
}
