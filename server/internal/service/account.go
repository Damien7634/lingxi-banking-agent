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

func (s *Service) registerAccount(r *trpc.Router) {
	r.Query("account.overview", trpc.NoInput(s.AccountOverview))
}

func (s *Service) registerTransfer(r *trpc.Router) {
	r.Query("transfer.searchContacts", trpc.Typed(func(ctx context.Context, in SearchContactsIn) ([]Contact, error) {
		return s.SearchContacts(ctx, in.Query)
	}))
	r.Query("transfer.listContacts", trpc.NoInput(s.ListContacts))
	r.Mutation("transfer.transfer", trpc.Typed(s.Transfer))
	r.Mutation("transfer.schedule", trpc.Typed(s.ScheduleTransfer))
	r.Mutation("transfer.cancelScheduled", trpc.Typed(s.CancelScheduled))
	r.Mutation("transfer.createAASplit", trpc.Typed(s.CreateAASplit))
	r.Query("transfer.listAASplits", trpc.NoInput(s.ListAASplits))
	r.Mutation("transfer.markAAPaid", trpc.Typed(s.MarkAAPaid))
}

// ================= account =================

type Overview struct {
	Account        Account             `json:"account"`
	AvailableCents int64               `json:"availableCents"`
	Recent         []Transaction       `json:"recent"`
	Scheduled      []ScheduledTransfer `json:"scheduled"`
}

func (s *Service) loadAccount(ctx context.Context, q db.Querier, uid uint64) (Account, error) {
	var a Account
	err := q.QueryRowContext(ctx, `SELECT id, holder_name, balance_cents, locked_cents, updated_at FROM accounts WHERE user_id = ?`, uid).
		Scan(&a.ID, &a.HolderName, &a.BalanceCents, &a.LockedCents, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, apperr.NotFoundf("账户不存在")
	}
	return a, err
}

