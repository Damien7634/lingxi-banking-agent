package service

import (
	"testing"
	"time"
)

func TestNextBirthday(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 11, 2, 15, 0, 0, 0, loc)
	cases := map[string]string{
		"11-18": "2026-11-18", // later this month
		"11-02": "2027-11-02", // today but past noon → next year
		"01-05": "2027-01-05",
		"02-29": "2027-02-28", // non-leap fallback
	}
	for in, want := range cases {
		got, err := nextBirthday(in, now, loc)
		if err != nil || got.Format("2006-01-02") != want {
			t.Errorf("%s → %v %v, want %s", in, got, err, want)
		}
	}
	if _, err := nextBirthday("13-01", now, loc); err == nil {
		t.Error("invalid month accepted")
	}
}

func TestLoanInterest(t *testing.T) {
	applied := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	i, d := LoanInterest(15_000_000, 680, applied, applied.Add(29*24*time.Hour+time.Minute))
	if d != 30 || i != 83_836 {
		t.Fatalf("interest=%d days=%d", i, d)
	}
	if _, d := LoanInterest(1, 1, applied, applied); d != 1 {
		t.Fatal("minimum one day")
	}
}

func TestQuoteDeterministicAndBounded(t *testing.T) {
	now := time.Now()
	a, b := quoteBps("USDCNYB", 71000, now), quoteBps("USDCNYB", 71000, now)
	if a != b {
		t.Fatal("quote not deterministic within a slot")
	}
	if a < 70914 || a > 71086 { // ±0.12%
		t.Fatalf("quote out of band: %d", a)
	}
}

func TestAAValidate(t *testing.T) {
	in := AAIn{Title: "聚餐", TotalCents: 1000, Participants: []AAParticipantIn{{Name: "甲"}, {Name: "甲"}}}
	if err := in.Validate(); err == nil {
		t.Fatal("duplicate participant accepted")
	}
}
