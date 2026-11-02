package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/db"
	"lingxi/server/internal/reqctx"
	"lingxi/server/internal/trpc"
)

func (s *Service) registerTrade(r *trpc.Router) {
	r.Query("trade.overview", trpc.NoInput(s.TradeOverview))
	r.Query("trade.shipments", trpc.NoInput(s.ListShipments))
	r.Query("trade.track", trpc.Typed(func(ctx context.Context, in TrackShipmentIn) (*TradeShipment, error) {
		return s.TrackShipment(ctx, in.TrackingNo)
	}))
	r.Query("trade.customs", trpc.NoInput(s.ListCustoms))
	r.Query("trade.taxRefunds", trpc.NoInput(s.ListTaxRefunds))
}

// ================= types =================

type TradeMilestone struct {
	Time     string `json:"time"`
	Text     string `json:"text"`
	Location string `json:"location"`
}

type TradeShipment struct {
	ID           uint64           `json:"id"`
	TrackingNo   string           `json:"trackingNo"`
	Carrier      string           `json:"carrier"`
	Platform     string           `json:"platform"`
	Destination  string           `json:"destination"`
	GoodsDesc    *string          `json:"goodsDesc"`
	WeightKg     *string          `json:"weightKg"`
	FreightCents int64            `json:"freightCents"`
	Status       string           `json:"status"`
	Milestones   []TradeMilestone `json:"milestones"`
	EtaAt        *time.Time       `json:"etaAt"`
	CreatedAt    time.Time        `json:"createdAt"`
}

type TradeTaxRefund struct {
	ID          uint64    `json:"id"`
	Period      string    `json:"period"`
	AmountCents int64     `json:"amountCents"`
	Status      string    `json:"status"`
	Progress    int64     `json:"progress"`
	DeclNo      *string   `json:"declNo"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type TradeCustoms struct {
	ID            uint64          `json:"id"`
	DeclNo        string          `json:"declNo"`
	Port          string          `json:"port"`
	GoodsDesc     *string         `json:"goodsDesc"`
	ValueUsdCents int64           `json:"valueUsdCents"`
	Status        string          `json:"status"`
	DeclaredAt    time.Time       `json:"declaredAt"`
	ReleasedAt    *time.Time      `json:"releasedAt"`
	Refund        *TradeTaxRefund `json:"refund"`
}

type TradeOverviewOut struct {
	ActiveShipments       int64 `json:"activeShipments"`
	PendingCustoms        int64 `json:"pendingCustoms"`
	RefundInProgressCents int64 `json:"refundInProgressCents"`
}

type TrackShipmentIn struct {
	TrackingNo string `json:"trackingNo"`
}

func (in *TrackShipmentIn) Validate() error {
	in.TrackingNo = strings.TrimSpace(in.TrackingNo)
	if err := checkLen("运单号", in.TrackingNo, 4, 32); err != nil {
		return err
	}
	for _, c := range in.TrackingNo {
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-'
		if !ok {
			return apperr.Bad("运单号只能包含字母、数字和连字符")
		}
	}
	return nil
}

// ================= overview =================

func (s *Service) TradeOverview(ctx context.Context) (*TradeOverviewOut, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	out := &TradeOverviewOut{}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM shipments WHERE user_id = ? AND status <> 'delivered'`, uid).
		Scan(&out.ActiveShipments); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM customs_declarations WHERE user_id = ? AND status IN ('declared','inspecting')`, uid).
		Scan(&out.PendingCustoms); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents), 0) FROM tax_refunds WHERE user_id = ? AND status IN ('filing','reviewing','approved')`, uid).
		Scan(&out.RefundInProgressCents); err != nil {
		return nil, err
	}
	return out, nil
}

// ================= shipments =================

const shipmentColumns = `id, tracking_no, carrier, platform, destination, goods_desc, weight_kg, freight_cents, status, milestones, eta_at, created_at`

func parseTradeMilestones(raw []byte) []TradeMilestone {
	out := []TradeMilestone{}
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return []TradeMilestone{}
	}
	return out
}

