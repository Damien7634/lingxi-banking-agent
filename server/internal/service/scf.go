package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/db"
	"lingxi/server/internal/ledger"
	"lingxi/server/internal/money"
	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/trpc"
)

const minLoanCents = 100_000 // ¥1,000

type SCFProduct struct {
	ID             uint64  `json:"id"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	RateBps        int64   `json:"rateBps"`
	MaxAmountCents int64   `json:"maxAmountCents"`
	TermDays       int     `json:"termDays"`
	Tags           *string `json:"tags"`
	Description    *string `json:"description"`
}

const scfpColumns = `id, code, name, rate_bps, max_amount_cents, term_days, tags, description`

func scanSCFP(sc interface{ Scan(...any) error }) (SCFProduct, error) {
	var p SCFProduct
	var tags, desc sql.NullString
	err := sc.Scan(&p.ID, &p.Code, &p.Name, &p.RateBps, &p.MaxAmountCents, &p.TermDays, &tags, &desc)
	p.Tags, p.Description = ns(tags), ns(desc)
	return p, err
}

type SCFLoan struct {
	ID            uint64      `json:"id"`
	ProductID     uint64      `json:"productId"`
	AmountCents   int64       `json:"amountCents"`
	RateBps       int64       `json:"rateBps"`
	Purpose       *string     `json:"purpose"`
	RelatedRef    *string     `json:"relatedRef"`
	Status        string      `json:"status"`
	InterestCents int64       `json:"interestCents"`
	AppliedAt     time.Time   `json:"appliedAt"`
	DueAt         time.Time   `json:"dueAt"`
	RepaidAt      *time.Time  `json:"repaidAt"`
	Product       *SCFProduct `json:"product"`
}

func (s *Service) registerSCF(r *trpc.Router) {
	r.Query("scf.products", trpc.NoInput(s.SCFProducts))
	r.Query("scf.myLoans", trpc.NoInput(s.MyLoans))
	r.Mutation("scf.applyLoan", trpc.Typed(s.ApplyLoan))
	r.Mutation("scf.repay", trpc.Typed(s.RepayLoan))
}

func (s *Service) SCFProducts(ctx context.Context) ([]SCFProduct, error) {
	if _, err := reqctx.UserID(ctx); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+scfpColumns+` FROM scf_products ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SCFProduct{}
	for rows.Next() {
		p, err := scanSCFP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) getSCFProduct(ctx context.Context, q db.Querier, id uint64) (*SCFProduct, error) {
	p, err := scanSCFP(q.QueryRowContext(ctx, `SELECT `+scfpColumns+` FROM scf_products WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundf("产品不存在")
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Service) MyLoans(ctx context.Context) ([]SCFLoan, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT l.id, l.product_id, l.amount_cents, l.rate_bps, l.purpose, l.related_ref, l.status, l.interest_cents, l.applied_at, l.due_at, l.repaid_at,
		       p.id, p.code, p.name, p.rate_bps, p.max_amount_cents, p.term_days, p.tags, p.description
		FROM scf_loans l LEFT JOIN scf_products p ON p.id = l.product_id
		WHERE l.user_id = ? ORDER BY l.id DESC LIMIT 200`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SCFLoan{}
	for rows.Next() {
		var l SCFLoan
		var purpose, ref sql.NullString
		var repaid sql.NullTime
		var pid sql.NullInt64
		var code, name, tags, desc sql.NullString
		var prate, pmax, pterm sql.NullInt64
		if err := rows.Scan(&l.ID, &l.ProductID, &l.AmountCents, &l.RateBps, &purpose, &ref, &l.Status, &l.InterestCents, &l.AppliedAt, &l.DueAt, &repaid,
			&pid, &code, &name, &prate, &pmax, &pterm, &tags, &desc); err != nil {
			return nil, err
		}
		l.Purpose, l.RelatedRef, l.RepaidAt = ns(purpose), ns(ref), nt(repaid)
		if pid.Valid {
			l.Product = &SCFProduct{ID: uint64(pid.Int64), Code: code.String, Name: name.String, RateBps: prate.Int64, MaxAmountCents: pmax.Int64, TermDays: int(pterm.Int64), Tags: ns(tags), Description: ns(desc)}
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

type ApplyLoanIn struct {
	ProductID   uint64 `json:"productId"`
	AmountCents int64  `json:"amountCents"`
	Purpose     string `json:"purpose"`
	RelatedRef  string `json:"relatedRef"`
}

func (in *ApplyLoanIn) Validate() error {
	in.Purpose, in.RelatedRef = strings.TrimSpace(in.Purpose), strings.TrimSpace(in.RelatedRef)
	if err := checkID("产品", in.ProductID); err != nil {
		return err
	}
	if err := checkCents("融资金额", in.AmountCents, money.MaxCents); err != nil {
		return err
	}
	if in.AmountCents < minLoanCents {
		return apperr.Bad("单笔融资金额不低于 ¥%s", money.YuanGrouped(minLoanCents))
	}
	if err := checkLen("融资用途", in.Purpose, 0, 100); err != nil {
		return err
	}
	return checkLen("关联凭证", in.RelatedRef, 0, 64)
}

type ApplyLoanOut struct {
	ID         uint64    `json:"id"`
	Product    string    `json:"product"`
	AmountText string    `json:"amountText"`
	DueAt      time.Time `json:"dueAt"`
	Message    string    `json:"message"`
}

// ApplyLoan: demo environment approves instantly and disburses to the CNY
// account. One open loan per product per user; the account lock serialises
// concurrent applications so the check cannot be raced.
func (s *Service) ApplyLoan(ctx context.Context, in ApplyLoanIn) (*ApplyLoanOut, error) {
	// Money-moving entry points validate themselves; callers (tRPC, agent, worker) may not.
	if err := in.Validate(); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	var out ApplyLoanOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		p, err := s.getSCFProduct(ctx, tx, in.ProductID)
		if err != nil {
			return err
		}
		if in.AmountCents > p.MaxAmountCents {
			return apperr.Bad("超出该产品额度上限 ¥%s", money.YuanGrouped(p.MaxAmountCents))
		}
		var open int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scf_loans WHERE user_id = ? AND product_id = ? AND status IN ('disbursed','overdue')`, uid, p.ID).Scan(&open); err != nil {
			return err
		}
		if open > 0 {
			return apperr.Bad("「%s」存在在途融资，请先归还后再申请", p.Name)
		}
		var overdue int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scf_loans WHERE user_id = ? AND status = 'overdue'`, uid).Scan(&overdue); err != nil {
			return err
		}
		if overdue > 0 {
			return apperr.Bad("您有逾期未还的融资，暂不能申请新融资")
		}
		now := s.Now().UTC()
		due := now.AddDate(0, 0, p.TermDays)
		res, err := tx.ExecContext(ctx, `INSERT INTO scf_loans (user_id, product_id, amount_cents, rate_bps, purpose, related_ref, status, applied_at, due_at)
			VALUES (?, ?, ?, ?, ?, ?, 'disbursed', ?, ?)`, uid, p.ID, in.AmountCents, p.RateBps, nullable(in.Purpose), nullable(in.RelatedRef), now, due)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := ledger.Credit(ctx, tx, acc, ledger.Posting{
			AmountCents: in.AmountCents, Category: "融资放款", Counterparty: "灵犀银行供应链金融",
			Memo:    p.Name + " 放款（年化 " + bpsPct(p.RateBps) + "%）",
			BizType: "scf_disburse", BizRef: itoa(id),
		}); err != nil {
			return err
		}
		out = ApplyLoanOut{
			ID: uint64(id), Product: p.Name, AmountText: money.Yuan(in.AmountCents), DueAt: due,
			Message: "「" + p.Name + "」已审批通过并放款 ¥" + money.YuanGrouped(in.AmountCents) + " 至活期账户，期限 " + itoa(int64(p.TermDays)) + " 天",
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type RepayIn struct {
	LoanID uint64 `json:"loanId"`
}

func (in *RepayIn) Validate() error { return checkID("融资记录", in.LoanID) }

type RepayOut struct {
	Product       string `json:"product"`
	PrincipalText string `json:"principalText"`
	InterestText  string `json:"interestText"`
	Days          int64  `json:"days"`
	Message       string `json:"message"`
}

// LoanInterest = principal × rate × days / 365 with days = ceil(elapsed), min 1.
func LoanInterest(principal, rateBps int64, applied, now time.Time) (interest, days int64) {
	elapsed := now.Sub(applied)
	days = int64((elapsed + 24*time.Hour - 1) / (24 * time.Hour))
	if days < 1 {
		days = 1
	}
	return money.SimpleInterest(principal, rateBps, days), days
}

func (s *Service) RepayLoan(ctx context.Context, in RepayIn) (*RepayOut, error) {
	// Money-moving entry points validate themselves; callers (tRPC, agent, worker) may not.
	if err := in.Validate(); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	var out RepayOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		var productID uint64
		var amount, rate int64
		var status string
		var applied time.Time
		err = tx.QueryRowContext(ctx, `SELECT product_id, amount_cents, rate_bps, status, applied_at FROM scf_loans WHERE id = ? AND user_id = ? FOR UPDATE`, in.LoanID, uid).
			Scan(&productID, &amount, &rate, &status, &applied)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundf("融资记录不存在")
		}
		if err != nil {
			return err
		}
		if status == "repaid" {
			return apperr.Bad("该笔融资已结清")
		}
		name := "融资"
		if p, err := s.getSCFProduct(ctx, tx, productID); err == nil {
			name = p.Name
		}
		now := s.Now().UTC()
		interest, days := LoanInterest(amount, rate, applied, now)
		total, err := money.Add(amount, interest)
		if err != nil {
			return err
		}
		if _, err := ledger.Debit(ctx, tx, acc, ledger.Posting{
			AmountCents: total, Category: "融资还款", Counterparty: "灵犀银行供应链金融",
			Memo:    "归还「" + name + "」本金+利息（" + itoa(days) + " 天）",
			BizType: "scf_repay", BizRef: uintStr(in.LoanID),
		}); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE scf_loans SET status = 'repaid', interest_cents = ?, repaid_at = ? WHERE id = ? AND user_id = ? AND status IN ('disbursed','overdue')`,
			interest, now, in.LoanID, uid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return apperr.Conflictf("融资状态已变化，请刷新后重试")
		}
		out = RepayOut{
			Product: name, PrincipalText: money.Yuan(amount), InterestText: money.Yuan(interest), Days: days,
			Message: "已结清「" + name + "」：本金 ¥" + money.YuanGrouped(amount) + " + 利息 ¥" + money.YuanGrouped(interest),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkOverdueLoans flags loans past due; called by the worker.
func (s *Service) MarkOverdueLoans(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE scf_loans SET status = 'overdue' WHERE status = 'disbursed' AND due_at < ?`, s.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func bpsPct(bps int64) string {
	return itoa(bps/100) + "." + string([]byte{byte('0' + bps%100/10), byte('0' + bps%10)})
}
