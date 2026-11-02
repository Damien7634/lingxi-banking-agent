package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/ledger"
	"lingxi/server/internal/money"
	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/trpc"
)

type currencyMeta struct{ Name, Symbol string }

var currencies = map[string]currencyMeta{
	"USD": {"美元", "$"},
	"EUR": {"欧元", "€"},
	"GBP": {"英镑", "£"},
	"HKD": {"港币", "HK$"},
	"JPY": {"日元", "JP¥"},
}

const (
	forwardMarginBps = 500 // 5% CNY margin
	optionPremiumBps = 150 // 1.5% premium
	maxFXNotional    = 100_000_000
)

// quoteBps applies a deterministic ±0.12% jitter per 10-minute slot so the
// display is lively, and — unlike the old code — the same quote is used for
// execution: the customer trades at the rate they were shown.
func quoteBps(pair string, base int64, now time.Time) int64 {
	slot := now.Unix() / 600
	s := pair + ":" + itoa(slot)
	var h uint32
	for i := 0; i < len(s); i++ {
		h = h*31 + uint32(s[i])
	}
	ppm := int64(h%2401) - 1200 // −1200..+1200 ppm = ±0.12%
	return money.MulDiv(base, 1_000_000+ppm, 1_000_000)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

type rateRow struct {
	Pair     string
	Currency string
	BuyBps   int64
	SellBps  int64
}

func (s *Service) loadRates(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (map[string]rateRow, []rateRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT pair, currency, buy_rate_bps, sell_rate_bps FROM fx_rates ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	m := map[string]rateRow{}
	var list []rateRow
	now := s.Now()
	for rows.Next() {
		var r rateRow
		if err := rows.Scan(&r.Pair, &r.Currency, &r.BuyBps, &r.SellBps); err != nil {
			return nil, nil, err
		}
		r.BuyBps = quoteBps(r.Pair+"B", r.BuyBps, now)
		r.SellBps = quoteBps(r.Pair+"S", r.SellBps, now)
		m[r.Currency] = r
		list = append(list, r)
	}
	return m, list, rows.Err()
}

func (s *Service) registerFX(r *trpc.Router) {
	r.Query("fx.rates", trpc.NoInput(s.FXRates))
	r.Query("fx.wallets", trpc.NoInput(s.FXWallets))
	r.Query("fx.exposure", trpc.NoInput(s.FXExposure))
	r.Query("fx.contracts", trpc.NoInput(s.FXContracts))
	r.Mutation("fx.createForward", trpc.Typed(s.CreateForward))
	r.Mutation("fx.createOption", trpc.Typed(s.CreateOption))
	r.Mutation("fx.cancel", trpc.Typed(s.CancelFXContract))
}

type FXRate struct {
	Pair      string    `json:"pair"`
	Currency  string    `json:"currency"`
	Name      string    `json:"name"`
	Symbol    string    `json:"symbol"`
	BuyRate   float64   `json:"buyRate"`
	SellRate  float64   `json:"sellRate"`
	BuyBps    int64     `json:"buyRateBps"`
	SellBps   int64     `json:"sellRateBps"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Service) FXRates(ctx context.Context) ([]FXRate, error) {
	if _, err := reqctx.UserID(ctx); err != nil {
		return nil, err
	}
	_, list, err := s.loadRates(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	out := []FXRate{}
	now := s.Now()
	for _, r := range list {
		m := currencies[r.Currency]
		out = append(out, FXRate{Pair: r.Pair, Currency: r.Currency, Name: m.Name, Symbol: m.Symbol,
			BuyRate: float64(r.BuyBps) / 10000, SellRate: float64(r.SellBps) / 10000, BuyBps: r.BuyBps, SellBps: r.SellBps, UpdatedAt: now})
	}
	return out, nil
}

type FXWallet struct {
	ID                  uint64    `json:"id"`
	Currency            string    `json:"currency"`
	BalanceForeignCents int64     `json:"balanceForeignCents"`
	LockedForeignCents  int64     `json:"lockedForeignCents"`
	UpdatedAt           time.Time `json:"updatedAt"`
	Name                string    `json:"name"`
	Symbol              string    `json:"symbol"`
	RateBps             int64     `json:"rateBps"`
	CnyCents            int64     `json:"cnyCents"`
}

func (s *Service) FXWallets(ctx context.Context) ([]FXWallet, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	rates, _, err := s.loadRates(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, currency, balance_foreign_cents, locked_foreign_cents, updated_at FROM fx_wallets WHERE user_id = ? ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FXWallet{}
	for rows.Next() {
		var w FXWallet
		if err := rows.Scan(&w.ID, &w.Currency, &w.BalanceForeignCents, &w.LockedForeignCents, &w.UpdatedAt); err != nil {
			return nil, err
		}
		m := currencies[w.Currency]
		w.Name, w.Symbol = m.Name, m.Symbol
		w.RateBps = rates[w.Currency].BuyBps
		w.CnyCents = money.ForeignToCNY(w.BalanceForeignCents, w.RateBps)
		out = append(out, w)
	}
	return out, rows.Err()
}

type ExposureItem struct {
	Currency       string  `json:"currency"`
	Name           string  `json:"name"`
	ForeignCents   int64   `json:"foreignCents"`
	CnyCents       int64   `json:"cnyCents"`
	HedgedCnyCents int64   `json:"hedgedCnyCents"`
	HedgeRatio     float64 `json:"hedgeRatio"`
}

type Exposure struct {
	Items               []ExposureItem `json:"items"`
	TotalCnyCents       int64          `json:"totalCnyCents"`
	TotalHedgedCnyCents int64          `json:"totalHedgedCnyCents"`
	OpenCnyCents        int64          `json:"openCnyCents"`
	HedgeRatio          float64        `json:"hedgeRatio"`
}

// FXExposure: foreign assets are hedged only by active sell_foreign contracts
// (buy_foreign contracts hedge a liability, not these balances).
func (s *Service) FXExposure(ctx context.Context) (*Exposure, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	wallets, err := s.FXWallets(ctx)
	if err != nil {
		return nil, err
	}
	hedged := map[string]int64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT currency, SUM(foreign_amount_cents) FROM fx_contracts
		WHERE user_id = ? AND status = 'active' AND direction = 'sell_foreign' GROUP BY currency`, uid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c string
		var sum int64
		if err := rows.Scan(&c, &sum); err != nil {
			rows.Close()
			return nil, err
		}
		hedged[c] = sum
	}
	rows.Close()

	out := &Exposure{Items: []ExposureItem{}}
	for _, w := range wallets {
		if w.BalanceForeignCents <= 0 {
			continue
		}
		hc := money.ForeignToCNY(min(hedged[w.Currency], w.BalanceForeignCents), w.RateBps)
		it := ExposureItem{Currency: w.Currency, Name: w.Name, ForeignCents: w.BalanceForeignCents, CnyCents: w.CnyCents, HedgedCnyCents: hc}
		if w.CnyCents > 0 {
			it.HedgeRatio = min(1, float64(hc)/float64(w.CnyCents))
		}
		out.Items = append(out.Items, it)
		out.TotalCnyCents += w.CnyCents
		out.TotalHedgedCnyCents += hc
	}
	out.OpenCnyCents = out.TotalCnyCents - out.TotalHedgedCnyCents
	if out.TotalCnyCents > 0 {
		out.HedgeRatio = float64(out.TotalHedgedCnyCents) / float64(out.TotalCnyCents)
	}
	return out, nil
}

type FXContract struct {
	ID                 uint64    `json:"id"`
	Type               string    `json:"type"`
	Currency           string    `json:"currency"`
	ForeignAmountCents int64     `json:"foreignAmountCents"`
	LockedRateBps      int64     `json:"lockedRateBps"`
	Direction          string    `json:"direction"`
	MarginCents        int64     `json:"marginCents"`
	PremiumCents       int64     `json:"premiumCents"`
	ExpiresAt          time.Time `json:"expiresAt"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"createdAt"`
}

const fxcColumns = `id, type, currency, foreign_amount_cents, locked_rate_bps, direction, margin_cents, premium_cents, expires_at, status, created_at`

func scanFXC(sc interface{ Scan(...any) error }) (FXContract, error) {
	var c FXContract
	err := sc.Scan(&c.ID, &c.Type, &c.Currency, &c.ForeignAmountCents, &c.LockedRateBps, &c.Direction, &c.MarginCents, &c.PremiumCents, &c.ExpiresAt, &c.Status, &c.CreatedAt)
	return c, err
}

func (s *Service) FXContracts(ctx context.Context) ([]FXContract, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+fxcColumns+` FROM fx_contracts WHERE user_id = ? ORDER BY id DESC LIMIT 200`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FXContract{}
	for rows.Next() {
		c, err := scanFXC(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type ContractIn struct {
	Currency           string `json:"currency"`
	ForeignAmountCents int64  `json:"foreignAmountCents"`
	TenorDays          int    `json:"tenorDays"`
	Direction          string `json:"direction"`
}

func (in *ContractIn) Validate() error {
	if _, ok := currencies[in.Currency]; !ok {
		return apperr.Bad("暂不支持该币种")
	}
	if err := checkCents("名义本金", in.ForeignAmountCents, maxFXNotional); err != nil {
		return err
	}
	if in.TenorDays < 7 || in.TenorDays > 365 {
		return apperr.Bad("期限需为 7-365 天")
	}
	if in.Direction == "" {
		in.Direction = "sell_foreign"
	}
	if in.Direction != "sell_foreign" && in.Direction != "buy_foreign" {
		return apperr.Bad("交易方向无效")
	}
	return nil
}

type ContractOut struct {
	ID          uint64    `json:"id"`
	LockedRate  string    `json:"lockedRate"`
	MarginText  string    `json:"marginText,omitempty"`
	PremiumText string    `json:"premiumText,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Message     string    `json:"message"`
}

func pickRate(r rateRow, direction string) int64 {
	if direction == "sell_foreign" {
		return r.BuyBps // bank buys foreign from customer
	}
	return r.SellBps
}

// CreateForward locks a rate and freezes 5% CNY margin. For sell_foreign the
// foreign notional is also reserved in the wallet so it cannot be spent twice.
func (s *Service) CreateForward(ctx context.Context, in ContractIn) (*ContractOut, error) {
	// Money-moving entry points validate themselves; callers (tRPC, agent, worker) may not.
	if err := in.Validate(); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	var out ContractOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		rates, _, err := s.loadRates(ctx, tx)
		if err != nil {
			return err
		}
		rr, ok := rates[in.Currency]
		if !ok {
			return apperr.Bad("暂不支持该币种")
		}
		rate := pickRate(rr, in.Direction)
		if in.Direction == "sell_foreign" {
			w, err := ledger.LockWallet(ctx, tx, uid, in.Currency)
			if err != nil {
				return err
			}
			if err := ledger.HoldForeign(ctx, tx, w, in.ForeignAmountCents); err != nil {
				return apperr.Bad("%s 钱包可用余额不足，无法锁定该金额的远期结汇", in.Currency)
			}
		}
		margin := money.Pct(money.ForeignToCNY(in.ForeignAmountCents, rate), forwardMarginBps)
		if margin <= 0 {
			return apperr.Bad("名义本金过小")
		}
		if err := ledger.Hold(ctx, tx, acc, margin, "冻结远期保证金"); err != nil {
			return err
		}
		expires := s.Now().UTC().AddDate(0, 0, in.TenorDays)
		res, err := tx.ExecContext(ctx, `INSERT INTO fx_contracts (user_id, type, currency, foreign_amount_cents, locked_rate_bps, direction, margin_cents, expires_at)
			VALUES (?, 'forward', ?, ?, ?, ?, ?, ?)`, uid, in.Currency, in.ForeignAmountCents, rate, in.Direction, margin, expires)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		out = ContractOut{
			ID: uint64(id), LockedRate: money.Rate(rate), MarginText: money.Yuan(margin), ExpiresAt: expires,
			Message: "远期锁汇成功：" + in.Currency + " 锁定汇率 " + money.Rate(rate) + "，保证金 ¥" + money.YuanGrouped(margin) + " 已冻结，" + itoa(int64(in.TenorDays)) + " 天后到期",
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateOption charges a 1.5% premium for the right (not obligation) to trade
// at the locked rate on expiry.
func (s *Service) CreateOption(ctx context.Context, in ContractIn) (*ContractOut, error) {
	// Money-moving entry points validate themselves; callers (tRPC, agent, worker) may not.
	if err := in.Validate(); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	var out ContractOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		rates, _, err := s.loadRates(ctx, tx)
		if err != nil {
			return err
		}
		rr, ok := rates[in.Currency]
		if !ok {
			return apperr.Bad("暂不支持该币种")
		}
		rate := pickRate(rr, in.Direction)
		premium := money.Pct(money.ForeignToCNY(in.ForeignAmountCents, rate), optionPremiumBps)
		if premium <= 0 {
			return apperr.Bad("名义本金过小")
		}
		expires := s.Now().UTC().AddDate(0, 0, in.TenorDays)
		res, err := tx.ExecContext(ctx, `INSERT INTO fx_contracts (user_id, type, currency, foreign_amount_cents, locked_rate_bps, direction, premium_cents, expires_at)
			VALUES (?, 'option', ?, ?, ?, ?, ?, ?)`, uid, in.Currency, in.ForeignAmountCents, rate, in.Direction, premium, expires)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := ledger.Debit(ctx, tx, acc, ledger.Posting{
			AmountCents: premium, Category: "金融衍生品", Counterparty: "灵犀银行金融市场部",
			Memo:    in.Currency + " 外汇期权费（名义本金 " + in.Currency + " " + money.YuanGrouped(in.ForeignAmountCents) + "）",
			BizType: "fx_option_premium", BizRef: itoa(id),
		}); err != nil {
			return err
		}
		out = ContractOut{
			ID: uint64(id), LockedRate: money.Rate(rate), PremiumText: money.Yuan(premium), ExpiresAt: expires,
			Message: "期权合约已生效：支付期权费 ¥" + money.YuanGrouped(premium) + "，获得 " + itoa(int64(in.TenorDays)) + " 天后按 " + money.Rate(rate) + " 交割的权利",
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type MessageOut struct {
	Message string `json:"message"`
}

func (s *Service) CancelFXContract(ctx context.Context, in IDIn) (*MessageOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	var out MessageOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		c, err := s.lockContract(ctx, tx, uid, in.ID)
		if err != nil {
			return err
		}
		if c.Status != "active" {
			return apperr.Bad("该合约已结束，无法取消")
		}
		if c.Type == "option" {
			return apperr.Bad("期权合约一经购买不可取消（期权费不退）")
		}
		if err := s.releaseForwardHolds(ctx, tx, acc, c); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE fx_contracts SET status = 'cancelled' WHERE id = ? AND user_id = ? AND status = 'active'`, c.ID, uid); err != nil {
			return err
		}
		out.Message = "合约已取消，保证金 ¥" + money.YuanGrouped(c.MarginCents) + " 已退回活期"
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// lockContract must be called after LockAccount (and after wallet locks).
func (s *Service) lockContract(ctx context.Context, tx *sql.Tx, uid, id uint64) (*FXContract, error) {
	c, err := scanFXC(tx.QueryRowContext(ctx, `SELECT `+fxcColumns+` FROM fx_contracts WHERE id = ? AND user_id = ? FOR UPDATE`, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundf("合约不存在")
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) releaseForwardHolds(ctx context.Context, tx *sql.Tx, acc *ledger.Account, c *FXContract) error {
	if err := ledger.Release(ctx, tx, acc, c.MarginCents); err != nil {
		return err
	}
	if c.Direction == "sell_foreign" {
		w, err := ledger.LockWallet(ctx, tx, acc.UserID, c.Currency)
		if err != nil {
			return err
		}
		return ledger.ReleaseForeign(ctx, tx, w, c.ForeignAmountCents)
	}
	return nil
}

// SettleExpiredFX is run by the worker. Forwards are delivered at the locked
// rate; options are exercised only when in the money and fundable, otherwise
// they lapse. Each contract settles in its own transaction.
func (s *Service) SettleExpiredFX(ctx context.Context) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id FROM fx_contracts WHERE status = 'active' AND expires_at <= ? ORDER BY expires_at LIMIT 50`, s.Now().UTC())
	if err != nil {
		return 0, err
	}
	type due struct{ id, uid uint64 }
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.uid); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()

	n := 0
	for _, d := range list {
		var note string
		err := s.tx(ctx, func(tx *sql.Tx) error {
			acc, err := ledger.LockAccount(ctx, tx, d.uid)
			if err != nil {
				return err
			}
			// lock the wallet before the contract row to keep lock order
			var ccy string
			if err := tx.QueryRowContext(ctx, `SELECT currency FROM fx_contracts WHERE id = ? AND user_id = ?`, d.id, d.uid).Scan(&ccy); err != nil {
				return err
			}
			w, err := ledger.LockWallet(ctx, tx, d.uid, ccy)
			if err != nil {
				return err
			}
			c, err := s.lockContract(ctx, tx, d.uid, d.id)
			if err != nil {
				return err
			}
			if c.Status != "active" {
				return nil
			}
			note, err = s.settleOne(ctx, tx, acc, w, c)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE fx_contracts SET status = 'settled' WHERE id = ? AND status = 'active'`, c.ID)
			return err
		})
		s.Audit.Write(ctx, auditEntry(d.uid, "worker.fxSettle", uintStr(d.id)+" "+note, err))
		if err != nil {
			slog.ErrorContext(ctx, "fx settle failed", "contractId", d.id, "error", err)
			continue // one bad contract must not block the rest
		}
		n++
	}
	return n, nil
}

func (s *Service) settleOne(ctx context.Context, tx *sql.Tx, acc *ledger.Account, w *ledger.Wallet, c *FXContract) (string, error) {
	cny := money.ForeignToCNY(c.ForeignAmountCents, c.LockedRateBps)
	ref := uintStr(c.ID)
	memo := c.Currency + " " + map[string]string{"forward": "远期", "option": "期权"}[c.Type] + "交割 @" + money.Rate(c.LockedRateBps)

	if c.Type == "forward" {
		if err := ledger.Release(ctx, tx, acc, c.MarginCents); err != nil {
			return "", err
		}
		if c.Direction == "sell_foreign" {
			if err := ledger.DebitForeign(ctx, tx, w, c.ForeignAmountCents, true, "fx_forward", ref, memo); err != nil {
				return "", err
			}
			_, err := ledger.Credit(ctx, tx, acc, ledger.Posting{AmountCents: cny, Category: "结汇", Counterparty: "灵犀银行金融市场部", Memo: memo, BizType: "fx_forward", BizRef: ref})
			return "delivered", err
		}
		if acc.Available() < cny {
			// Customer failed to fund a buy-side forward: margin is forfeited.
			_, err := ledger.Debit(ctx, tx, acc, ledger.Posting{AmountCents: c.MarginCents, Category: "金融衍生品", Counterparty: "灵犀银行金融市场部", Memo: memo + " 违约，扣收保证金", BizType: "fx_forward_default", BizRef: ref})
			return "defaulted", err
		}
		if _, err := ledger.Debit(ctx, tx, acc, ledger.Posting{AmountCents: cny, Category: "购汇", Counterparty: "灵犀银行金融市场部", Memo: memo, BizType: "fx_forward", BizRef: ref}); err != nil {
			return "", err
		}
		return "delivered", ledger.CreditForeign(ctx, tx, w, c.ForeignAmountCents, "fx_forward", ref, memo)
	}

	// option: exercise only if in the money at the current quote
	rates, _, err := s.loadRates(ctx, tx)
	if err != nil {
		return "", err
	}
	spot := pickRate(rates[c.Currency], c.Direction)
	if c.Direction == "sell_foreign" {
		if c.LockedRateBps <= spot || w.Available() < c.ForeignAmountCents {
			return "lapsed", nil
		}
		if err := ledger.DebitForeign(ctx, tx, w, c.ForeignAmountCents, false, "fx_option", ref, memo); err != nil {
			return "", err
		}
		_, err := ledger.Credit(ctx, tx, acc, ledger.Posting{AmountCents: cny, Category: "结汇", Counterparty: "灵犀银行金融市场部", Memo: memo, BizType: "fx_option", BizRef: ref})
		return "exercised", err
	}
	if c.LockedRateBps >= spot || acc.Available() < cny {
		return "lapsed", nil
	}
	if _, err := ledger.Debit(ctx, tx, acc, ledger.Posting{AmountCents: cny, Category: "购汇", Counterparty: "灵犀银行金融市场部", Memo: memo, BizType: "fx_option", BizRef: ref}); err != nil {
		return "", err
	}
	return "exercised", ledger.CreditForeign(ctx, tx, w, c.ForeignAmountCents, "fx_option", ref, memo)
}