func scanShipment(sc interface{ Scan(...any) error }) (TradeShipment, error) {
	var sh TradeShipment
	var goods, weight sql.NullString
	var ms []byte
	var eta sql.NullTime
	err := sc.Scan(&sh.ID, &sh.TrackingNo, &sh.Carrier, &sh.Platform, &sh.Destination, &goods, &weight, &sh.FreightCents, &sh.Status, &ms, &eta, &sh.CreatedAt)
	sh.GoodsDesc, sh.WeightKg, sh.EtaAt = ns(goods), ns(weight), nt(eta)
	sh.Milestones = parseTradeMilestones(ms)
	return sh, err
}

func (s *Service) ListShipments(ctx context.Context) ([]TradeShipment, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+shipmentColumns+` FROM shipments WHERE user_id = ? ORDER BY id DESC LIMIT 200`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TradeShipment{}
	for rows.Next() {
		sh, err := scanShipment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// TrackShipment finds a shipment by exact (case-insensitive) tracking number, falling back to fuzzy match.
func (s *Service) TrackShipment(ctx context.Context, no string) (*TradeShipment, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	in := TrackShipmentIn{TrackingNo: no}
	if err := in.Validate(); err != nil {
		return nil, err
	}
	kw := in.TrackingNo

	sh, err := scanShipment(s.DB.QueryRowContext(ctx,
		`SELECT `+shipmentColumns+` FROM shipments WHERE user_id = ? AND UPPER(tracking_no) = UPPER(?) ORDER BY id DESC LIMIT 1`, uid, kw))
	if err == nil {
		return &sh, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	like := "%" + db.EscapeLike(kw) + "%"
	sh, err = scanShipment(s.DB.QueryRowContext(ctx,
		`SELECT `+shipmentColumns+` FROM shipments WHERE user_id = ? AND UPPER(tracking_no) LIKE UPPER(?) ORDER BY id DESC LIMIT 1`, uid, like))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundf("未找到运单 %s，请核对单号", kw)
	}
	if err != nil {
		return nil, err
	}
	return &sh, nil
}

// ================= tax refunds =================

const taxRefundColumns = `id, period, amount_cents, status, progress, decl_no, updated_at`

func scanTaxRefund(sc interface{ Scan(...any) error }) (TradeTaxRefund, error) {
	var t TradeTaxRefund
	var decl sql.NullString
	err := sc.Scan(&t.ID, &t.Period, &t.AmountCents, &t.Status, &t.Progress, &decl, &t.UpdatedAt)
	t.DeclNo = ns(decl)
	return t, err
}

func (s *Service) queryTaxRefunds(ctx context.Context, uid uint64) ([]TradeTaxRefund, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+taxRefundColumns+` FROM tax_refunds WHERE user_id = ? ORDER BY id DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TradeTaxRefund{}
	for rows.Next() {
		t, err := scanTaxRefund(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) ListTaxRefunds(ctx context.Context) ([]TradeTaxRefund, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	return s.queryTaxRefunds(ctx, uid)
}

// ================= customs =================

func (s *Service) ListCustoms(ctx context.Context) ([]TradeCustoms, error) {
	uid, err := reqctx.UserID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, decl_no, port, goods_desc, value_usd_cents, status, declared_at, released_at
		FROM customs_declarations WHERE user_id = ? ORDER BY id DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TradeCustoms{}
	for rows.Next() {
		var c TradeCustoms
		var goods sql.NullString
		var rel sql.NullTime
		if err := rows.Scan(&c.ID, &c.DeclNo, &c.Port, &goods, &c.ValueUsdCents, &c.Status, &c.DeclaredAt, &rel); err != nil {
			return nil, err
		}
		c.GoodsDesc, c.ReleasedAt = ns(goods), nt(rel)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	refunds, err := s.queryTaxRefunds(ctx, uid)
	if err != nil {
		return nil, err
	}
	// refunds are newest-first; keep the newest per declaration number.
	byDecl := make(map[string]*TradeTaxRefund, len(refunds))
	for i := range refunds {
		if refunds[i].DeclNo == nil {
			continue
		}
		if _, ok := byDecl[*refunds[i].DeclNo]; !ok {
			byDecl[*refunds[i].DeclNo] = &refunds[i]
		}
	}
	for i := range out {
		out[i].Refund = byDecl[out[i].DeclNo]
	}
	return out, nil
}
