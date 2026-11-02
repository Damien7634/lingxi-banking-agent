package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/money"
	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/trpc"
)

func (s *Service) registerBill(r *trpc.Router) {
	r.Query("bill.monthly", trpc.Typed(func(ctx context.Context, in MonthlyBillIn) (*MonthlyBill, error) {
		return s.MonthlyBill(ctx, in.Year, in.Month)
	}))
	r.Query("bill.yearly", trpc.Typed(func(ctx context.Context, in YearlyBillIn) (*YearlyBill, error) {
		return s.YearlyBill(ctx, in.Year)
	}))
	r.Query("bill.anomalies", trpc.Typed(func(ctx context.Context, in AnomaliesIn) ([]AnomalyItem, error) {
		return s.DetectAnomalies(ctx, in.Days)
	}))
}

// ---- inputs ----

type MonthlyBillIn struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}

func (in *MonthlyBillIn) Validate() error { return checkYearMonth(in.Year, in.Month) }

type YearlyBillIn struct {
	Year int `json:"year"`
}

func (in *YearlyBillIn) Validate() error { return checkYearMonth(in.Year, 1) }

type AnomaliesIn struct {
	Days int `json:"days"`
}

func (in *AnomaliesIn) Validate() error {
	if in.Days == 0 {
		in.Days = 90
	}
	if in.Days < 7 || in.Days > 365 {
		return apperr.Bad("天数需在 7-365 之间")
	}
	return nil
}

func checkYearMonth(year, month int) error {
	if year < 2000 || year > 2100 {
		return apperr.Bad("年份需在 2000-2100 之间")
	}
	if month < 1 || month > 12 {
		return apperr.Bad("月份需在 1-12 之间")
	}
	return nil
}

// ---- outputs ----

type CategoryStat struct {
	Category string  `json:"category"`
	Cents    int64   `json:"cents"`
	Pct      float64 `json:"pct"`
}

type CounterpartyStat struct {
	Name  string `json:"name"`
	Cents int64  `json:"cents"`
}

type BillSummary struct {
	TotalInCents      int64              `json:"totalInCents"`
	TotalOutCents     int64              `json:"totalOutCents"`
	TopCategories     []CategoryStat     `json:"topCategories"`
	TopCounterparties []CounterpartyStat `json:"topCounterparties"`
	Count             int                `json:"count"`
}

type MonthlyBill struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	BillSummary
	PrevTotalOutCents int64         `json:"prevTotalOutCents"`
	MomPct            *float64      `json:"momPct"`
	Rows              []Transaction `json:"rows"`
}

type MonthStat struct {
	Month    int   `json:"month"`
	InCents  int64 `json:"inCents"`
	OutCents int64 `json:"outCents"`
}

type YearlyBill struct {
	Year   int         `json:"year"`
	Months []MonthStat `json:"months"`
	BillSummary
}

type AnomalyItem struct {
	Tx      Transaction `json:"tx"`
	Reasons []string    `json:"reasons"`
}

// ---- summarizer ----

type billAcc struct {
	in, out int64
	count   int
	byCat   map[string]int64
	byCpty  map[string]int64
}

func newBillAcc() *billAcc {
	return &billAcc{byCat: map[string]int64{}, byCpty: map[string]int64{}}
}

func (a *billAcc) add(direction string, cents int64, category, counterparty string) {
	a.count++
	if direction == "in" {
		a.in += cents
		return
	}
	a.out += cents
	a.byCat[category] += cents
	a.byCpty[counterparty] += cents
}

func (a *billAcc) summary() BillSummary {
	cats := make([]CategoryStat, 0, len(a.byCat))
	for c, v := range a.byCat {
		pct := 0.0
		if a.out > 0 {
			pct = float64(money.MulDiv(v, 1000, a.out)) / 10
		}
		cats = append(cats, CategoryStat{Category: c, Cents: v, Pct: pct})
	}
	sort.Slice(cats, func(i, j int) bool {
		if cats[i].Cents != cats[j].Cents {
			return cats[i].Cents > cats[j].Cents
		}
		return cats[i].Category < cats[j].Category
	})
	cps := make([]CounterpartyStat, 0, len(a.byCpty))
	for n, v := range a.byCpty {
		cps = append(cps, CounterpartyStat{Name: n, Cents: v})
	}
	sort.Slice(cps, func(i, j int) bool {
		if cps[i].Cents != cps[j].Cents {
			return cps[i].Cents > cps[j].Cents
		}
		return cps[i].Name < cps[j].Name
	})
	if len(cps) > 5 {
		cps = cps[:5]
	}
	return BillSummary{TotalInCents: a.in, TotalOutCents: a.out, TopCategories: cats, TopCounterparties: cps, Count: a.count}
}

func (s *Service) monthRange(year, month int) (time.Time, time.Time) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, s.Cfg.Location)
	return start.UTC(), start.AddDate(0, 1, 0).UTC()
}

// ---- monthly ----

