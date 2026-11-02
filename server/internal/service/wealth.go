package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/db"
	"lingxi/server/internal/ledger"
	"lingxi/server/internal/money"
	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/trpc"
)

var riskOrder = []string{"R1", "R2", "R3", "R4", "R5"}

var levelMaxRisk = map[string]string{
	"保守型": "R1",
	"稳健型": "R2",
	"平衡型": "R3",
	"进取型": "R4",
	"激进型": "R5",
}

// A risk assessment is valid for one year (suitability rules); after that the
// customer is treated as unassessed, i.e. capped at 稳健型 / R2.
const riskProfileValidity = 365 * 24 * time.Hour
const defaultRiskLevel = "稳健型"

func scoreToLevel(score int) string {
	switch {
	case score <= 30:
		return "保守型"
	case score <= 50:
		return "稳健型"
	case score <= 70:
		return "平衡型"
	case score <= 85:
		return "进取型"
	default:
		return "激进型"
	}
}

type WealthProduct struct {
	ID             uint64  `json:"id"`
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	RiskLevel      string  `json:"riskLevel"`
	AnnualRateBps  int64   `json:"annualRateBps"`
	MinAmountCents int64   `json:"minAmountCents"`
	TermDays       int     `json:"termDays"`
	Tags           *string `json:"tags"`
	Description    *string `json:"description"`
}

const wpColumns = `id, name, type, risk_level, annual_rate_bps, min_amount_cents, term_days, tags, description`

func scanWP(sc interface{ Scan(...any) error }) (WealthProduct, error) {
	var p WealthProduct
	var tags, desc sql.NullString
	err := sc.Scan(&p.ID, &p.Name, &p.Type, &p.RiskLevel, &p.AnnualRateBps, &p.MinAmountCents, &p.TermDays, &tags, &desc)
	p.Tags, p.Description = ns(tags), ns(desc)
	return p, err
}

type RiskProfile struct {
	ID        uint64    `json:"id"`
	Score     int       `json:"score"`
	Level     string    `json:"level"`
	Answers   string    `json:"answers"`
	CreatedAt time.Time `json:"createdAt"`
}

type Holding struct {
	ID          uint64         `json:"id"`
	ProductID   uint64         `json:"productId"`
	AmountCents int64          `json:"amountCents"`
	ProfitCents int64          `json:"profitCents"`
	Status      string         `json:"status"`
	PurchasedAt time.Time      `json:"purchasedAt"`
	RedeemedAt  *time.Time     `json:"redeemedAt"`
	Product     *WealthProduct `json:"product"`
}

func (s *Service) registerWealth(r *trpc.Router) {
	r.Query("wealth.products", trpc.NoInput(s.WealthProducts))
	r.Query("wealth.recommend", trpc.NoInput(func(ctx context.Context) (*Recommendation, error) { return s.RecommendWealth(ctx, 4) }))
	r.Query("wealth.riskProfile", trpc.NoInput(s.LatestRiskProfile))
	r.Mutation("wealth.submitRiskAssessment", trpc.Typed(s.SubmitRiskAssessment))
	r.Query("wealth.holdings", trpc.NoInput(s.Holdings))
	r.Mutation("wealth.purchase", trpc.Typed(s.PurchaseWealth))
	r.Mutation("wealth.redeem", trpc.Typed(s.RedeemWealth))
}

