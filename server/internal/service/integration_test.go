package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/audit"
	"lingxi/server/internal/auth"
	"lingxi/server/internal/config"
	"lingxi/server/internal/db"
	"lingxi/server/internal/reqctx"
)

// Integration tests run against a real MySQL. Set TEST_DATABASE_DSN to a
// database whose name contains "test" (guard against pointing at real data):
//
//	TEST_DATABASE_DSN='user:pass@tcp(127.0.0.1:3306)/lingxi_test' go test ./internal/service -run Integration -v

func testService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.Contains(strings.ToLower(cfg.DBName), "test") {
		t.Fatalf("refusing to run: database name %q must contain 'test'", cfg.DBName)
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	c := &config.Config{Location: loc, SingleTransferLimitCents: 100_000_000, DailyTransferLimitCents: 200_000_000}
	return New(d, c, audit.New(d)), d
}

type tuser struct {
	id  uint64
	ctx context.Context
}

func newUser(t *testing.T, d *sql.DB, balance int64) tuser {
	t.Helper()
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	name := "t_" + hex.EncodeToString(b)
	id, err := auth.CreateUser(context.Background(), d, auth.RegisterInput{Username: name, Password: "Test-Passw0rd!", DisplayName: "测试"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE accounts SET balance_cents = ? WHERE user_id = ?`, balance, id); err != nil {
		t.Fatal(err)
	}
	ctx := reqctx.WithUser(context.Background(), &reqctx.User{ID: id, Username: name})
	return tuser{id: id, ctx: ctx}
}

func addContact(t *testing.T, d *sql.DB, uid uint64, name string) uint64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO contacts (user_id, name, phone, birthday) VALUES (?, ?, '13800000000', '06-01')`, uid, name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return uint64(id)
}

func balance(t *testing.T, d *sql.DB, uid uint64) (bal, locked int64) {
	t.Helper()
	if err := d.QueryRow(`SELECT balance_cents, locked_cents FROM accounts WHERE user_id = ?`, uid).Scan(&bal, &locked); err != nil {
		t.Fatal(err)
	}
	return
}

func code(err error) apperr.Code {
	if e, ok := apperr.As(err); ok {
		return e.Code
	}
	return ""
}

// 50 concurrent ¥100 transfers against a ¥1,000 balance: exactly 10 succeed,
// the balance ends at 0, and the journal replays to the same figure.
func TestIntegrationConcurrentTransfersNoOverdraft(t *testing.T) {
	s, d := testService(t)
	u := newUser(t, d, 100_000)
	cid := addContact(t, d, u.id, "并发收款人")

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, insufficient := 0, 0
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Transfer(u.ctx, TransferIn{ContactID: cid, AmountCents: 10_000})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case code(err) == apperr.BadRequest:
				insufficient++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	bal, _ := balance(t, d, u.id)
	if ok != 10 || insufficient != 40 || bal != 0 {
		t.Fatalf("ok=%d insufficient=%d balance=%d", ok, insufficient, bal)
	}
	var out int64
	var minAfter int64
	if err := d.QueryRow(`SELECT COALESCE(SUM(amount_cents),0), COALESCE(MIN(balance_after_cents),0) FROM transactions WHERE user_id = ? AND direction='out'`, u.id).Scan(&out, &minAfter); err != nil {
		t.Fatal(err)
	}
	if out != 100_000 || minAfter < 0 {
		t.Fatalf("journal out=%d minAfter=%d", out, minAfter)
	}
}

func TestIntegrationIDOR(t *testing.T) {
	s, d := testService(t)
	alice := newUser(t, d, 1_000_000)
	bob := newUser(t, d, 1_000_000)
	aliceContact := addContact(t, d, alice.id, "Alice的收款人")

	if _, err := s.Transfer(bob.ctx, TransferIn{ContactID: aliceContact, AmountCents: 100}); code(err) != apperr.NotFound {
		t.Fatalf("bob used alice's contact: %v", err)
	}
	sched, err := s.ScheduleTransfer(alice.ctx, ScheduleIn{ContactID: aliceContact, AmountCents: 100, ExecuteAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelScheduled(bob.ctx, IDIn{ID: sched.ID}); code(err) != apperr.NotFound {
		t.Fatalf("bob cancelled alice's schedule: %v", err)
	}
	list, _ := s.ListContacts(bob.ctx)
	if len(list) != 0 {
		t.Fatalf("bob sees %d foreign contacts", len(list))
	}
	if _, err := s.AccountOverview(context.Background()); !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("anonymous overview: %v", err)
	}
}

func TestIntegrationDailyLimit(t *testing.T) {
	s, d := testService(t)
	s.Cfg.DailyTransferLimitCents = 150_000
	defer func() { s.Cfg.DailyTransferLimitCents = 200_000_000 }()
	u := newUser(t, d, 1_000_000)
	cid := addContact(t, d, u.id, "限额")
	if _, err := s.Transfer(u.ctx, TransferIn{ContactID: cid, AmountCents: 100_000}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transfer(u.ctx, TransferIn{ContactID: cid, AmountCents: 60_000}); code(err) != apperr.BadRequest {
		t.Fatalf("daily limit not enforced: %v", err)
	}
}

func TestIntegrationForwardHoldAndCancel(t *testing.T) {
	s, d := testService(t)
	u := newUser(t, d, 10_000_000)
	if _, err := d.Exec(`INSERT IGNORE INTO fx_rates (pair, currency, buy_rate_bps, sell_rate_bps) VALUES ('USDCNY','USD',71000,71500)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO fx_wallets (user_id, currency, balance_foreign_cents) VALUES (?, 'USD', 1000000)`, u.id); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateForward(u.ctx, ContractIn{Currency: "USD", ForeignAmountCents: 1_000_000, TenorDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	_, locked := balance(t, d, u.id)
	if locked <= 0 {
		t.Fatal("margin not held")
	}
	// the same USD cannot back a second forward
	if _, err := s.CreateForward(u.ctx, ContractIn{Currency: "USD", ForeignAmountCents: 1, TenorDays: 30}); code(err) != apperr.BadRequest {
		t.Fatalf("double-hedged wallet: %v", err)
	}
	if _, err := s.CancelFXContract(u.ctx, IDIn{ID: c.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelFXContract(u.ctx, IDIn{ID: c.ID}); code(err) != apperr.BadRequest {
		t.Fatalf("double cancel: %v", err)
	}
	_, locked = balance(t, d, u.id)
	var flocked int64
	_ = d.QueryRow(`SELECT locked_foreign_cents FROM fx_wallets WHERE user_id = ?`, u.id).Scan(&flocked)
	if locked != 0 || flocked != 0 {
		t.Fatalf("holds not released: cny=%d usd=%d", locked, flocked)
	}
}

func TestIntegrationSuitability(t *testing.T) {
	s, d := testService(t)
	u := newUser(t, d, 10_000_000)
	res, err := d.Exec(`INSERT INTO wealth_products (name, type, risk_level, annual_rate_bps, min_amount_cents) VALUES ('测试高风险', 'index', 'R4', 800, 100)`)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := res.LastInsertId()
	if _, err := s.PurchaseWealth(u.ctx, PurchaseIn{ProductID: uint64(pid), AmountCents: 10_000}); code(err) != apperr.Forbidden {
		t.Fatalf("unassessed user bought R4: %v", err)
	}
	if bal, _ := balance(t, d, u.id); bal != 10_000_000 {
		t.Fatal("money moved despite rejection")
	}
	if _, err := s.SubmitRiskAssessment(u.ctx, RiskIn{Score: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurchaseWealth(u.ctx, PurchaseIn{ProductID: uint64(pid), AmountCents: 10_000}); err != nil {
		t.Fatalf("进取型 should buy R4: %v", err)
	}
}