func (s *Service) AccountOverview(ctx context.Context) (*Overview, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	acc, err := s.loadAccount(ctx, s.DB, uid)
	if err != nil {
		return nil, err
	}
	out := &Overview{Account: acc, AvailableCents: acc.BalanceCents - acc.LockedCents, Recent: []Transaction{}, Scheduled: []ScheduledTransfer{}}

	rows, err := s.DB.QueryContext(ctx, `SELECT `+txColumns+` FROM transactions WHERE user_id = ? ORDER BY occurred_at DESC, id DESC LIMIT 8`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTx(rows)
		if err != nil {
			return nil, err
		}
		out.Recent = append(out.Recent, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.Scheduled, err = s.listScheduled(ctx, uid, "pending")
	return out, err
}

func (s *Service) listScheduled(ctx context.Context, uid uint64, status string) ([]ScheduledTransfer, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT st.id, st.contact_id, st.amount_cents, st.memo, st.execute_at, st.status, st.fail_reason, st.created_at,
		       c.id, c.name, c.phone, c.bank_name, c.card_tail, c.relation, c.birthday, c.note
		FROM scheduled_transfers st LEFT JOIN contacts c ON c.id = st.contact_id AND c.user_id = st.user_id
		WHERE st.user_id = ? AND st.status = ? ORDER BY st.execute_at`, uid, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduledTransfer{}
	for rows.Next() {
		var st ScheduledTransfer
		var memo, reason sql.NullString
		var cid sql.NullInt64
		var cname, phone, bank, tail, rel, bday, note sql.NullString
		if err := rows.Scan(&st.ID, &st.ContactID, &st.AmountCents, &memo, &st.ExecuteAt, &st.Status, &reason, &st.CreatedAt,
			&cid, &cname, &phone, &bank, &tail, &rel, &bday, &note); err != nil {
			return nil, err
		}
		st.Memo, st.FailReason = ns(memo), ns(reason)
		if cid.Valid {
			st.Contact = &Contact{ID: uint64(cid.Int64), Name: cname.String, Phone: ns(phone), BankName: ns(bank), CardTail: ns(tail), Relation: ns(rel), Birthday: ns(bday), Note: ns(note)}
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ================= contacts =================

type SearchContactsIn struct {
	Query string `json:"query"`
}

func (in *SearchContactsIn) Validate() error {
	in.Query = strings.TrimSpace(in.Query)
	return checkLen("搜索关键词", in.Query, 1, 64)
}

// SearchContacts matches name / phone / note / relation / card tail (LIKE input escaped).
func (s *Service) SearchContacts(ctx context.Context, query string) ([]Contact, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	q := "%" + db.EscapeLike(strings.TrimSpace(query)) + "%"
	return s.queryContacts(ctx, `SELECT `+contactColumns+` FROM contacts WHERE user_id = ? AND
		(name LIKE ? OR phone LIKE ? OR note LIKE ? OR relation LIKE ? OR card_tail LIKE ?) ORDER BY id LIMIT 20`,
		uid, q, q, q, q, q)
}

func (s *Service) ListContacts(ctx context.Context) ([]Contact, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	return s.queryContacts(ctx, `SELECT `+contactColumns+` FROM contacts WHERE user_id = ? ORDER BY id`, uid)
}

func (s *Service) queryContacts(ctx context.Context, q string, args ...any) ([]Contact, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Contact{}
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) getContact(ctx context.Context, q db.Querier, uid, id uint64) (*Contact, error) {
	c, err := scanContact(q.QueryRowContext(ctx, `SELECT `+contactColumns+` FROM contacts WHERE id = ? AND user_id = ?`, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundf("收款人不存在")
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ================= transfer =================

type TransferIn struct {
	ContactID   uint64 `json:"contactId"`
	AmountCents int64  `json:"amountCents"`
	Memo        string `json:"memo"`
}

func (in *TransferIn) Validate() error {
	in.Memo = strings.TrimSpace(in.Memo)
	if err := checkID("收款人", in.ContactID); err != nil {
		return err
	}
	if err := checkCents("转账金额", in.AmountCents, money.MaxCents); err != nil {
		return err
	}
	return checkLen("备注", in.Memo, 0, 50)
}

type TransferOut struct {
	TxID         uint64   `json:"txId"`
	BalanceCents int64    `json:"balanceCents"`
	Contact      *Contact `json:"contact"`
	AmountText   string   `json:"amountText"`
}

// Transfer is an immediate payment to a saved payee, bounded by the single
// and daily limits. The account row lock serialises it against every other
// money movement of the same user.
func (s *Service) Transfer(ctx context.Context, in TransferIn) (*TransferOut, error) {
	// Money-moving entry points validate themselves; callers (tRPC, agent, worker) may not.
	if err := in.Validate(); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	if in.AmountCents > s.Cfg.SingleTransferLimitCents {
		return nil, apperr.Bad("单笔转账不能超过 ¥%s", money.YuanGrouped(s.Cfg.SingleTransferLimitCents))
	}
	var out TransferOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		c, err := s.getContact(ctx, tx, uid, in.ContactID)
		if err != nil {
			return err
		}
		txID, err := s.debitTransfer(ctx, tx, acc, c, in.AmountCents, in.Memo, "手机银行")
		if err != nil {
			return err
		}
		out = TransferOut{TxID: txID, BalanceCents: acc.BalanceCents, Contact: c, AmountText: money.Yuan(in.AmountCents)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// debitTransfer enforces the daily cap then debits. Caller holds the account lock.
func (s *Service) debitTransfer(ctx context.Context, tx *sql.Tx, acc *ledger.Account, c *Contact, amount int64, memo, channel string) (uint64, error) {
	used, err := s.transferredToday(ctx, tx, acc.UserID)
	if err != nil {
		return 0, err
	}
	if used+amount > s.Cfg.DailyTransferLimitCents {
		return 0, apperr.Bad("超出单日转账限额 ¥%s（今日已转 ¥%s）",
			money.YuanGrouped(s.Cfg.DailyTransferLimitCents), money.YuanGrouped(used))
	}
	if memo == "" {
		memo = "转账给" + c.Name
	}
	return ledger.Debit(ctx, tx, acc, ledger.Posting{
		AmountCents: amount, Category: "转账", Counterparty: c.Name, Memo: memo, Channel: channel,
		BizType: "transfer", BizRef: uintStr(c.ID),
	})
}

func (s *Service) transferredToday(ctx context.Context, q db.Querier, uid uint64) (int64, error) {
	now := s.local(s.Now())
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.Cfg.Location).UTC()
	var sum sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT SUM(amount_cents) FROM transactions
		WHERE user_id = ? AND direction = 'out' AND biz_type = 'transfer' AND occurred_at >= ?`, uid, start).Scan(&sum)
	return sum.Int64, err
}

// ================= scheduled =================

type ScheduleIn struct {
	ContactID   uint64    `json:"contactId"`
	AmountCents int64     `json:"amountCents"`
	Memo        string    `json:"memo"`
	ExecuteAt   time.Time `json:"executeAt"`
}

func (in *ScheduleIn) Validate() error {
	t := TransferIn{ContactID: in.ContactID, AmountCents: in.AmountCents, Memo: in.Memo}
	if err := t.Validate(); err != nil {
		return err
	}
	in.Memo = t.Memo
	if in.ExecuteAt.IsZero() {
		return apperr.Bad("请选择执行时间")
	}
	return nil
}

type ScheduleOut struct {
	ID         uint64    `json:"id"`
	Contact    *Contact  `json:"contact"`
	ExecuteAt  time.Time `json:"executeAt"`
	AmountText string    `json:"amountText"`
}

func (s *Service) ScheduleTransfer(ctx context.Context, in ScheduleIn) (*ScheduleOut, error) {
	// Money-moving entry points validate themselves; callers (tRPC, agent, worker) may not.
	if err := in.Validate(); err != nil {
		return nil, err
	}
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	if !in.ExecuteAt.After(now) {
		return nil, apperr.Bad("执行时间必须晚于当前时间")
	}
	if in.ExecuteAt.After(now.AddDate(1, 0, 0)) {
		return nil, apperr.Bad("定时转账最长可预约 1 年")
	}
	if in.AmountCents > s.Cfg.SingleTransferLimitCents {
		return nil, apperr.Bad("单笔转账不能超过 ¥%s", money.YuanGrouped(s.Cfg.SingleTransferLimitCents))
	}
	var pending int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_transfers WHERE user_id = ? AND status = 'pending'`, uid).Scan(&pending); err != nil {
		return nil, err
	}
	if pending >= 50 {
		return nil, apperr.Bad("待执行的定时转账已达上限（50 笔）")
	}
	c, err := s.getContact(ctx, s.DB, uid, in.ContactID)
	if err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO scheduled_transfers (user_id, contact_id, amount_cents, memo, execute_at) VALUES (?, ?, ?, ?, ?)`,
		uid, c.ID, in.AmountCents, nullable(in.Memo), in.ExecuteAt.UTC())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &ScheduleOut{ID: uint64(id), Contact: c, ExecuteAt: in.ExecuteAt, AmountText: money.Yuan(in.AmountCents)}, nil
}

