// Command seed loads the demo data set (cross-border e-commerce seller
// 「陈航 · 跨洋科技」) into an empty or -reset database. It is a port of the old
// db/seed.ts, adjusted so the ledger is internally consistent.
//
// Usage:
//
//	SEED_PASSWORD='...' go run ./cmd/seed [-reset]
//
// Refuses to run when APP_ENV=production.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
	"time"

	"lingxi/server/internal/auth"
	"lingxi/server/internal/config"
	"lingxi/server/internal/db"
)

const (
	demoUsername    = "chenhang"
	demoDisplayName = "陈航"
	demoHolderName  = "陈航 · 跨洋科技"

	accountBalanceCents int64 = 48652040
)

// userTables are wiped (by user_id) on -reset, children before parents.
var userTables = []string{
	"transactions", "contacts", "scheduled_transfers", "aa_splits", "cards",
	"holdings", "risk_profiles", "scenarios", "gift_orders", "agent_messages",
	"agent_confirmations", "fx_wallets", "fx_wallet_journal", "fx_contracts",
	"scf_loans", "shipments", "customs_declarations", "tax_refunds",
	"idempotency_keys", "sessions", "accounts", "audit_logs",
}

func main() {
	reset := flag.Bool("reset", false, "delete the existing demo user and all of its data, then re-seed")
	flag.Parse()
	if err := run(*reset); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(reset bool) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return errors.New("APP_ENV=production: refusing to seed demo data")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() {
		return errors.New("APP_ENV=production: refusing to seed demo data")
	}

	password := os.Getenv("SEED_PASSWORD")
	generated := false
	if password == "" {
		if password, err = randomPassword(16); err != nil {
			return err
		}
		generated = true
	}
	if err := auth.ValidatePasswordPolicy(password); err != nil {
		return fmt.Errorf("SEED_PASSWORD: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	d, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := db.Migrate(ctx, d); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	var existing uint64
	err = d.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, demoUsername).Scan(&existing)
	switch {
	case err == nil:
		if !reset {
			return fmt.Errorf("user %q already exists; re-run with -reset to delete it and re-seed", demoUsername)
		}
		if err := db.WithTx(ctx, d, func(tx *sql.Tx) error { return deleteUser(ctx, tx, existing) }); err != nil {
			return fmt.Errorf("reset: %w", err)
		}
		fmt.Printf("reset: removed user %q (id %d) and its data\n", demoUsername, existing)
	case errors.Is(err, sql.ErrNoRows):
	default:
		return err
	}

	uid, err := auth.CreateUser(ctx, d, auth.RegisterInput{Username: demoUsername, Password: password, DisplayName: demoDisplayName})
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	counts := map[string]int{}
	err = db.WithTx(ctx, d, func(tx *sql.Tx) error {
		counts = map[string]int{} // tx may be retried
		return seed(ctx, tx, uid, time.Now(), cfg.Location, counts)
	})
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	fmt.Printf("seeded user %q (id %d)\n", demoUsername, uid)
	names := make([]string, 0, len(counts))
	for k := range counts {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Printf("  %-22s %d\n", k, counts[k])
	}
	if generated {
		fmt.Printf("\ngenerated password (shown once, not stored): %s\n", password)
	}
	return nil
}

func deleteUser(ctx context.Context, tx *sql.Tx, uid uint64) error {
	for _, t := range userTables {
		// table names come from the constant list above, never from input
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t+" WHERE user_id = ?", uid); err != nil {
			return fmt.Errorf("delete %s: %w", t, err)
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, uid)
	return err
}

func randomPassword(n int) (string, error) {
	const (
		lower = "abcdefghijkmnopqrstuvwxyz"
		upper = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digit = "23456789"
	)
	all := lower + upper + digit
	pick := func(set string) (byte, error) {
		i, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		if err != nil {
			return 0, err
		}
		return set[i.Int64()], nil
	}
	buf := make([]byte, 0, n)
	for _, set := range []string{lower, upper, digit} {
		c, err := pick(set)
		if err != nil {
			return "", err
		}
		buf = append(buf, c)
	}
	for len(buf) < n {
		c, err := pick(all)
		if err != nil {
			return "", err
		}
		buf = append(buf, c)
	}
	for i := len(buf) - 1; i > 0; i-- { // Fisher-Yates
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		buf[i], buf[j.Int64()] = buf[j.Int64()], buf[i]
	}
	return string(buf), nil
}

// ---------------------------------------------------------------- helpers

type seeder struct {
	ctx    context.Context
	tx     *sql.Tx
	uid    uint64
	now    time.Time
	loc    *time.Location
	counts map[string]int
}

// day returns now+offset days at local h:m, in UTC.
func (s *seeder) day(offset, h, m int) time.Time {
	n := s.now.In(s.loc)
	return time.Date(n.Year(), n.Month(), n.Day()+offset, h, m, 0, 0, s.loc).UTC()
}

func (s *seeder) localFmt(t time.Time) string { return t.In(s.loc).Format("01-02 15:04") }

func (s *seeder) exec(table, q string, args ...any) (int64, error) {
	res, err := s.tx.ExecContext(s.ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("insert %s: %w", table, err)
	}
	s.counts[table]++
	id, _ := res.LastInsertId()
	return id, nil
}

func ns(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func js(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // static, JSON-safe data
	}
	return string(b)
}

// ---------------------------------------------------------------- data

type txRow struct {
	dir      string
	amount   int64
	category string
	party    string
	memo     string
	channel  string
	at       time.Time
	anomaly  string
}

type contract struct {
	typ, ccy, dir, status string
	foreign, rateBps      int64
	margin                int64
	expires, created      time.Time
}

type milestone struct {
	Time     string `json:"time"`
	Text     string `json:"text"`
	Location string `json:"location"`
}

type giftPlan struct {
	Kind        string `json:"kind"`
	Merchant    string `json:"merchant"`
	ItemName    string `json:"itemName"`
	AmountCents int64  `json:"amountCents"`
}

type birthdayPayload struct {
	LockCents     int64      `json:"lockCents"`
	Birthday      string     `json:"birthday"`
	OrderLeadDays int        `json:"orderLeadDays"`
	Gifts         []giftPlan `json:"gifts"`
	TargetDate    string     `json:"targetDate,omitempty"`
}

type logEntry struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

type aaParticipant struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	AmountCents int64  `json:"amountCents"`
	Paid        bool   `json:"paid"`
}

