package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/ledger"
	"lingxi/server/internal/money"
	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/trpc"
)

// Birthday guard state machine:
//
//	armed   → locked   on entering the birthday month: hold lockCents of CNY
//	locked  → ordered  N days before: pay the gifts out of the held funds
//	ordered → done     on the birthday (12:00 local): release what is left
//	any open → cancelled  by the user: release the hold, refund undelivered gifts

type GiftPlan struct {
	Kind        string `json:"kind"`
	Merchant    string `json:"merchant"`
	ItemName    string `json:"itemName"`
	AmountCents int64  `json:"amountCents"`
}

type BirthdayPayload struct {
	LockCents     int64      `json:"lockCents"`
	Birthday      string     `json:"birthday"` // MM-DD
	OrderLeadDays int        `json:"orderLeadDays"`
	Gifts         []GiftPlan `json:"gifts"`
	TargetDate    string     `json:"targetDate,omitempty"` // YYYY-MM-DD the current cycle aims at
}

var defaultGifts = []GiftPlan{
	{Kind: "flower", Merchant: "花加", ItemName: "向日葵混搭花束 · 19 枝", AmountCents: 26800},
	{Kind: "cake", Merchant: "诺心", ItemName: "6 寸草莓奶油生日蛋糕", AmountCents: 19800},
}

func giftsTotal(g []GiftPlan) int64 {
	var t int64
	for _, x := range g {
		t += x.AmountCents
	}
	return t
}

type LogEntry struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

