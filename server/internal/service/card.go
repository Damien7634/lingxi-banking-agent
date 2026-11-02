package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strings"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/ledger"
	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/trpc"
)

const (
	maxActiveCards      = 10
	defaultCreditLimit  = int64(3000000)
	maxCreditLimitCents = int64(20000000)
	maxDailyLimitCents  = int64(50000000)
)

func (s *Service) registerCard(r *trpc.Router) {
	r.Query("card.list", trpc.NoInput(s.ListCards))
	r.Mutation("card.applyCard", trpc.Typed(s.ApplyCard))
	r.Mutation("card.adjustLimit", trpc.Typed(s.AdjustLimit))
	r.Mutation("card.setStatus", trpc.Typed(s.SetCardStatus))
	r.Mutation("card.toggleOnlinePay", trpc.Typed(s.ToggleOnlinePay))
}

const cardColumns = `id, card_type, bank_name, last4, credit_limit_cents, used_cents, daily_limit_cents, status, online_pay_enabled, created_at`

func scanCard(sc interface{ Scan(...any) error }) (Card, error) {
	var c Card
	var limit sql.NullInt64
	err := sc.Scan(&c.ID, &c.CardType, &c.BankName, &c.Last4, &limit, &c.UsedCents, &c.DailyLimitCents, &c.Status, &c.OnlinePayEnabled, &c.CreatedAt)
	c.CreditLimitCents = ni(limit)
	return c, err
}

