package money

import "testing"

func TestMulDivRounding(t *testing.T) {
	cases := []struct{ a, b, c, want int64 }{
		{10, 1, 4, 3},   // 2.5 → 3 (half away from zero)
		{-10, 1, 4, -3}, // -2.5 → -3
		{9, 1, 4, 2},    // 2.25 → 2
		{1, 1, 3, 0},
		{2, 1, 3, 1},
		{100_000_000, 71234, 10_000, 712_340_000},
	}
	for _, c := range cases {
		if got := MulDiv(c.a, c.b, c.c); got != c.want {
			t.Errorf("MulDiv(%d,%d,%d)=%d want %d", c.a, c.b, c.c, got, c.want)
		}
	}
}

func TestMulDivNoOverflow(t *testing.T) {
	// a*b overflows int64 but the quotient fits
	if got := MulDiv(MaxCents, 1_000_000, 1_000_000); got != MaxCents {
		t.Fatalf("got %d", got)
	}
}

func TestSimpleInterest(t *testing.T) {
	// ¥150,000 at 6.8% for 30 days = 838.356… → ¥838.36
	if got := SimpleInterest(15_000_000, 680, 30); got != 83_836 {
		t.Fatalf("got %d", got)
	}
}

func TestAddOverflow(t *testing.T) {
	if _, err := Add(1<<62, 1<<62); err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestFormat(t *testing.T) {
	if s := YuanGrouped(123456789); s != "1,234,567.89" {
		t.Fatal(s)
	}
	if s := YuanGrouped(-5); s != "-0.05" {
		t.Fatal(s)
	}
	if s := Rate(71234); s != "7.1234" {
		t.Fatal(s)
	}
}