type GiftOrder struct {
	ID          uint64    `json:"id"`
	ScenarioID  uint64    `json:"scenarioId"`
	Kind        string    `json:"kind"`
	Merchant    string    `json:"merchant"`
	ItemName    string    `json:"itemName"`
	AmountCents int64     `json:"amountCents"`
	DeliverAt   time.Time `json:"deliverAt"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Scenario struct {
	ID        uint64          `json:"id"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	ContactID uint64          `json:"contactId"`
	Payload   BirthdayPayload `json:"payload"`
	Status    string          `json:"status"`
	Log       []LogEntry      `json:"log"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Contact   *Contact        `json:"contact"`
	Orders    []GiftOrder     `json:"orders"`
}

func (s *Service) registerScenario(r *trpc.Router) {
	r.Mutation("scenario.createBirthdayGuard", trpc.Typed(s.CreateBirthdayGuard))
	r.Query("scenario.list", trpc.NoInput(s.ListScenarios))
	r.Mutation("scenario.cancel", trpc.Typed(s.CancelScenario))
}

// nextBirthday returns the next birthday (12:00 local) not yet passed;
// Feb 29 falls back to Feb 28 in non-leap years.
func nextBirthday(mmdd string, now time.Time, loc *time.Location) (time.Time, error) {
	var m, d int
	if _, err := fmt.Sscanf(mmdd, "%02d-%02d", &m, &d); err != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, fmt.Errorf("invalid birthday %q", mmdd)
	}
	now = now.In(loc)
	for y := now.Year(); y <= now.Year()+1; y++ {
		b := birthdayIn(y, m, d, loc)
		if b.After(now) {
			return b, nil
		}
	}
	return birthdayIn(now.Year()+1, m, d, loc), nil
}

func birthdayIn(year, m, d int, loc *time.Location) time.Time {
	t := time.Date(year, time.Month(m), d, 12, 0, 0, 0, loc)
	if t.Month() != time.Month(m) { // e.g. Feb 29 in a non-leap year normalised into March
		t = time.Date(year, time.Month(m)+1, 0, 12, 0, 0, 0, loc)
	}
	return t
}

type BirthdayIn struct {
	ContactQuery    string `json:"contactQuery"`
	LockCents       int64  `json:"lockCents"`
	OrderLeadDays   int    `json:"orderLeadDays"`
	GiftBudgetCents int64  `json:"giftBudgetCents"`
}

func (in *BirthdayIn) Validate() error {
	in.ContactQuery = strings.TrimSpace(in.ContactQuery)
	if err := checkLen("联系人", in.ContactQuery, 1, 64); err != nil {
		return err
	}
	if in.LockCents == 0 {
		in.LockCents = 100_000
	}
	if in.OrderLeadDays == 0 {
		in.OrderLeadDays = 2
	}
	if err := checkCents("锁定金额", in.LockCents, 1_000_000); err != nil {
		return err
	}
	if in.LockCents < giftsTotal(defaultGifts) {
		return apperr.Bad("锁定金额不能低于礼品合计 ¥%s", money.YuanGrouped(giftsTotal(defaultGifts)))
	}
	if in.OrderLeadDays < 1 || in.OrderLeadDays > 7 {
		return apperr.Bad("提前下单天数需为 1-7 天")
	}
	if in.GiftBudgetCents < 0 || in.GiftBudgetCents > 200_000 {
		return apperr.Bad("礼品预算超出范围")
	}
	return nil
}

type BirthdayOut struct {
	ID      uint64          `json:"id"`
	Contact *Contact        `json:"contact"`
	Payload BirthdayPayload `json:"payload"`
	Events  []string        `json:"events"`
}

func (s *Service) CreateBirthdayGuard(ctx context.Context, in BirthdayIn) (*BirthdayOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	matched, err := s.SearchContacts(ctx, in.ContactQuery)
	if err != nil {
		return nil, err
	}
	if len(matched) == 0 {
		return nil, apperr.NotFoundf("没有找到与「%s」相关的联系人", in.ContactQuery)
	}
	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, c := range matched {
			names = append(names, c.Name)
		}
		return nil, apperr.Bad("匹配到多位联系人：%s，请说明具体是哪一位", strings.Join(names, "、"))
	}
	c := matched[0]
	if c.Birthday == nil || *c.Birthday == "" {
		return nil, apperr.Bad("联系人「%s」没有记录生日，请先在通讯录补充", c.Name)
	}
	target, err := nextBirthday(*c.Birthday, s.Now(), s.Cfg.Location)
	if err != nil {
		return nil, apperr.Bad("联系人「%s」的生日格式有误", c.Name)
	}
	p := BirthdayPayload{LockCents: in.LockCents, Birthday: *c.Birthday, OrderLeadDays: in.OrderLeadDays, Gifts: defaultGifts, TargetDate: target.Format("2006-01-02")}

	var id uint64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := ledger.LockAccount(ctx, tx, uid); err != nil { // serialise per user
			return err
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scenarios WHERE user_id = ? AND contact_id = ? AND status NOT IN ('done','cancelled')`, uid, c.ID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return apperr.Bad("「%s」已有进行中的守护任务", c.Name)
		}
		log := []LogEntry{{At: s.Now().UTC(), Text: fmt.Sprintf("守护已开启：监测到%s生日为 %s，当月将锁定 ¥%s 活期，生日前 %d 天自动下单鲜花和蛋糕",
			c.Name, *c.Birthday, money.YuanGrouped(in.LockCents), in.OrderLeadDays)}}
		res, err := tx.ExecContext(ctx, `INSERT INTO scenarios (user_id, type, title, contact_id, payload, status, log, created_at, updated_at)
			VALUES (?, 'birthday_guard', ?, ?, ?, 'armed', ?, ?, ?)`, uid, c.Name+"生日守护", c.ID, mustJSON(p), mustJSON(log), s.Now().UTC(), s.Now().UTC())
		if err != nil {
			return err
		}
		lid, _ := res.LastInsertId()
		id = uint64(lid)
		return nil
	})
	if err != nil {
		return nil, err
	}
	events, err := s.TickScenarios(ctx, uid)
	if err != nil {
		return nil, err
	}
	return &BirthdayOut{ID: id, Contact: &c, Payload: p, Events: events}, nil
}