// ListCards returns the current user's cards, newest first.
func (s *Service) ListCards(ctx context.Context) ([]Card, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cardColumns+` FROM cards WHERE user_id = ? ORDER BY created_at DESC, id DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// loadCard fetches one card owned by uid.
func (s *Service) loadCard(ctx context.Context, uid, id uint64) (Card, error) {
	c, err := scanCard(s.DB.QueryRowContext(ctx, `SELECT `+cardColumns+` FROM cards WHERE id = ? AND user_id = ?`, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return c, apperr.NotFoundf("卡片不存在")
	}
	return c, err
}

// ================= apply =================

type ApplyCardIn struct {
	CardType   string  `json:"cardType"`
	HolderName *string `json:"holderName,omitempty"`
}

func (in *ApplyCardIn) Validate() error {
	if in.CardType != "debit" && in.CardType != "credit" {
		return apperr.Bad("卡片类型无效")
	}
	if in.HolderName != nil {
		v := strings.TrimSpace(*in.HolderName)
		in.HolderName = &v
		if err := checkLen("持卡人姓名", v, 1, 30); err != nil {
			return err
		}
	}
	return nil
}

type ApplyCardOut struct {
	ID       uint64 `json:"id"`
	Last4    string `json:"last4"`
	CardType string `json:"cardType"`
	Message  string `json:"message"`
}

func randomLast4() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(9000))
	if err != nil {
		return "", err
	}
	return uintStr(uint64(n.Int64()) + 1000), nil
}

func (s *Service) ApplyCard(ctx context.Context, in ApplyCardIn) (*ApplyCardOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	last4, err := randomLast4()
	if err != nil {
		return nil, err
	}
	var credit any
	msg := "借记卡开立成功"
	if in.CardType == "credit" {
		credit = defaultCreditLimit
		msg = "信用卡申请已通过预审，初始额度 ¥30,000"
	}
	var id uint64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		// The account row lock serialises concurrent applications of one user,
		// so the card-count cap cannot be bypassed by parallel requests.
		if _, err := ledger.LockAccount(ctx, tx, uid); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cards WHERE user_id = ? AND status <> 'lost'`, uid).Scan(&n); err != nil {
			return err
		}
		if n >= maxActiveCards {
			return apperr.Bad("最多可持有 %d 张有效卡片，请先处理不用的卡片", maxActiveCards)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO cards (user_id, card_type, bank_name, last4, credit_limit_cents, status) VALUES (?, ?, '灵犀银行', ?, ?, 'normal')`,
			uid, in.CardType, last4, credit)
		if err != nil {
			return err
		}
		lid, err := res.LastInsertId()
		id = uint64(lid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &ApplyCardOut{ID: id, Last4: last4, CardType: in.CardType, Message: msg}, nil
}

// ================= adjust limit =================

type AdjustLimitIn struct {
	CardID           uint64 `json:"cardId"`
	CreditLimitCents *int64 `json:"creditLimitCents,omitempty"`
	DailyLimitCents  *int64 `json:"dailyLimitCents,omitempty"`
}

func (in *AdjustLimitIn) Validate() error {
	if err := checkID("卡片", in.CardID); err != nil {
		return err
	}
	if in.CreditLimitCents != nil {
		if err := checkCents("信用额度", *in.CreditLimitCents, maxCreditLimitCents); err != nil {
			return err
		}
	}
	if in.DailyLimitCents != nil {
		if err := checkCents("每日限额", *in.DailyLimitCents, maxDailyLimitCents); err != nil {
			return err
		}
	}
	return nil
}

type AdjustLimitOut struct {
	OK               bool   `json:"ok"`
	CardID           uint64 `json:"cardId"`
	CreditLimitCents *int64 `json:"creditLimitCents,omitempty"`
	DailyLimitCents  *int64 `json:"dailyLimitCents,omitempty"`
}

func (s *Service) AdjustLimit(ctx context.Context, in AdjustLimitIn) (*AdjustLimitOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	card, err := s.loadCard(ctx, uid, in.CardID)
	if err != nil {
		return nil, err
	}
	if card.Status == "lost" {
		return nil, apperr.Bad("卡片已挂失，不能调整额度")
	}
	var credit, daily *int64
	if in.CreditLimitCents != nil && card.CardType == "credit" {
		if *in.CreditLimitCents < card.UsedCents {
			return nil, apperr.Bad("信用额度不能低于已用额度")
		}
		credit = in.CreditLimitCents
	}
	if in.DailyLimitCents != nil {
		daily = in.DailyLimitCents
	}
	if credit == nil && daily == nil {
		return nil, apperr.Bad("没有可调整的额度项")
	}
	sets := []string{}
	args := []any{}
	if credit != nil {
		sets = append(sets, "credit_limit_cents = ?")
		args = append(args, *credit)
	}
	if daily != nil {
		sets = append(sets, "daily_limit_cents = ?")
		args = append(args, *daily)
	}
	args = append(args, in.CardID, uid)
	q := `UPDATE cards SET ` + strings.Join(sets, ", ") + ` WHERE id = ? AND user_id = ? AND status <> 'lost'`
	if credit != nil {
		// Re-check against used_cents atomically (used_cents may change concurrently).
		q += ` AND used_cents <= ?`
		args = append(args, *credit)
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// MySQL reports 0 for no-op updates too; distinguish via reload.
		cur, err := s.loadCard(ctx, uid, in.CardID)
		if err != nil {
			return nil, err
		}
		if cur.Status == "lost" {
			return nil, apperr.Bad("卡片已挂失，不能调整额度")
		}
		if credit != nil && *credit < cur.UsedCents {
			return nil, apperr.Bad("信用额度不能低于已用额度")
		}
	}
	return &AdjustLimitOut{OK: true, CardID: in.CardID, CreditLimitCents: credit, DailyLimitCents: daily}, nil
}

// ================= status =================

type SetCardStatusIn struct {
	CardID uint64 `json:"cardId"`
	Action string `json:"action"`
}

func (in *SetCardStatusIn) Validate() error {
	if err := checkID("卡片", in.CardID); err != nil {
		return err
	}
	if _, ok := cardActions[in.Action]; !ok {
		return apperr.Bad("操作类型无效")
	}
	return nil
}

type cardAction struct {
	to    string
	label string
}

var cardActions = map[string]cardAction{
	"freeze":     {"frozen", "临时冻结"},
	"unfreeze":   {"normal", "解除冻结"},
	"reportLost": {"lost", "挂失"},
	"restrict":   {"restricted", "限制交易"},
	"unrestrict": {"normal", "解除交易限制"},
}

var cardStatusText = map[string]string{
	"normal": "正常", "frozen": "冻结", "restricted": "限制交易", "lost": "挂失",
}

type SetCardStatusOut struct {
	OK      bool   `json:"ok"`
	CardID  uint64 `json:"cardId"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (s *Service) SetCardStatus(ctx context.Context, in SetCardStatusIn) (*SetCardStatusOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	act, ok := cardActions[in.Action]
	if !ok {
		return nil, apperr.Bad("操作类型无效")
	}
	card, err := s.loadCard(ctx, uid, in.CardID)
	if err != nil {
		return nil, err
	}
	if card.Status == "lost" {
		return nil, apperr.Bad("卡片已挂失，请补办新卡")
	}
	var required string // status the card must currently be in; "" = any non-lost
	switch in.Action {
	case "unfreeze":
		required = "frozen"
	case "unrestrict":
		required = "restricted"
	case "freeze", "restrict":
		required = "normal"
	}
	if required != "" && card.Status != required {
		return nil, apperr.Bad("当前卡片状态为「%s」，无法%s", cardStatusText[card.Status], act.label)
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE cards SET status = ? WHERE id = ? AND user_id = ? AND status = ?`, act.to, in.CardID, uid, card.Status)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, apperr.Bad("卡片状态已变化，请刷新后重试")
	}
	return &SetCardStatusOut{OK: true, CardID: in.CardID, Status: act.to, Message: "尾号 " + card.Last4 + " 已" + act.label}, nil
}

// ================= online pay =================

type ToggleOnlinePayIn struct {
	CardID  uint64 `json:"cardId"`
	Enabled bool   `json:"enabled"`
}

func (in *ToggleOnlinePayIn) Validate() error { return checkID("卡片", in.CardID) }

func (s *Service) ToggleOnlinePay(ctx context.Context, in ToggleOnlinePayIn) (*OKOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	card, err := s.loadCard(ctx, uid, in.CardID)
	if err != nil {
		return nil, err
	}
	if card.Status == "lost" {
		return nil, apperr.Bad("卡片已挂失，无法修改网上支付设置")
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE cards SET online_pay_enabled = ? WHERE id = ? AND user_id = ? AND status <> 'lost'`, in.Enabled, in.CardID, uid); err != nil {
		return nil, err
	}
	return &OKOut{OK: true}, nil
}
