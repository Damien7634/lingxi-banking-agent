// Package ledger is the only code allowed to change balances.
//
// Rules every caller must follow:
//  1. All calls happen inside db.WithTx; the account row is locked first with
//     LockAccount (SELECT ... FOR UPDATE), which serialises all money movement
//     for that user.
//  2. Lock order is fixed: account → fx wallet(s) → business rows. Never lock a
//     business row before the account, or concurrent requests can deadlock.
//  3. Every balance change writes a transactions row with balance_after, so the
//     journal can always be replayed against the account.
//
// The DB CHECK constraints (balance >= locked >= 0) are a second line of
// defence; the checks here produce the user-facing error first.
package ledger

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/money"
	"lingxi/server/internal/reqctx"
)

type Account struct {
	ID           uint64
	UserID       uint64
	HolderName   string
	BalanceCents int64
	LockedCents  int64
}

func (a *Account) Available() int64 { return a.BalanceCents - a.LockedCents }

// LockAccount loads and row-locks the user's account.
func LockAccount(ctx context.Context, tx *sql.Tx, userID uint64) (*Account, error) {
	a := &Account{UserID: userID}
	err := tx.QueryRowContext(ctx,
		`SELECT id, holder_name, balance_cents, locked_cents FROM accounts WHERE user_id = ? FOR UPDATE`, userID).
		Scan(&a.ID, &a.HolderName, &a.BalanceCents, &a.LockedCents)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundf("账户不存在")
	}
	return a, err
}

type Posting struct {
	AmountCents  int64
	Category     string
	Counterparty string
	Memo         string
	Channel      string
	BizType      string
	BizRef       string
}

func (p *Posting) validate() error {
	if p.AmountCents <= 0 || p.AmountCents > money.MaxCents {
		return apperr.Bad("金额必须为正数")
	}
	if p.Channel == "" {
		p.Channel = "手机银行"
	}
	if p.Category == "" {
		p.Category = "其他"
	}
	p.Counterparty = clip(p.Counterparty, 128)
	p.Memo = clip(p.Memo, 255)
	return nil
}

// Debit spends from available balance (balance − locked).
func Debit(ctx context.Context, tx *sql.Tx, a *Account, p Posting) (txID uint64, err error) {
	if err := p.validate(); err != nil {
		return 0, err
	}
	if a.Available() < p.AmountCents {
		return 0, apperr.Bad("可用余额不足：当前可用 ¥%s，需支付 ¥%s（另有 ¥%s 被冻结或专款锁定）",
			money.YuanGrouped(a.Available()), money.YuanGrouped(p.AmountCents), money.YuanGrouped(a.LockedCents))
	}
	newBal := a.BalanceCents - p.AmountCents
	if err := setBalance(ctx, tx, a, newBal, a.LockedCents); err != nil {
		return 0, err
	}
	return journal(ctx, tx, a, "out", p)
}

// Credit adds to the balance.
func Credit(ctx context.Context, tx *sql.Tx, a *Account, p Posting) (txID uint64, err error) {
	if err := p.validate(); err != nil {
		return 0, err
	}
	newBal, err := money.Add(a.BalanceCents, p.AmountCents)
	if err != nil {
		return 0, apperr.Bad("金额超出上限")
	}
	if err := setBalance(ctx, tx, a, newBal, a.LockedCents); err != nil {
		return 0, err
	}
	return journal(ctx, tx, a, "in", p)
}

// Hold earmarks available funds (margin, scenario lock). No journal row: the
// money has not left the account.
func Hold(ctx context.Context, tx *sql.Tx, a *Account, cents int64, purpose string) error {
	if cents <= 0 {
		return apperr.Bad("冻结金额必须为正数")
	}
	if a.Available() < cents {
		return apperr.Bad("可用余额不足以%s：可用 ¥%s，需冻结 ¥%s", purpose, money.YuanGrouped(a.Available()), money.YuanGrouped(cents))
	}
	return setBalance(ctx, tx, a, a.BalanceCents, a.LockedCents+cents)
}

// Release returns earmarked funds. Releasing more than is locked is a bug,
// not a user error, so it fails instead of clamping.
func Release(ctx context.Context, tx *sql.Tx, a *Account, cents int64) error {
	if cents <= 0 {
		return nil
	}
	if cents > a.LockedCents {
		return errors.New("ledger: release exceeds locked amount")
	}
	return setBalance(ctx, tx, a, a.BalanceCents, a.LockedCents-cents)
}

// DebitLocked pays out of previously held funds (e.g. a scenario paying for
// gifts from its own lock): it reduces both locked and balance.
func DebitLocked(ctx context.Context, tx *sql.Tx, a *Account, p Posting) (uint64, error) {
	if err := p.validate(); err != nil {
		return 0, err
	}
	if p.AmountCents > a.LockedCents {
		return 0, errors.New("ledger: debit-locked exceeds locked amount")
	}
	if err := setBalance(ctx, tx, a, a.BalanceCents-p.AmountCents, a.LockedCents-p.AmountCents); err != nil {
		return 0, err
	}
	return journal(ctx, tx, a, "out", p)
}