func (s *Service) MonthlyBill(ctx context.Context, year, month int) (*MonthlyBill, error) {
	if err := checkYearMonth(year, month); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	start, end := s.monthRange(year, month)
	py, pm := year, month-1
	if pm == 0 {
		py, pm = year-1, 12
	}
	pstart, pend := s.monthRange(py, pm)

	rows, err := s.DB.QueryContext(ctx, `SELECT `+txColumns+` FROM transactions
		WHERE user_id = ? AND occurred_at >= ? AND occurred_at < ? ORDER BY occurred_at DESC, id DESC`, uid, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	acc := newBillAcc()
	list := []Transaction{}
	for rows.Next() {
		t, err := scanTx(rows)
		if err != nil {
			return nil, err
		}
		acc.add(t.Direction, t.AmountCents, t.Category, t.Counterparty)
		if len(list) < 200 {
			list = append(list, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var prevOut int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents), 0) FROM transactions
		WHERE user_id = ? AND direction = 'out' AND occurred_at >= ? AND occurred_at < ?`, uid, pstart, pend).Scan(&prevOut); err != nil {
		return nil, err
	}
	sum := acc.summary()
	out := &MonthlyBill{Year: year, Month: month, BillSummary: sum, PrevTotalOutCents: prevOut, Rows: list}
	if prevOut > 0 {
		v := float64(money.MulDiv(sum.TotalOutCents-prevOut, 1000, prevOut)) / 10
		out.MomPct = &v
	}
	return out, nil
}

// ---- yearly ----

func (s *Service) YearlyBill(ctx context.Context, year int) (*YearlyBill, error) {
	if err := checkYearMonth(year, 1); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, s.Cfg.Location).UTC()
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, s.Cfg.Location).UTC()
	rows, err := s.DB.QueryContext(ctx, `SELECT direction, amount_cents, category, counterparty, occurred_at FROM transactions
		WHERE user_id = ? AND occurred_at >= ? AND occurred_at < ?`, uid, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	months := make([]MonthStat, 12)
	for i := range months {
		months[i].Month = i + 1
	}
	acc := newBillAcc()
	for rows.Next() {
		var dir, cat, cp string
		var cents int64
		var at time.Time
		if err := rows.Scan(&dir, &cents, &cat, &cp, &at); err != nil {
			return nil, err
		}
		acc.add(dir, cents, cat, cp)
		m := int(at.In(s.Cfg.Location).Month()) - 1
		if m < 0 || m > 11 {
			continue
		}
		if dir == "in" {
			months[m].InCents += cents
		} else {
			months[m].OutCents += cents
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &YearlyBill{Year: year, Months: months, BillSummary: acc.summary()}, nil
}

// ---- anomalies ----

const anomalyScanLimit = 5000

func (s *Service) DetectAnomalies(ctx context.Context, days int) ([]AnomalyItem, error) {
	if days == 0 {
		days = 90
	}
	if days < 7 || days > 365 {
		return nil, apperr.Bad("天数需在 7-365 之间")
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	since := s.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()

	rows, err := s.DB.QueryContext(ctx, `SELECT `+txColumns+` FROM transactions
		WHERE user_id = ? AND direction = 'out' AND occurred_at >= ? ORDER BY occurred_at DESC, id DESC LIMIT ?`, uid, since, anomalyScanLimit)
	if err != nil {
		return nil, err
	}
	var txs []Transaction
	for rows.Next() {
		t, err := scanTx(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		txs = append(txs, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	known := map[string]struct{}{}
	kr, err := s.DB.QueryContext(ctx, `SELECT DISTINCT counterparty FROM transactions
		WHERE user_id = ? AND direction = 'out' AND occurred_at < ?`, uid, since)
	if err != nil {
		return nil, err
	}
	defer kr.Close()
	for kr.Next() {
		var cp string
		if err := kr.Scan(&cp); err != nil {
			return nil, err
		}
		known[cp] = struct{}{}
	}
	if err := kr.Err(); err != nil {
		return nil, err
	}

	// per business-day, per category totals
	dayCat := map[string]int64{}
	dayKey := func(t Transaction) string {
		return t.OccurredAt.In(s.Cfg.Location).Format("2006-01-02") + "|" + t.Category
	}
	for _, t := range txs {
		dayCat[dayKey(t)] += t.AmountCents
	}

	out := []AnomalyItem{}
	seen := map[uint64]struct{}{}
	for _, t := range txs {
		if _, dup := seen[t.ID]; dup {
			continue
		}
		var reasons []string
		add := func(r string) {
			for _, e := range reasons {
				if e == r {
					return
				}
			}
			reasons = append(reasons, r)
		}
		if hour := t.OccurredAt.In(s.Cfg.Location).Hour(); hour < 5 && t.AmountCents >= 100000 {
			add(fmt.Sprintf("凌晨 %d 点发生大额支出（¥%s）", hour, money.YuanGrouped(t.AmountCents)))
		}
		if t.Category == "转账" && t.AmountCents >= 500000 {
			if _, ok := known[t.Counterparty]; !ok {
				add(fmt.Sprintf("收款方「%s」无历史往来记录，单笔金额较大", t.Counterparty))
			}
		}
		if t.Category == "娱乐" || t.Category == "游戏" {
			if total := dayCat[dayKey(t)]; total >= 300000 {
				add(fmt.Sprintf("当日「%s」类支出累计 ¥%s，频次/金额异常", t.Category, money.YuanGrouped(total)))
			}
		}
		if t.IsAnomaly && t.AnomalyReason != nil && *t.AnomalyReason != "" {
			add(*t.AnomalyReason)
		}
		if len(reasons) > 0 {
			seen[t.ID] = struct{}{}
			out = append(out, AnomalyItem{Tx: t, Reasons: reasons})
		}
	}
	return out, nil
}