func (s *Service) WealthProducts(ctx context.Context) ([]WealthProduct, error) {
	if _, err := reqctx.UserID(ctx); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+wpColumns+` FROM wealth_products ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WealthProduct{}
	for rows.Next() {
		p, err := scanWP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) getWealthProduct(ctx context.Context, q db.Querier, id uint64) (*WealthProduct, error) {
	p, err := scanWP(q.QueryRowContext(ctx, `SELECT `+wpColumns+` FROM wealth_products WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundf("产品不存在")
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// LatestRiskProfile returns the newest still-valid assessment, or nil.
func (s *Service) LatestRiskProfile(ctx context.Context) (*RiskProfile, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	return s.latestRiskProfile(ctx, s.DB, uid)
}

func (s *Service) latestRiskProfile(ctx context.Context, q db.Querier, uid uint64) (*RiskProfile, error) {
	var p RiskProfile
	err := q.QueryRowContext(ctx, `SELECT id, score, level, answers, created_at FROM risk_profiles
		WHERE user_id = ? AND created_at >= ? ORDER BY created_at DESC, id DESC LIMIT 1`,
		uid, s.Now().UTC().Add(-riskProfileValidity)).
		Scan(&p.ID, &p.Score, &p.Level, &p.Answers, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func maxRiskFor(p *RiskProfile) (level, maxRisk string) {
	level = defaultRiskLevel
	if p != nil {
		level = p.Level
	}
	maxRisk, ok := levelMaxRisk[level]
	if !ok {
		level, maxRisk = defaultRiskLevel, levelMaxRisk[defaultRiskLevel]
	}
	return level, maxRisk
}

type Recommendation struct {
	Profile  *RiskProfile    `json:"profile"`
	MaxRisk  string          `json:"maxRisk"`
	Products []WealthProduct `json:"products"`
}

func (s *Service) RecommendWealth(ctx context.Context, limit int) (*Recommendation, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := s.latestRiskProfile(ctx, s.DB, uid)
	if err != nil {
		return nil, err
	}
	_, maxRisk := maxRiskFor(profile)
	all, err := s.WealthProducts(ctx)
	if err != nil {
		return nil, err
	}
	maxIdx := slices.Index(riskOrder, maxRisk)
	eligible := []WealthProduct{}
	for _, p := range all {
		if slices.Index(riskOrder, p.RiskLevel) <= maxIdx {
			eligible = append(eligible, p)
		}
	}
	slices.SortStableFunc(eligible, func(a, b WealthProduct) int { return int(b.AnnualRateBps - a.AnnualRateBps) })
	if len(eligible) > limit {
		eligible = eligible[:limit]
	}
	return &Recommendation{Profile: profile, MaxRisk: maxRisk, Products: eligible}, nil
}

type RiskIn struct {
	Score   int               `json:"score"`
	Answers map[string]string `json:"answers"`
}

func (in *RiskIn) Validate() error {
	if in.Score < 0 || in.Score > 100 {
		return apperr.Bad("测评分数需在 0-100 之间")
	}
	if len(in.Answers) > 30 {
		return apperr.Bad("测评答案过多")
	}
	for k, v := range in.Answers {
		if runeLen(k) > 64 || runeLen(v) > 128 {
			return apperr.Bad("测评答案过长")
		}
	}
	if in.Answers == nil {
		in.Answers = map[string]string{}
	}
	return nil
}

type RiskOut struct {
	ID    uint64 `json:"id"`
	Score int    `json:"score"`
	Level string `json:"level"`
}

func (s *Service) SubmitRiskAssessment(ctx context.Context, in RiskIn) (*RiskOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	level := scoreToLevel(in.Score)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO risk_profiles (user_id, score, level, answers) VALUES (?, ?, ?, ?)`,
		uid, in.Score, level, mustJSON(in.Answers))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &RiskOut{ID: uint64(id), Score: in.Score, Level: level}, nil
}

func (s *Service) Holdings(ctx context.Context) ([]Holding, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT h.id, h.product_id, h.amount_cents, h.profit_cents, h.status, h.purchased_at, h.redeemed_at,
		       p.id, p.name, p.type, p.risk_level, p.annual_rate_bps, p.min_amount_cents, p.term_days, p.tags, p.description
		FROM holdings h JOIN wealth_products p ON p.id = h.product_id
		WHERE h.user_id = ? AND h.status = 'holding' ORDER BY h.purchased_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Holding{}
	for rows.Next() {
		var h Holding
		var redeemed sql.NullTime
		var p WealthProduct
		var tags, desc sql.NullString
		if err := rows.Scan(&h.ID, &h.ProductID, &h.AmountCents, &h.ProfitCents, &h.Status, &h.PurchasedAt, &redeemed,
			&p.ID, &p.Name, &p.Type, &p.RiskLevel, &p.AnnualRateBps, &p.MinAmountCents, &p.TermDays, &tags, &desc); err != nil {
			return nil, err
		}
		p.Tags, p.Description = ns(tags), ns(desc)
		h.RedeemedAt, h.Product = nt(redeemed), &p
		out = append(out, h)
	}
	return out, rows.Err()
}

type PurchaseIn struct {
	ProductID   uint64 `json:"productId"`
	AmountCents int64  `json:"amountCents"`
}

func (in *PurchaseIn) Validate() error {
	if err := checkID("产品", in.ProductID); err != nil {
		return err
	}
	return checkCents("申购金额", in.AmountCents, 100_000_000)
}

type PurchaseOut struct {
	HoldingID    uint64 `json:"holdingId"`
	Merged       bool   `json:"merged"`
	Product      string `json:"product"`
	BalanceCents int64  `json:"balanceCents"`
}

// PurchaseWealth enforces suitability (product risk ≤ assessed level) before
// any money moves, then debits and books the holding in one transaction.
func (s *Service) PurchaseWealth(ctx context.Context, in PurchaseIn) (*PurchaseOut, error) {
	// Money-moving entry points validate themselves; callers (tRPC, agent, worker) may not.
	if err := in.Validate(); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	var out PurchaseOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		p, err := s.getWealthProduct(ctx, tx, in.ProductID)
		if err != nil {
			return err
		}
		profile, err := s.latestRiskProfile(ctx, tx, uid)
		if err != nil {
			return err
		}
		level, maxRisk := maxRiskFor(profile)
		if slices.Index(riskOrder, p.RiskLevel) > slices.Index(riskOrder, maxRisk) {
			return apperr.New(apperr.Forbidden, "适当性拦截：您的风险测评结果为「%s」，该产品为 %s，超出您的风险承受能力", level, p.RiskLevel)
		}
		if in.AmountCents < p.MinAmountCents {
			return apperr.Bad("该产品起购金额为 ¥%s", money.YuanGrouped(p.MinAmountCents))
		}
		if _, err := ledger.Debit(ctx, tx, acc, ledger.Posting{
			AmountCents: in.AmountCents, Category: "理财", Counterparty: "灵犀理财", Memo: "申购 " + p.Name,
			BizType: "wealth_purchase", BizRef: uintStr(p.ID),
		}); err != nil {
			return err
		}
		out = PurchaseOut{Product: p.Name, BalanceCents: acc.BalanceCents}

		var hid uint64
		err = tx.QueryRowContext(ctx, `SELECT id FROM holdings WHERE user_id = ? AND product_id = ? AND status = 'holding' ORDER BY id LIMIT 1 FOR UPDATE`, uid, p.ID).Scan(&hid)
		switch {
		case err == nil:
			_, err = tx.ExecContext(ctx, `UPDATE holdings SET amount_cents = amount_cents + ? WHERE id = ? AND user_id = ?`, in.AmountCents, hid, uid)
			out.HoldingID, out.Merged = hid, true
			return err
		case errors.Is(err, sql.ErrNoRows):
			res, err := tx.ExecContext(ctx, `INSERT INTO holdings (user_id, product_id, amount_cents, purchased_at) VALUES (?, ?, ?, ?)`, uid, p.ID, in.AmountCents, s.Now().UTC())
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			out.HoldingID = uint64(id)
			return nil
		default:
			return err
		}
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type RedeemIn struct {
	HoldingID   uint64 `json:"holdingId"`
	AmountCents *int64 `json:"amountCents"` // nil = full redemption
}

func (in *RedeemIn) Validate() error {
	if err := checkID("持仓", in.HoldingID); err != nil {
		return err
	}
	if in.AmountCents != nil {
		return checkCents("赎回金额", *in.AmountCents, money.MaxCents)
	}
	return nil
}

type RedeemOut struct {
	OK            bool   `json:"ok"`
	RedeemedCents int64  `json:"redeemedCents"`
	ProfitCents   int64  `json:"profitCents"`
	Product       string `json:"product"`
}

func (s *Service) RedeemWealth(ctx context.Context, in RedeemIn) (*RedeemOut, error) {
	// Money-moving entry points validate themselves; callers (tRPC, agent, worker) may not.
	if err := in.Validate(); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	var out RedeemOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		var productID uint64
		var amount, profit int64
		var purchasedAt time.Time
		err = tx.QueryRowContext(ctx, `SELECT product_id, amount_cents, profit_cents, purchased_at FROM holdings
			WHERE id = ? AND user_id = ? AND status = 'holding' FOR UPDATE`, in.HoldingID, uid).Scan(&productID, &amount, &profit, &purchasedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundf("持仓不存在或已赎回")
		}
		if err != nil {
			return err
		}
		p, err := s.getWealthProduct(ctx, tx, productID)
		if err != nil {
			return err
		}
		if p.TermDays > 0 {
			if maturity := purchasedAt.AddDate(0, 0, p.TermDays); s.Now().Before(maturity) {
				return apperr.Bad("「%s」为 %d 天封闭期产品，%s 到期后方可赎回", p.Name, p.TermDays, s.local(maturity).Format("2006-01-02"))
			}
		}
		redeem := amount
		if in.AmountCents != nil && *in.AmountCents < amount {
			redeem = *in.AmountCents
		}
		full := redeem == amount
		paid := redeem
		if full {
			paid += profit
			out.ProfitCents = profit
		}
		if paid > 0 {
			if _, err := ledger.Credit(ctx, tx, acc, ledger.Posting{
				AmountCents: paid, Category: "理财", Counterparty: "灵犀理财", Memo: "赎回 " + p.Name,
				BizType: "wealth_redeem", BizRef: uintStr(in.HoldingID),
			}); err != nil {
				return err
			}
		}
		if full {
			_, err = tx.ExecContext(ctx, `UPDATE holdings SET status = 'redeemed', redeemed_at = ?, amount_cents = 0 WHERE id = ? AND user_id = ?`, s.Now().UTC(), in.HoldingID, uid)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE holdings SET amount_cents = amount_cents - ? WHERE id = ? AND user_id = ?`, redeem, in.HoldingID, uid)
		}
		out.OK, out.RedeemedCents, out.Product = true, redeem, p.Name
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