type IDIn struct {
	ID uint64 `json:"id"`
}

func (in *IDIn) Validate() error { return checkID("ID", in.ID) }

type OKOut struct {
	OK bool `json:"ok"`
}

func (s *Service) CancelScheduled(ctx context.Context, in IDIn) (*OKOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE scheduled_transfers SET status = 'cancelled' WHERE id = ? AND user_id = ? AND status = 'pending'`, in.ID, uid)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, apperr.NotFoundf("定时转账不存在或已执行")
	}
	return &OKOut{OK: true}, nil
}

// RunDueScheduled executes due transfers; called by the worker. Each transfer
// is its own transaction: account lock → re-check status under row lock →
// debit → mark executed. Safe with multiple instances.
func (s *Service) RunDueScheduled(ctx context.Context) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id FROM scheduled_transfers WHERE status = 'pending' AND execute_at <= ? ORDER BY execute_at LIMIT 50`, s.Now().UTC())
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

	done := 0
	for _, d := range list {
		var bizErr error
		err := s.tx(ctx, func(tx *sql.Tx) error {
			bizErr = nil
			acc, err := ledger.LockAccount(ctx, tx, d.uid)
			if err != nil {
				return err
			}
			var contactID uint64
			var amount int64
			var memo sql.NullString
			err = tx.QueryRowContext(ctx, `SELECT contact_id, amount_cents, memo FROM scheduled_transfers WHERE id = ? AND user_id = ? AND status = 'pending' FOR UPDATE`, d.id, d.uid).
				Scan(&contactID, &amount, &memo)
			if errors.Is(err, sql.ErrNoRows) {
				return nil // cancelled or handled by another instance
			}
			if err != nil {
				return err
			}
			c, err := s.getContact(ctx, tx, d.uid, contactID)
			if err == nil {
				var txID uint64
				txID, err = s.debitTransfer(ctx, tx, acc, c, amount, memo.String, "定时转账")
				if err == nil {
					_, err = tx.ExecContext(ctx, `UPDATE scheduled_transfers SET status = 'executed', tx_id = ? WHERE id = ?`, txID, d.id)
					return err
				}
			}
			if e, ok := apperr.As(err); ok {
				bizErr = e
				_, err = tx.ExecContext(ctx, `UPDATE scheduled_transfers SET status = 'failed', fail_reason = ? WHERE id = ?`, clipStr(e.Message, 255), d.id)
			}
			return err
		})
		if err != nil {
			return done, err
		}
		s.Audit.Write(ctx, auditEntry(d.uid, "worker.scheduledTransfer", uintStr(d.id), bizErr))
		if bizErr == nil {
			done++
		}
	}
	return done, nil
}