func seed(ctx context.Context, tx *sql.Tx, uid uint64, now time.Time, loc *time.Location, counts map[string]int) error {
	s := &seeder{ctx: ctx, tx: tx, uid: uid, now: now, loc: loc, counts: counts}
	d := s.day

	// ---------- global tables (only when empty) ----------
	if err := s.seedGlobals(); err != nil {
		return err
	}

	// ---------- account ----------
	var accID uint64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE user_id = ? FOR UPDATE`, uid).Scan(&accID); err != nil {
		return fmt.Errorf("load account: %w", err)
	}

	// ---------- fx contracts ----------
	contracts := []contract{
		{"forward", "USD", "sell_foreign", "active", 5000000, 70850, 1771250, d(30, 12, 0), d(-12, 12, 0)},
		{"forward", "USD", "sell_foreign", "settled", 3000000, 70960, 0, d(-40, 12, 0), d(-70, 12, 0)},
	}
	var locked int64
	lockedForeign := map[string]int64{}
	for _, c := range contracts {
		if _, err := s.exec("fx_contracts", `INSERT INTO fx_contracts
			(user_id, type, currency, foreign_amount_cents, locked_rate_bps, direction, margin_cents, expires_at, status, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			uid, c.typ, c.ccy, c.foreign, c.rateBps, c.dir, c.margin, c.expires, c.status, c.created); err != nil {
			return err
		}
		if c.status == "active" {
			locked += c.margin
			if c.dir == "sell_foreign" {
				lockedForeign[c.ccy] += c.foreign
			}
		}
	}

	// ---------- fx wallets (+ opening journal entry) ----------
	wallets := []struct {
		ccy     string
		balance int64
	}{{"USD", 6230050}, {"EUR", 1852000}}
	for _, w := range wallets {
		lf := lockedForeign[w.ccy]
		if lf > w.balance {
			return fmt.Errorf("wallet %s: locked %d exceeds balance %d", w.ccy, lf, w.balance)
		}
		wid, err := s.exec("fx_wallets", `INSERT INTO fx_wallets (user_id, currency, balance_foreign_cents, locked_foreign_cents) VALUES (?,?,?,?)`,
			uid, w.ccy, w.balance, lf)
		if err != nil {
			return err
		}
		if _, err := s.exec("fx_wallet_journal", `INSERT INTO fx_wallet_journal
			(user_id, wallet_id, currency, direction, amount_cents, balance_after_cents, biz_type, memo, occurred_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			uid, wid, w.ccy, "in", w.balance, w.balance, "seed", "期初余额", d(-90, 9, 0)); err != nil {
			return err
		}
	}

	// ---------- scf loans ----------
	loans := []struct {
		code, purpose, ref, status string
		amount, rate               int64
		applied, due               time.Time
		repaid                     *time.Time
	}{
		{"ORDER_LOAN", "黑五备货采购", "AMZ-ORDER-88213", "disbursed", 12000000, 680, d(-25, 12, 0), d(65, 12, 0), nil},
		{"TAX_REBATE", "8 月退税过桥", "2025-08", "repaid", 8000000, 560, d(-95, 12, 0), d(-35, 12, 0), ptr(d(-38, 12, 0))},
	}
	for _, l := range loans {
		var pid uint64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM scf_products WHERE code = ?`, l.code).Scan(&pid); err != nil {
			return fmt.Errorf("scf product %s: %w", l.code, err)
		}
		var interest int64
		var repaidAt any
		if l.repaid != nil {
			days := int64(l.repaid.Sub(l.applied).Hours()/24) + 1 // ceil, as the service does
			interest = l.amount * l.rate * days / (10000 * 365)
			repaidAt = *l.repaid
		}
		if _, err := s.exec("scf_loans", `INSERT INTO scf_loans
			(user_id, product_id, amount_cents, rate_bps, purpose, related_ref, status, interest_cents, applied_at, due_at, repaid_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			uid, pid, l.amount, l.rate, l.purpose, l.ref, l.status, interest, l.applied, l.due, repaidAt); err != nil {
			return err
		}
	}

	// ---------- shipments ----------
	ms := func(off, h, m int, text, where string) milestone {
		return milestone{Time: s.localFmt(d(off, h, m)), Text: text, Location: where}
	}
	shipments := []struct {
		no, carrier, platform, dest, goods, weight, status string
		freight                                            int64
		miles                                              []milestone
		eta, created                                       time.Time
	}{
		{"YT88362012", "云途物流", "Amazon 北美站", "美国·洛杉矶 ONT8 仓", "蓝牙耳机 × 1,200 件", "486kg", "in_transit", 2136000,
			[]milestone{
				ms(-9, 14, 20, "深圳揽收，货物已入云途仓", "深圳"),
				ms(-7, 9, 5, "出口报关放行（蛇口港）", "深圳蛇口"),
				ms(-5, 22, 40, "装船离港，船名 COSCO SHIPPING UNIVERSE", "太平洋航线"),
				ms(-1, 6, 12, "抵达洛杉矶港，等待卸船", "洛杉矶"),
			}, d(9, 12, 0), d(-9, 12, 0)},
		{"4PX77120908", "递四方", "速卖通", "德国·法兰克福", "手机壳套装 × 860 件", "152kg", "customs_clearance", 684000,
			[]milestone{
				ms(-6, 11, 30, "东莞仓揽收", "东莞"),
				ms(-4, 16, 45, "香港机场起飞，航班 CX880", "香港"),
				ms(-2, 8, 20, "抵达法兰克福，转海关查验", "法兰克福"),
				ms(0, 7, 55, "海关查验中（抽中人工查验）", "法兰克福海关"),
			}, d(6, 12, 0), d(-6, 12, 0)},
		{"SFIN33012871", "顺丰国际", "Temu", "日本·东京", "智能手环 × 300 件", "45kg", "delivered", 189000,
			[]milestone{
				ms(-8, 10, 10, "深圳揽收", "深圳"),
				ms(-7, 19, 30, "宝安机场起飞", "深圳"),
				ms(-6, 8, 40, "东京羽田清关放行", "东京"),
				ms(-5, 15, 22, "签收完成（Temu 集货仓）", "东京"),
			}, d(-4, 12, 0), d(-8, 12, 0)},
		{"YW55690234", "燕文物流", "独立站", "英国·伦敦", "宠物用品 × 540 件", "310kg", "label_created", 1176000,
			[]milestone{ms(0, 9, 0, "运单已创建，等待揽收", "深圳")}, d(14, 12, 0), d(0, 9, 0)},
	}
	for _, sh := range shipments {
		if _, err := s.exec("shipments", `INSERT INTO shipments
			(user_id, tracking_no, carrier, platform, destination, goods_desc, weight_kg, freight_cents, status, milestones, eta_at, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			uid, sh.no, sh.carrier, sh.platform, sh.dest, sh.goods, sh.weight, sh.freight, sh.status, js(sh.miles), sh.eta, sh.created); err != nil {
			return err
		}
	}

	// ---------- customs declarations ----------
	customs := []struct {
		no, port, goods, status string
		usd                     int64
		declared                time.Time
		released                any
	}{
		{"5304202611882033", "深圳蛇口海关", "蓝牙耳机（出口退税 13%）", "released", 8640000, d(-8, 12, 0), d(-7, 12, 0)},
		{"5304202611885617", "宁波舟山港海关", "手机壳套装", "inspecting", 2380000, d(-2, 12, 0), nil},
		{"5304202611889901", "深圳前海海关", "宠物用品", "declared", 4120000, d(0, 9, 0), nil},
	}
	for _, c := range customs {
		if _, err := s.exec("customs_declarations", `INSERT INTO customs_declarations
			(user_id, decl_no, port, goods_desc, value_usd_cents, status, declared_at, released_at) VALUES (?,?,?,?,?,?,?,?)`,
			uid, c.no, c.port, c.goods, c.usd, c.status, c.declared, c.released); err != nil {
			return err
		}
	}

	// ---------- tax refunds ----------
	refunds := []struct {
		period, status, declNo string
		amount                 int64
		progress               int
		updated                time.Time
	}{
		{"2025-08", "paid", "5304202611774521", 4238000, 100, d(-38, 12, 0)},
		{"2025-09", "approved", "5304202611778903", 3815000, 85, d(-6, 12, 0)},
		{"2025-10", "reviewing", "5304202611882033", 4572000, 55, d(-3, 12, 0)},
	}
	for _, r := range refunds {
		if _, err := s.exec("tax_refunds", `INSERT INTO tax_refunds (user_id, period, amount_cents, status, progress, decl_no, updated_at) VALUES (?,?,?,?,?,?,?)`,
			uid, r.period, r.amount, r.status, r.progress, r.declNo, r.updated); err != nil {
			return err
		}
	}

	// ---------- contacts ----------
	contacts := []struct{ name, phone, bank, tail, relation, birthday, note string }{
		{"林悦", "138****6621", "招商银行", "8842", "爱人", "11-18", "喜欢向日葵和草莓蛋糕"},
		{"王强", "137****3309", "灵犀银行", "2201", "合伙人", "03-24", ""},
		{"李厂长", "136****8874", "中国银行", "5566", "供应商", "", "深圳华强北电子厂"},
		{"张经理", "135****2210", "建设银行", "9012", "物流商", "", "云途物流客户经理"},
		{"James Miller", "+1 310****882", "Bank of America", "4408", "海外仓", "", "洛杉矶 ONT8 仓运营"},
	}
	cid := map[string]int64{}
	for _, c := range contacts {
		id, err := s.exec("contacts", `INSERT INTO contacts (user_id, name, phone, bank_name, card_tail, relation, birthday, note) VALUES (?,?,?,?,?,?,?,?)`,
			uid, c.name, ns(c.phone), ns(c.bank), ns(c.tail), ns(c.relation), ns(c.birthday), ns(c.note))
		if err != nil {
			return err
		}
		cid[c.name] = id
	}

	// ---------- transactions ----------
	in, out := "in", "out"
	txs := []txRow{
		// 电商回款
		{in, 12866000, "电商回款", "Amazon.com Services", "北美站 10 月下半月回款", "跨境收款", d(-13, 10, 0), ""},
		{in, 9642000, "电商回款", "Amazon.com Services", "北美站 10 月上半月回款", "跨境收款", d(-28, 10, 0), ""},
		{in, 11328000, "电商回款", "Amazon.com Services", "北美站 9 月下半月回款", "跨境收款", d(-43, 10, 0), ""},
		{in, 8895000, "电商回款", "Amazon.com Services", "北美站 9 月上半月回款", "跨境收款", d(-58, 10, 0), ""},
		{in, 2310000, "电商回款", "速卖通 AliExpress", "欧洲站回款", "跨境收款", d(-17, 11, 0), ""},
		{in, 1874000, "电商回款", "Payoneer 派安盈", "美元余额提现结汇", "跨境收款", d(-6, 14, 0), ""},
		{in, 4238000, "退税", "国家税务总局深圳市税务局", "2025-08 出口退税到账", "国库", d(-38, 9, 0), ""},
		// 经营支出
		{out, 2136000, "物流", "云途物流", "YT88362012 运费", "", d(-9, 15, 0), ""},
		{out, 684000, "物流", "递四方速递", "4PX77120908 运费", "", d(-6, 16, 0), ""},
		{out, 189000, "物流", "顺丰国际", "SFIN33012871 运费", "", d(-8, 11, 0), ""},
		{out, 960000, "物流", "云途物流", "10 月上旬批次运费", "", d(-24, 15, 0), ""},
		{out, 35600000, "采购", "深圳华强北电子厂（李厂长）", "蓝牙耳机黑五备货", "", d(-26, 14, 0), ""},
		{out, 18200000, "采购", "深圳华强北电子厂（李厂长）", "手机壳原料款", "", d(-52, 14, 0), ""},
		{out, 9800000, "采购", "东莞宠物用品厂", "宠物用品 540 件", "", d(-3, 10, 0), ""},
		{out, 4260000, "推广", "Amazon Advertising", "10 月 PPC 广告费", "", d(-12, 9, 0), ""},
		{out, 3150000, "推广", "Amazon Advertising", "9 月 PPC 广告费", "", d(-42, 9, 0), ""},
		{out, 1880000, "关税", "洛杉矶海关（代缴）", "蓝牙耳机关税", "", d(-1, 8, 0), ""},
		{out, 2400000, "仓储", "James Miller（ONT8 海外仓）", "10 月仓租+操作费", "", d(-10, 13, 0), ""},
		{out, 2400000, "仓储", "James Miller（ONT8 海外仓）", "9 月仓租+操作费", "", d(-40, 13, 0), ""},
		// 定时/固定
		{out, 1200000, "转账", "王强", "10 月工资", "", d(-17, 10, 0), ""},
		{out, 1200000, "转账", "王强", "9 月工资", "", d(-48, 10, 0), ""},
		{out, 1200000, "转账", "王强", "8 月工资", "", d(-79, 10, 0), ""},
		// 日常消费
		{out, 26800, "餐饮", "海底捞（海上世界店）", "团队聚餐", "", d(-5, 19, 0), ""},
		{out, 8900, "餐饮", "星巴克（科技园店）", "", "", d(-2, 9, 0), ""},
		{out, 15600, "餐饮", "八合里牛肉火锅", "", "", d(-20, 19, 0), ""},
		{out, 129900, "购物", "京东", "iPhone 配件", "", d(-7, 21, 0), ""},
		{out, 34900, "购物", "山姆会员店", "", "", d(-15, 16, 0), ""},
		{out, 4500, "交通", "滴滴出行", "", "", d(-4, 8, 0), ""},
		{out, 18600, "交通", "中石化加油站", "", "", d(-18, 18, 0), ""},
		{out, 56000, "住房", "自如公寓", "11 月房租", "", d(-1, 10, 0), ""},
		{in, 520000, "转账", "林悦", "家用补贴", "", d(-9, 20, 0), ""},
		// 异常交易
		{out, 520000, "其他", "深夜便利店扫码", "凌晨大额支出", "扫码支付", d(-3, 3, 42), "凌晨 0-5 点大额交易"},
		{out, 2800000, "转账", "某某商贸有限公司", "货款？", "", d(-2, 22, 0), "陌生对手方大额转账"},
		{out, 128000, "娱乐", "星光 KTV", "", "", d(-4, 21, 0), "当日娱乐类高频大额累计"},
		{out, 89900, "娱乐", "环球影城旗舰店", "", "", d(-4, 22, 0), "当日娱乐类高频大额累计"},
		{out, 128600, "娱乐", "夜宴酒吧", "", "", d(-4, 23, 0), "当日娱乐类高频大额累计"},
	}
	sort.SliceStable(txs, func(i, j int) bool { return txs[i].at.Before(txs[j].at) })

	var sumIn, sumOut int64
	for _, t := range txs {
		if t.dir == in {
			sumIn += t.amount
		} else {
			sumOut += t.amount
		}
	}
	opening := accountBalanceCents - sumIn + sumOut
	if opening < 0 {
		return fmt.Errorf("seed ledger inconsistent: opening balance %d is negative", opening)
	}
	if opening > 0 {
		txs = append([]txRow{{in, opening, "其他", "期初余额", "账户期初余额", "系统", txs[0].at.Add(-time.Minute), ""}}, txs...)
	}
	bal := int64(0)
	for _, t := range txs {
		if t.dir == in {
			bal += t.amount
		} else {
			bal -= t.amount
		}
		if bal < 0 {
			return fmt.Errorf("seed ledger inconsistent: balance goes negative (%d) at %s", bal, t.at)
		}
		channel := t.channel
		if channel == "" {
			channel = "手机银行"
		}
		if _, err := s.exec("transactions", `INSERT INTO transactions
			(user_id, account_id, direction, amount_cents, balance_after_cents, category, counterparty, memo, channel, status, is_anomaly, anomaly_reason, biz_type, occurred_at)
			VALUES (?,?,?,?,?,?,?,?,?,'success',?,?,'seed',?)`,
			uid, accID, t.dir, t.amount, bal, t.category, t.party, ns(t.memo), channel, t.anomaly != "", ns(t.anomaly), t.at); err != nil {
			return err
		}
	}
	if bal != accountBalanceCents {
		return fmt.Errorf("seed ledger inconsistent: final balance %d != %d", bal, accountBalanceCents)
	}

	// ---------- scheduled transfer ----------
	if _, err := s.exec("scheduled_transfers", `INSERT INTO scheduled_transfers (user_id, contact_id, amount_cents, memo, execute_at, status) VALUES (?,?,?,?,?,'pending')`,
		uid, cid["王强"], 1200000, "11 月工资", d(13, 10, 0)); err != nil {
		return err
	}

	// ---------- AA split ----------
	aa := []aaParticipant{
		{"王强", "", 6700, true}, {"陈晨", "", 6700, true}, {"刘洋", "", 6700, false}, {"赵敏", "", 6700, false},
	}
	if _, err := s.exec("aa_splits", `INSERT INTO aa_splits (user_id, title, total_cents, participants, status, created_at) VALUES (?,?,?,?,'collecting',?)`,
		uid, "部门聚餐 · 海底捞", 26800, js(aa), d(-5, 21, 0)); err != nil {
		return err
	}

	// ---------- cards ----------
	if _, err := s.exec("cards", `INSERT INTO cards (user_id, card_type, bank_name, last4, credit_limit_cents, used_cents, daily_limit_cents, status, online_pay_enabled)
		VALUES (?,?,?,?,?,?,?,?,?)`, uid, "debit", "灵犀银行", "6688", nil, 0, 5000000, "normal", true); err != nil {
		return err
	}
	if _, err := s.exec("cards", `INSERT INTO cards (user_id, card_type, bank_name, last4, credit_limit_cents, used_cents, daily_limit_cents, status, online_pay_enabled)
		VALUES (?,?,?,?,?,?,?,?,?)`, uid, "credit", "灵犀银行", "8899", 10000000, 1258000, 3000000, "normal", true); err != nil {
		return err
	}

	// ---------- holdings ----------
	for _, h := range []struct {
		product        string
		amount, profit int64
		at             time.Time
	}{
		{"灵犀日日盈", 20000000, 182300, d(-60, 12, 0)},
		{"稳利 180 天固收", 30000000, 402500, d(-80, 12, 0)},
	} {
		var pid uint64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM wealth_products WHERE name = ? ORDER BY id LIMIT 1`, h.product).Scan(&pid); err != nil {
			return fmt.Errorf("wealth product %q: %w", h.product, err)
		}
		if _, err := s.exec("holdings", `INSERT INTO holdings (user_id, product_id, amount_cents, profit_cents, status, purchased_at) VALUES (?,?,?,?,'holding',?)`,
			uid, pid, h.amount, h.profit, h.at); err != nil {
			return err
		}
	}

	// ---------- risk profile ----------
	answers := map[string]string{
		"您的计划投资期限是？":    "1-3 年",
		"您能接受的最大本金回撤是？": "5% 以内",
		"您的收入稳定性如何？":    "基本稳定",
		"您的投资经验？":       "买过债基/理财",
	}
	if _, err := s.exec("risk_profiles", `INSERT INTO risk_profiles (user_id, score, level, answers, created_at) VALUES (?,?,?,?,?)`,
		uid, 46, "稳健型", js(answers), d(-70, 12, 0)); err != nil {
		return err
	}

	// ---------- birthday guard (armed: nothing locked yet) ----------
	local := s.now.In(s.loc)
	target := time.Date(local.Year(), time.November, 18, 0, 0, 0, 0, s.loc)
	if target.Before(time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.loc)) {
		target = target.AddDate(1, 0, 0)
	}
	payload := birthdayPayload{
		LockCents: 100000, Birthday: "11-18", OrderLeadDays: 2, TargetDate: target.Format("2006-01-02"),
		Gifts: []giftPlan{
			{"flower", "花加", "向日葵花束", 26800},
			{"cake", "诺心", "6 寸草莓蛋糕", 19800},
		},
	}
	created := d(-2, 20, 0)
	scenarioStatus := "armed"
	logs := []logEntry{{At: created, Text: "守护任务已创建，进入生日当月将自动锁定 ¥1,000 专款"}}
	if _, err := s.exec("scenarios", `INSERT INTO scenarios (user_id, type, title, contact_id, payload, status, log, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		uid, "birthday_guard", "林悦生日守护", cid["林悦"], js(payload), scenarioStatus, js(logs), created, created); err != nil {
		return err
	}
	if scenarioStatus == "locked" { // a locked scenario also holds its lockCents
		locked += payload.LockCents
	}

	// ---------- finalise account ----------
	if locked > accountBalanceCents {
		return fmt.Errorf("locked %d exceeds balance %d", locked, accountBalanceCents)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET holder_name = ?, balance_cents = ?, locked_cents = ? WHERE id = ? AND user_id = ?`,
		demoHolderName, accountBalanceCents, locked, accID, uid); err != nil {
		return err
	}
	fmt.Printf("ledger: opening=%d in=%d out=%d balance=%d locked=%d\n", opening, sumIn, sumOut, accountBalanceCents, locked)
	return nil
}

func ptr[T any](v T) *T { return &v }

func (s *seeder) seedGlobals() error {
	empty := func(table string) (bool, error) {
		var n int
		if err := s.tx.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			return false, err
		}
		return n == 0, nil
	}

	if ok, err := empty("fx_rates"); err != nil {
		return err
	} else if ok {
		for _, r := range []struct {
			pair, ccy string
			buy, sell int
		}{
			{"USDCNY", "USD", 70850, 71130},
			{"EURCNY", "EUR", 77200, 77560},
			{"GBPCNY", "GBP", 90800, 91250},
			{"HKDCNY", "HKD", 9108, 9142},
			{"JPYCNY", "JPY", 468, 472},
		} {
			if _, err := s.exec("fx_rates", `INSERT INTO fx_rates (pair, currency, buy_rate_bps, sell_rate_bps, updated_at) VALUES (?,?,?,?,?)`,
				r.pair, r.ccy, r.buy, r.sell, s.now.UTC()); err != nil {
				return err
			}
		}
	}

	if ok, err := empty("scf_products"); err != nil {
		return err
	} else if ok {
		for _, p := range []struct {
			code, name, tags, desc string
			rate                   int
			max                    int64
			term                   int
		}{
			{"ORDER_LOAN", "跨境订单贷", "凭平台订单,随借随还,线上秒批", "凭亚马逊/速卖通已发货未回款订单融资，按订单金额 80% 授信，回款自动归还。", 680, 50000000, 90},
			{"WAREHOUSE", "海外仓仓单质押贷", "库存质押,海外仓,大额", "以海外仓在库货物仓单质押，按货值 60% 放款，适合备货旺季资金周转。", 750, 100000000, 180},
			{"TAX_REBATE", "出口退税贷", "退税过桥,低息,快到账", "以已申报未到账的出口退税款为质押，提前拿到退税资金，年化利率更低。", 560, 30000000, 60},
			{"CREDIT_INS", "出口信保融资", "中信保保单,赊销订单", "凭中信保短期出口信用保险保单，为赊销（OA）订单提供应收账款融资。", 620, 80000000, 120},
		} {
			if _, err := s.exec("scf_products", `INSERT INTO scf_products (code, name, rate_bps, max_amount_cents, term_days, tags, description) VALUES (?,?,?,?,?,?,?)`,
				p.code, p.name, p.rate, p.max, p.term, p.tags, p.desc); err != nil {
				return err
			}
		}
	}

	if ok, err := empty("wealth_products"); err != nil {
		return err
	} else if ok {
		for _, p := range []struct {
			name, typ, risk, tags, desc string
			rate                        int
			min                         int64
			term                        int
		}{
			{"灵犀日日盈", "cash", "R1", "灵活申赎,货基增强", "跨境收款闲置资金好去处，T+0 赎回实时到账。", 185, 100, 0},
			{"稳利 180 天固收", "bond", "R2", "纯债,持有到期", "主投利率债与高等级信用债，适合账期闲置资金。", 268, 100000, 180},
			{"汇金双债增强", "bond", "R2", "固收+,转债增强", "纯债打底 + 可转债增强，追求稳健超额。", 295, 100000, 365},
			{"均衡配置混合 A", "mixed", "R3", "股债平衡", "股债 4:6 动态平衡，波动适中。", 420, 100000, 0},
			{"纳斯达克 100 指数 QDII", "index", "R4", "美股,科技,美元资产", "跟踪纳斯达克 100，美元资产天然对冲汇率敞口。", 0, 10000, 0},
			{"积存金", "gold", "R3", "黄金,抗通胀", "1 元起投实物黄金积存，可提金可赎回。", 0, 100, 0},
		} {
			if _, err := s.exec("wealth_products", `INSERT INTO wealth_products (name, type, risk_level, annual_rate_bps, min_amount_cents, term_days, tags, description) VALUES (?,?,?,?,?,?,?,?)`,
				p.name, p.typ, p.risk, p.rate, p.min, p.term, p.tags, p.desc); err != nil {
				return err
			}
		}
	}
	return nil
}