func setBalance(ctx context.Context, tx *sql.Tx, a *Account, balance, locked int64) error {
	if balance < 0 || locked < 0 || locked > balance {
		return errors.New("ledger: invariant violated")
	}
	res, err := tx.ExecContext(ctx, `UPDATE accounts SET balance_cents = ?, locked_cents = ? WHERE id = ? AND user_id = ?`,
		balance, locked, a.ID, a.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("ledger: account update affected unexpected rows")
	}
	a.BalanceCents, a.LockedCents = balance, locked
	return nil
}

func journal(ctx context.Context, tx *sql.Tx, a *Account, dir string, p Posting) (uint64, error) {
	var memo any
	if p.Memo != "" {
		memo = p.Memo
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO transactions
		(user_id, account_id, direction, amount_cents, balance_after_cents, category, counterparty, memo, channel, biz_type, biz_ref, request_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.UserID, a.ID, dir, p.AmountCents, a.BalanceCents, p.Category, p.Counterparty, memo, p.Channel, p.BizType, p.BizRef,
		reqctx.MetaFrom(ctx).RequestID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

// ---- FX wallets ----

type Wallet struct {
	ID           uint64
	UserID       uint64
	Currency     string
	BalanceCents int64
	LockedCents  int64
}

func (w *Wallet) Available() int64 { return w.BalanceCents - w.LockedCents }

// LockWallet row-locks a currency wallet. Call only after LockAccount.
func LockWallet(ctx context.Context, tx *sql.Tx, userID uint64, ccy string) (*Wallet, error) {
	w := &Wallet{UserID: userID, Currency: ccy}
	err := tx.QueryRowContext(ctx,
		`SELECT id, balance_foreign_cents, locked_foreign_cents FROM fx_wallets WHERE user_id = ? AND currency = ? FOR UPDATE`, userID, ccy).
		Scan(&w.ID, &w.BalanceCents, &w.LockedCents)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.Bad("未开通 %s 钱包", ccy)
	}
	return w, err
}

func HoldForeign(ctx context.Context, tx *sql.Tx, w *Wallet, cents int64) error {
	if cents <= 0 {
		return apperr.Bad("金额必须为正数")
	}
	if w.Available() < cents {
		return apperr.Bad("%s 钱包可用余额不足：可用 %s，需锁定 %s", w.Currency, money.YuanGrouped(w.Available()), money.YuanGrouped(cents))
	}
	return setWallet(ctx, tx, w, w.BalanceCents, w.LockedCents+cents)
}

func ReleaseForeign(ctx context.Context, tx *sql.Tx, w *Wallet, cents int64) error {
	if cents <= 0 {
		return nil
	}
	if cents > w.LockedCents {
		return errors.New("ledger: foreign release exceeds locked")
	}
	return setWallet(ctx, tx, w, w.BalanceCents, w.LockedCents-cents)
}

// DebitForeign removes foreign funds. fromLocked=true consumes a prior hold
// (forward settlement); otherwise it spends available balance.
func DebitForeign(ctx context.Context, tx *sql.Tx, w *Wallet, cents int64, fromLocked bool, bizType, bizRef, memo string) error {
	if cents <= 0 {
		return apperr.Bad("金额必须为正数")
	}
	locked := w.LockedCents
	if fromLocked {
		if cents > w.LockedCents {
			return errors.New("ledger: foreign debit exceeds locked")
		}
		locked -= cents
	} else if w.Available() < cents {
		return apperr.Bad("%s 钱包可用余额不足", w.Currency)
	}
	if err := setWallet(ctx, tx, w, w.BalanceCents-cents, locked); err != nil {
		return err
	}
	return walletJournal(ctx, tx, w, "out", cents, bizType, bizRef, memo)
}

func CreditForeign(ctx context.Context, tx *sql.Tx, w *Wallet, cents int64, bizType, bizRef, memo string) error {
	if cents <= 0 {
		return apperr.Bad("金额必须为正数")
	}
	nb, err := money.Add(w.BalanceCents, cents)
	if err != nil {
		return apperr.Bad("金额超出上限")
	}
	if err := setWallet(ctx, tx, w, nb, w.LockedCents); err != nil {
		return err
	}
	return walletJournal(ctx, tx, w, "in", cents, bizType, bizRef, memo)
}

func walletJournal(ctx context.Context, tx *sql.Tx, w *Wallet, dir string, cents int64, bizType, bizRef, memo string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO fx_wallet_journal
		(user_id, wallet_id, currency, direction, amount_cents, balance_after_cents, biz_type, biz_ref, memo, request_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.UserID, w.ID, w.Currency, dir, cents, w.BalanceCents, bizType, bizRef, clip(memo, 255), reqctx.MetaFrom(ctx).RequestID, time.Now().UTC())
	return err
}

func setWallet(ctx context.Context, tx *sql.Tx, w *Wallet, balance, locked int64) error {
	if balance < 0 || locked < 0 || locked > balance {
		return errors.New("ledger: wallet invariant violated")
	}
	res, err := tx.ExecContext(ctx, `UPDATE fx_wallets SET balance_foreign_cents = ?, locked_foreign_cents = ? WHERE id = ? AND user_id = ?`,
		balance, locked, w.ID, w.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("ledger: wallet update affected unexpected rows")
	}
	w.BalanceCents, w.LockedCents = balance, locked
	return nil
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
