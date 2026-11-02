package service

import (
	"database/sql"
	"encoding/json"
	"time"
	"unicode/utf8"

	"lingxi/server/internal/apperr"
)

// JSON field names mirror the old Drizzle rows so the React pages read the same keys.

type Account struct {
	ID           uint64    `json:"id"`
	HolderName   string    `json:"holderName"`
	BalanceCents int64     `json:"balanceCents"`
	LockedCents  int64     `json:"lockedCents"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Transaction struct {
	ID                uint64    `json:"id"`
	Direction         string    `json:"direction"`
	AmountCents       int64     `json:"amountCents"`
	BalanceAfterCents int64     `json:"balanceAfterCents"`
	Category          string    `json:"category"`
	Counterparty      string    `json:"counterparty"`
	Memo              *string   `json:"memo"`
	Channel           string    `json:"channel"`
	Status            string    `json:"status"`
	IsAnomaly         bool      `json:"isAnomaly"`
	AnomalyReason     *string   `json:"anomalyReason"`
	OccurredAt        time.Time `json:"occurredAt"`
}

const txColumns = `id, direction, amount_cents, balance_after_cents, category, counterparty, memo, channel, status, is_anomaly, anomaly_reason, occurred_at`

func scanTx(sc interface{ Scan(...any) error }) (Transaction, error) {
	var t Transaction
	var memo, reason sql.NullString
	err := sc.Scan(&t.ID, &t.Direction, &t.AmountCents, &t.BalanceAfterCents, &t.Category, &t.Counterparty, &memo, &t.Channel, &t.Status, &t.IsAnomaly, &reason, &t.OccurredAt)
	t.Memo, t.AnomalyReason = ns(memo), ns(reason)
	return t, err
}

type Contact struct {
	ID       uint64  `json:"id"`
	Name     string  `json:"name"`
	Phone    *string `json:"phone"`
	BankName *string `json:"bankName"`
	CardTail *string `json:"cardTail"`
	Relation *string `json:"relation"`
	Birthday *string `json:"birthday"`
	Note     *string `json:"note"`
}

const contactColumns = `id, name, phone, bank_name, card_tail, relation, birthday, note`

func scanContact(sc interface{ Scan(...any) error }) (Contact, error) {
	var c Contact
	var phone, bank, tail, rel, bday, note sql.NullString
	err := sc.Scan(&c.ID, &c.Name, &phone, &bank, &tail, &rel, &bday, &note)
	c.Phone, c.BankName, c.CardTail, c.Relation, c.Birthday, c.Note = ns(phone), ns(bank), ns(tail), ns(rel), ns(bday), ns(note)
	return c, err
}

type ScheduledTransfer struct {
	ID          uint64    `json:"id"`
	ContactID   uint64    `json:"contactId"`
	AmountCents int64     `json:"amountCents"`
	Memo        *string   `json:"memo"`
	ExecuteAt   time.Time `json:"executeAt"`
	Status      string    `json:"status"`
	FailReason  *string   `json:"failReason"`
	CreatedAt   time.Time `json:"createdAt"`
	Contact     *Contact  `json:"contact"`
}

type AAParticipant struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	AmountCents int64  `json:"amountCents"`
	Paid        bool   `json:"paid"`
}

type AASplit struct {
	ID           uint64          `json:"id"`
	Title        string          `json:"title"`
	TotalCents   int64           `json:"totalCents"`
	Participants []AAParticipant `json:"participants"`
	Status       string          `json:"status"`
	CreatedAt    time.Time       `json:"createdAt"`
}

type Card struct {
	ID               uint64    `json:"id"`
	CardType         string    `json:"cardType"`
	BankName         string    `json:"bankName"`
	Last4            string    `json:"last4"`
	CreditLimitCents *int64    `json:"creditLimitCents"`
	UsedCents        int64     `json:"usedCents"`
	DailyLimitCents  int64     `json:"dailyLimitCents"`
	Status           string    `json:"status"`
	OnlinePayEnabled bool      `json:"onlinePayEnabled"`
	CreatedAt        time.Time `json:"createdAt"`
}

func ns(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func ni(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func nt(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ---- input validation helpers ----

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func checkLen(field, s string, min, max int) error {
	if n := runeLen(s); n < min || n > max {
		if min > 0 && n == 0 {
			return apperr.Bad("%s不能为空", field)
		}
		return apperr.Bad("%s长度需在 %d-%d 个字符之间", field, min, max)
	}
	return nil
}

func checkID(field string, id uint64) error {
	if id == 0 {
		return apperr.Bad("%s无效", field)
	}
	return nil
}

func checkCents(field string, v, max int64) error {
	if v <= 0 {
		return apperr.Bad("%s必须大于 0", field)
	}
	if v > max {
		return apperr.Bad("%s超出上限", field)
	}
	return nil
}