// ================= AA =================

type AAParticipantIn struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

type AAIn struct {
	Title        string            `json:"title"`
	TotalCents   int64             `json:"totalCents"`
	Participants []AAParticipantIn `json:"participants"`
}

func (in *AAIn) Validate() error {
	in.Title = strings.TrimSpace(in.Title)
	if err := checkLen("标题", in.Title, 1, 40); err != nil {
		return err
	}
	if err := checkCents("总金额", in.TotalCents, 10_000_000); err != nil {
		return err
	}
	if len(in.Participants) < 1 || len(in.Participants) > 20 {
		return apperr.Bad("参与人数需为 1-20 人")
	}
	seen := map[string]bool{}
	for i := range in.Participants {
		p := &in.Participants[i]
		p.Name, p.Phone = strings.TrimSpace(p.Name), strings.TrimSpace(p.Phone)
		if err := checkLen("参与者姓名", p.Name, 1, 20); err != nil {
			return err
		}
		if err := checkLen("手机号", p.Phone, 0, 20); err != nil {
			return err
		}
		if seen[p.Name] {
			return apperr.Bad("参与者「%s」重复", p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

type AAOut struct {
	ID           uint64          `json:"id"`
	PerPerson    string          `json:"perPerson"`
	Participants []AAParticipant `json:"participants"`
}

func (s *Service) CreateAASplit(ctx context.Context, in AAIn) (*AAOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	n := int64(len(in.Participants))
	per := in.TotalCents / (n + 1) // initiator pays one share too
	list := make([]AAParticipant, n)
	for i, p := range in.Participants {
		amt := per
		if i == 0 { // remainder goes to the first participant
			amt = in.TotalCents - per*n
		}
		list[i] = AAParticipant{Name: p.Name, Phone: p.Phone, AmountCents: amt}
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO aa_splits (user_id, title, total_cents, participants) VALUES (?, ?, ?, ?)`,
		uid, in.Title, in.TotalCents, mustJSON(list))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &AAOut{ID: uint64(id), PerPerson: money.Yuan(per), Participants: list}, nil
}

func (s *Service) ListAASplits(ctx context.Context) ([]AASplit, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, title, total_cents, participants, status, created_at FROM aa_splits WHERE user_id = ? ORDER BY created_at DESC LIMIT 100`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AASplit{}
	for rows.Next() {
		var a AASplit
		var raw []byte
		if err := rows.Scan(&a.ID, &a.Title, &a.TotalCents, &raw, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		if err := jsonUnmarshal(raw, &a.Participants); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type MarkAAIn struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

func (in *MarkAAIn) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if err := checkID("AA 收款", in.ID); err != nil {
		return err
	}
	return checkLen("参与者姓名", in.Name, 1, 20)
}

type MarkAAOut struct {
	OK   bool `json:"ok"`
	Done bool `json:"done"`
}

func (s *Service) MarkAAPaid(ctx context.Context, in MarkAAIn) (*MarkAAOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	var out MarkAAOut
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var raw []byte
		var status string
		err := tx.QueryRowContext(ctx, `SELECT participants, status FROM aa_splits WHERE id = ? AND user_id = ? FOR UPDATE`, in.ID, uid).Scan(&raw, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundf("AA 收款不存在")
		}
		if err != nil {
			return err
		}
		if status != "collecting" {
			return apperr.Bad("该 AA 收款已结束")
		}
		var list []AAParticipant
		if err := jsonUnmarshal(raw, &list); err != nil {
			return err
		}
		found := false
		done := true
		for i := range list {
			if list[i].Name == in.Name {
				list[i].Paid = true
				found = true
			}
			done = done && list[i].Paid
		}
		if !found {
			return apperr.Bad("参与者中没有「%s」", in.Name)
		}
		next := "collecting"
		if done {
			next = "done"
		}
		_, err = tx.ExecContext(ctx, `UPDATE aa_splits SET participants = ?, status = ? WHERE id = ? AND user_id = ?`, mustJSON(list), next, in.ID, uid)
		out = MarkAAOut{OK: true, Done: done}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