func (s *Service) ListScenarios(ctx context.Context) ([]Scenario, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.TickScenarios(ctx, uid); err != nil {
		slog.ErrorContext(ctx, "scenario tick failed", "error", err)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, type, title, contact_id, payload, status, log, created_at, updated_at
		FROM scenarios WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT 50`, uid)
	if err != nil {
		return nil, err
	}
	out := []Scenario{}
	for rows.Next() {
		var sc Scenario
		var payload, log []byte
		if err := rows.Scan(&sc.ID, &sc.Type, &sc.Title, &sc.ContactID, &payload, &sc.Status, &log, &sc.CreatedAt, &sc.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		_ = jsonUnmarshal(payload, &sc.Payload)
		if jsonUnmarshal(log, &sc.Log) != nil || sc.Log == nil {
			sc.Log = []LogEntry{}
		}
		out = append(out, sc)
	}
	rows.Close()
	for i := range out {
		if c, err := s.getContact(ctx, s.DB, uid, out[i].ContactID); err == nil {
			out[i].Contact = c
		}
		if out[i].Orders, err = s.giftOrders(ctx, uid, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) giftOrders(ctx context.Context, uid, scenarioID uint64) ([]GiftOrder, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, scenario_id, kind, merchant, item_name, amount_cents, deliver_at, status, created_at
		FROM gift_orders WHERE user_id = ? AND scenario_id = ? ORDER BY id`, uid, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GiftOrder{}
	for rows.Next() {
		var g GiftOrder
		if err := rows.Scan(&g.ID, &g.ScenarioID, &g.Kind, &g.Merchant, &g.ItemName, &g.AmountCents, &g.DeliverAt, &g.Status, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// TickScenarios advances every open scenario of one user (uid=0: all users,
// used by the worker). Returns human-readable events.
func (s *Service) TickScenarios(ctx context.Context, uid uint64) ([]string, error) {
	q := `SELECT id, user_id FROM scenarios WHERE status IN ('armed','locked','ordered')`
	args := []any{}
	if uid != 0 {
		q += ` AND user_id = ?`
		args = append(args, uid)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY id LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	type ref struct{ id, uid uint64 }
	var list []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.id, &r.uid); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	rows.Close()

	events := []string{}
	for _, r := range list {
		var ev []string
		err := s.tx(ctx, func(tx *sql.Tx) error {
			var err error
			ev, err = s.tickOne(ctx, tx, r.uid, r.id)
			return err
		})
		if err != nil {
			slog.ErrorContext(ctx, "scenario tick failed", "scenarioId", r.id, "error", err)
			continue
		}
		for _, e := range ev {
			s.Audit.Write(ctx, auditEntry(r.uid, "scenario.tick", uintStr(r.id)+" "+clipStr(e, 100), nil))
		}
		events = append(events, ev...)
	}
	return events, nil
}

func (s *Service) tickOne(ctx context.Context, tx *sql.Tx, uid, id uint64) ([]string, error) {
	acc, err := ledger.LockAccount(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	var status string
	var payloadRaw, logRaw []byte
	var contactID uint64
	err = tx.QueryRowContext(ctx, `SELECT status, payload, log, contact_id FROM scenarios WHERE id = ? AND user_id = ? FOR UPDATE`, id, uid).
		Scan(&status, &payloadRaw, &logRaw, &contactID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p BirthdayPayload
	if err := jsonUnmarshal(payloadRaw, &p); err != nil {
		return nil, err
	}
	var log []LogEntry
	_ = jsonUnmarshal(logRaw, &log)

	cname := "对方"
	if c, err := s.getContact(ctx, tx, uid, contactID); err == nil {
		cname = c.Name
	}
	now := s.Now()
	loc := s.Cfg.Location
	var target time.Time
	payloadChanged := false
	if p.TargetDate != "" {
		if t, err := time.ParseInLocation("2006-01-02", p.TargetDate, loc); err == nil {
			target = t.Add(12 * time.Hour)
		}
	}
	if target.IsZero() || (status == "armed" && now.After(target.Add(24*time.Hour))) {
		// legacy row or an armed cycle that was missed entirely: aim at the next birthday
		if target, err = nextBirthday(p.Birthday, now, loc); err != nil {
			return nil, err
		}
		p.TargetDate = target.Format("2006-01-02")
		payloadChanged = true
	}
	monthStart := time.Date(target.Year(), target.Month(), 1, 0, 0, 0, 0, loc)
	orderFrom := target.AddDate(0, 0, -p.OrderLeadDays)

	var events []string
	add := func(text string) {
		events = append(events, text)
		log = append(log, LogEntry{At: now.UTC(), Text: text})
	}

	if status == "armed" && !now.Before(monthStart) {
		if err := ledger.Hold(ctx, tx, acc, p.LockCents, "锁定生日专款"); err != nil {
			if _, biz := apperr.As(err); !biz {
				return nil, err
			}
			// business failure (insufficient funds): stay armed, retry next tick, log once per day
			if len(log) == 0 || now.Sub(log[len(log)-1].At) > 24*time.Hour {
				add(fmt.Sprintf("已进入%s生日月，但可用余额不足，暂未能锁定 ¥%s，将自动重试", cname, money.YuanGrouped(p.LockCents)))
			}
		} else {
			status = "locked"
			add(fmt.Sprintf("已进入%s生日月，锁定活期 ¥%s，专款专用", cname, money.YuanGrouped(p.LockCents)))
		}
	}

	if status == "locked" && !now.Before(orderFrom) && now.Before(target) {
		names := make([]string, 0, len(p.Gifts))
		for _, g := range p.Gifts {
			txID, err := ledger.DebitLocked(ctx, tx, acc, ledger.Posting{
				AmountCents: g.AmountCents, Category: "购物", Counterparty: g.Merchant, Memo: "生日礼品 · " + g.ItemName,
				Channel: "生日守护", BizType: "scenario_gift", BizRef: uintStr(id),
			})
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO gift_orders (user_id, scenario_id, kind, merchant, item_name, amount_cents, deliver_at, status, tx_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, 'placed', ?)`, uid, id, g.Kind, g.Merchant, g.ItemName, g.AmountCents, target.UTC(), txID); err != nil {
				return nil, err
			}
			names = append(names, "「"+g.ItemName+"」")
		}
		status = "ordered"
		add(fmt.Sprintf("已下单 %s，合计 ¥%s（从专款中支付），将于 %s 送达", strings.Join(names, "和"), money.YuanGrouped(giftsTotal(p.Gifts)), p.Birthday))
	}

	if (status == "locked" || status == "ordered") && !now.Before(target) {
		remaining := p.LockCents
		if status == "ordered" {
			remaining -= giftsTotal(p.Gifts)
		}
		if err := ledger.Release(ctx, tx, acc, remaining); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE gift_orders SET status = 'delivered' WHERE user_id = ? AND scenario_id = ? AND status = 'placed'`, uid, id); err != nil {
			return nil, err
		}
		status = "done"
		add(fmt.Sprintf("今天是%s生日，礼品已送达，剩余专款 ¥%s 已解锁", cname, money.YuanGrouped(remaining)))
	}

	if len(events) == 0 && !payloadChanged {
		return nil, nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE scenarios SET status = ?, payload = ?, log = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		status, mustJSON(p), mustJSON(log), now.UTC(), id, uid)
	return events, err
}

func (s *Service) CancelScenario(ctx context.Context, in IDIn) (*OKOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		acc, err := ledger.LockAccount(ctx, tx, uid)
		if err != nil {
			return err
		}
		var status string
		var payloadRaw, logRaw []byte
		err = tx.QueryRowContext(ctx, `SELECT status, payload, log FROM scenarios WHERE id = ? AND user_id = ? FOR UPDATE`, in.ID, uid).Scan(&status, &payloadRaw, &logRaw)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundf("任务不存在")
		}
		if err != nil {
			return err
		}
		if status == "done" || status == "cancelled" {
			return nil
		}
		var p BirthdayPayload
		if err := jsonUnmarshal(payloadRaw, &p); err != nil {
			return err
		}
		var log []LogEntry
		_ = jsonUnmarshal(logRaw, &log)
		text := "守护任务已取消"
		switch status {
		case "locked":
			if err := ledger.Release(ctx, tx, acc, p.LockCents); err != nil {
				return err
			}
			text += fmt.Sprintf("，锁定的 ¥%s 已解锁", money.YuanGrouped(p.LockCents))
		case "ordered":
			// Undelivered gifts are refunded to the account; the rest of the hold is released.
			rows, err := tx.QueryContext(ctx, `SELECT id, merchant, item_name, amount_cents FROM gift_orders WHERE user_id = ? AND scenario_id = ? AND status = 'placed' FOR UPDATE`, uid, in.ID)
			if err != nil {
				return err
			}
			type gift struct {
				id             uint64
				merchant, item string
				amount         int64
			}
			var gifts []gift
			for rows.Next() {
				var g gift
				if err := rows.Scan(&g.id, &g.merchant, &g.item, &g.amount); err != nil {
					rows.Close()
					return err
				}
				gifts = append(gifts, g)
			}
			rows.Close()
			var refunded int64
			for _, g := range gifts {
				if _, err := ledger.Credit(ctx, tx, acc, ledger.Posting{AmountCents: g.amount, Category: "退款", Counterparty: g.merchant, Memo: "取消礼品订单 · " + g.item, Channel: "生日守护", BizType: "scenario_refund", BizRef: uintStr(g.id)}); err != nil {
					return err
				}
				refunded += g.amount
			}
			if _, err := tx.ExecContext(ctx, `UPDATE gift_orders SET status = 'cancelled' WHERE user_id = ? AND scenario_id = ? AND status = 'placed'`, uid, in.ID); err != nil {
				return err
			}
			rest := p.LockCents - giftsTotal(p.Gifts)
			if err := ledger.Release(ctx, tx, acc, rest); err != nil {
				return err
			}
			text += fmt.Sprintf("，退回礼品款 ¥%s，解锁剩余专款 ¥%s", money.YuanGrouped(refunded), money.YuanGrouped(rest))
		}
		log = append(log, LogEntry{At: s.Now().UTC(), Text: text})
		_, err = tx.ExecContext(ctx, `UPDATE scenarios SET status = 'cancelled', log = ?, updated_at = ? WHERE id = ? AND user_id = ?`, mustJSON(log), s.Now().UTC(), in.ID, uid)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &OKOut{OK: true}, nil
}
