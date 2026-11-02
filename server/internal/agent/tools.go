package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"lingxi/server/internal/apperr"
	"lingxi/server/internal/money"
	"lingxi/server/internal/service"
)

// plan is a validated, resolved mutation waiting to run.
type plan struct {
	summary string
	exec    func() (any, error)
}

type tool struct {
	name    string
	desc    string
	params  map[string]any
	confirm bool
	// exactly one of run (read-only) / plan (mutation) is set
	run  func(ctx context.Context, raw json.RawMessage) (any, error)
	plan func(ctx context.Context, raw json.RawMessage) (*plan, any, error) // any = early result (clarification)
}

// ---- JSON schema helpers ----

func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}
func str(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
func num(d string) map[string]any { return map[string]any{"type": "number", "description": d} }
func integer(d string) map[string]any {
	return map[string]any{"type": "integer", "description": d}
}
func boolean(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
func enum(d string, vals ...string) map[string]any {
	return map[string]any{"type": "string", "description": d, "enum": vals}
}

func withConfirm(p map[string]any) map[string]any {
	p["properties"].(map[string]any)["confirmToken"] = str("确认令牌：首次调用不要传；用户明确同意后再次调用时原样带上")
	return p
}

// Yuan parses a JSON number of yuan exactly (no float rounding), max 2 decimals.
type Yuan int64

func (y *Yuan) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	s = strings.ReplaceAll(s, ",", "")
	if s == "" || s == "null" {
		*y = 0
		return nil
	}
	if strings.ContainsAny(s, "eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return apperr.Bad("金额格式不正确")
		}
		s = strconv.FormatFloat(f, 'f', -1, 64)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return apperr.Bad("金额最多保留两位小数")
		}
		frac = frac[:2]
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if len(intPart) > 13 {
		return apperr.Bad("金额超出上限")
	}
	v, err := strconv.ParseInt(intPart+frac, 10, 64)
	if err != nil {
		return apperr.Bad("金额格式不正确")
	}
	if neg {
		v = -v
	}
	*y = Yuan(v)
	return nil
}

func (y Yuan) Cents() int64 { return int64(y) }

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		if e, ok := apperr.As(err); ok {
			return v, e
		}
		return v, apperr.Bad("参数格式错误")
	}
	return v, nil
}

func validate(v any) error {
	if x, ok := v.(interface{ Validate() error }); ok {
		return x.Validate()
	}
	return nil
}

func yuan(c int64) string { return "¥" + money.YuanGrouped(c) }

func fail(msg string) map[string]any { return map[string]any{"成功": false, "原因": msg} }

func (a *Agent) loc(t time.Time) string { return t.In(a.cfg.Location).Format("2006-01-02 15:04") }

// ---- resolvers (return early=clarification when ambiguous) ----

func (a *Agent) resolvePayee(ctx context.Context, q string) (*service.Contact, any, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fail("请提供收款人姓名、手机号或备注"), nil
	}
	list, err := a.svc.SearchContacts(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	switch len(list) {
	case 0:
		return nil, fail(fmt.Sprintf("通讯录里没有找到与「%s」相关的收款人，请先添加收款人", q)), nil
	case 1:
		return &list[0], nil, nil
	}
	cands := make([]map[string]any, 0, len(list))
	for _, c := range list {
		cands = append(cands, map[string]any{"name": c.Name, "phone": c.Phone, "bank": c.BankName, "cardTail": c.CardTail, "relation": c.Relation})
	}
	return nil, map[string]any{"needClarify": true, "candidates": cands, "message": fmt.Sprintf("找到 %d 位可能的收款人，请用户确认是哪一位（可用全名或手机号再次调用）", len(list))}, nil
}

func (a *Agent) resolveCard(ctx context.Context, q string) (*service.Card, any, error) {
	cards, err := a.svc.ListCards(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(cards) == 0 {
		return nil, fail("当前没有任何银行卡"), nil
	}
	q = strings.TrimSpace(q)
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, q)
	var hits []service.Card
	for _, c := range cards {
		switch {
		case q == "":
			hits = append(hits, c)
		case len(digits) >= 4 && c.Last4 == digits[len(digits)-4:]:
			hits = append(hits, c)
		case digits == "" && strings.Contains(q, "信用") && c.CardType == "credit":
			hits = append(hits, c)
		case digits == "" && (strings.Contains(q, "借记") || strings.Contains(q, "储蓄")) && c.CardType == "debit":
			hits = append(hits, c)
		}
	}
	if len(hits) == 1 {
		return &hits[0], nil, nil
	}
	list := make([]string, 0, len(cards))
	for _, c := range cards {
		list = append(list, fmt.Sprintf("%s 尾号%s（%s）", cardTypeText(c.CardType), c.Last4, c.Status))
	}
	if len(hits) == 0 {
		return nil, map[string]any{"成功": false, "原因": fmt.Sprintf("没有匹配「%s」的卡片", q), "卡片列表": list}, nil
	}
	return nil, map[string]any{"成功": false, "原因": "有多张卡，请用户说明卡片尾号", "卡片列表": list}, nil
}

func cardTypeText(t string) string {
	if t == "credit" {
		return "信用卡"
	}
	return "借记卡"
}

func (a *Agent) resolveWealthProduct(ctx context.Context, q string) (*service.WealthProduct, any, error) {
	all, err := a.svc.WealthProducts(ctx)
	if err != nil {
		return nil, nil, err
	}
	q = strings.TrimSpace(q)
	var hits []service.WealthProduct
	for _, p := range all {
		if q != "" && (strings.Contains(p.Name, q) || strconv.FormatUint(p.ID, 10) == q) {
			hits = append(hits, p)
		}
	}
	if len(hits) == 1 {
		return &hits[0], nil, nil
	}
	names := make([]string, 0, len(all))
	for _, p := range all {
		names = append(names, fmt.Sprintf("%s（%s）", p.Name, p.RiskLevel))
	}
	return nil, map[string]any{"成功": false, "原因": fmt.Sprintf("「%s」匹配到 %d 个产品，请用户明确产品名称", q, len(hits)), "可选产品": names}, nil
}

func (a *Agent) resolveSCFProduct(ctx context.Context, q string) (*service.SCFProduct, any, error) {
	all, err := a.svc.SCFProducts(ctx)
	if err != nil {
		return nil, nil, err
	}
	q = strings.TrimSpace(q)
	var hits []service.SCFProduct
	for _, p := range all {
		if q != "" && (strings.Contains(p.Name, q) || strings.EqualFold(p.Code, q)) {
			hits = append(hits, p)
		}
	}
	if len(hits) == 1 {
		return &hits[0], nil, nil
	}
	names := make([]string, 0, len(all))
	for _, p := range all {
		names = append(names, p.Name)
	}
	return nil, map[string]any{"成功": false, "原因": fmt.Sprintf("未能唯一匹配「%s」", q), "可选产品": names}, nil
}

// ---- tool set ----

func (a *Agent) tools() []tool {
	s := a.svc
	return []tool{
		// ===== 账户 / 转账 =====
		{name: "getAccountOverview", desc: "查询账户总览：总余额、可用余额、锁定金额、近期交易、待执行定时转账", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				o, err := s.AccountOverview(ctx)
				if err != nil {
					return nil, err
				}
				recent := make([]string, 0, len(o.Recent))
				for _, t := range o.Recent {
					sign := "-"
					if t.Direction == "in" {
						sign = "+"
					}
					recent = append(recent, fmt.Sprintf("%s %s %s%s %s", a.loc(t.OccurredAt), t.Counterparty, sign, yuan(t.AmountCents), t.Category))
				}
				sched := make([]string, 0, len(o.Scheduled))
				for _, st := range o.Scheduled {
					name := ""
					if st.Contact != nil {
						name = st.Contact.Name
					}
					sched = append(sched, fmt.Sprintf("%s 向%s转账 %s", a.loc(st.ExecuteAt), name, yuan(st.AmountCents)))
				}
				return map[string]any{"户名": o.Account.HolderName, "总余额": yuan(o.Account.BalanceCents), "可用余额": yuan(o.AvailableCents),
					"锁定金额": yuan(o.Account.LockedCents), "近期交易": recent, "待执行定时转账": sched}, nil
			}},
		{name: "searchPayee", desc: "按人名/手机号/备注/关系搜索收款人，返回候选列表", params: obj(map[string]any{"query": str("人名/手机号/备注/关系")}, "query"),
			run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ Query string }](raw)
				if err != nil {
					return nil, err
				}
				q := service.SearchContactsIn{Query: in.Query}
				if err := validate(&q); err != nil {
					return nil, err
				}
				return s.SearchContacts(ctx, q.Query)
			}},
		{name: "transfer", confirm: true, desc: "向收款人实时转账。收款人可以是人名/手机号/备注；匹配多人会返回候选请用户确认",
			params: withConfirm(obj(map[string]any{"payeeQuery": str("收款人的人名/手机号/备注"), "amountYuan": num("转账金额（元）"), "memo": str("转账备注，可选")}, "payeeQuery", "amountYuan")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					PayeeQuery string `json:"payeeQuery"`
					AmountYuan Yuan   `json:"amountYuan"`
					Memo       string `json:"memo"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				c, early, err := a.resolvePayee(ctx, in.PayeeQuery)
				if c == nil {
					return nil, early, err
				}
				req := service.TransferIn{ContactID: c.ID, AmountCents: in.AmountYuan.Cents(), Memo: in.Memo}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{
					summary: fmt.Sprintf("向 %s（%s 尾号%s）实时转账 %s%s", c.Name, deref(c.BankName), deref(c.CardTail), yuan(req.AmountCents), memoText(req.Memo)),
					exec: func() (any, error) {
						r, err := s.Transfer(ctx, req)
						if err != nil {
							return nil, err
						}
						return map[string]any{"成功": true, "收款人": c.Name, "金额": yuan(req.AmountCents), "流水号": r.TxID, "余额": yuan(r.BalanceCents)}, nil
					},
				}, nil, nil
			}},
		{name: "scheduleTransfer", confirm: true, desc: "创建定时转账，在指定时间自动执行",
			params: withConfirm(obj(map[string]any{"payeeQuery": str("收款人"), "amountYuan": num("金额（元）"),
				"executeAt": str("执行时间，ISO 8601 含时区，如 2026-11-05T09:00:00+08:00"), "memo": str("备注，可选")}, "payeeQuery", "amountYuan", "executeAt")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					PayeeQuery string `json:"payeeQuery"`
					AmountYuan Yuan   `json:"amountYuan"`
					ExecuteAt  string `json:"executeAt"`
					Memo       string `json:"memo"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				at, err := time.Parse(time.RFC3339, in.ExecuteAt)
				if err != nil {
					if at, err = time.ParseInLocation("2006-01-02T15:04:05", in.ExecuteAt, a.cfg.Location); err != nil {
						return nil, fail("无法解析执行时间，请使用 ISO 8601 格式"), nil
					}
				}
				c, early, err := a.resolvePayee(ctx, in.PayeeQuery)
				if c == nil {
					return nil, early, err
				}
				req := service.ScheduleIn{ContactID: c.ID, AmountCents: in.AmountYuan.Cents(), Memo: in.Memo, ExecuteAt: at}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{
					summary: fmt.Sprintf("于 %s 向 %s 定时转账 %s%s", a.loc(at), c.Name, yuan(req.AmountCents), memoText(req.Memo)),
					exec: func() (any, error) {
						r, err := s.ScheduleTransfer(ctx, req)
						if err != nil {
							return nil, err
						}
						return map[string]any{"成功": true, "任务号": r.ID, "收款人": c.Name, "金额": yuan(req.AmountCents), "执行时间": a.loc(r.ExecuteAt)}, nil
					},
				}, nil, nil
			}},
		{name: "createAASplit", desc: "创建 AA 收款：把一笔总账单按人数均摊，向参与者发起收款（不动账）",
			params: obj(map[string]any{"title": str("如「部门聚餐·海底捞」"), "totalYuan": num("总金额（元）"),
				"participantNames": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "参与人名列表（不含发起人）"}}, "title", "totalYuan", "participantNames"),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					Title            string   `json:"title"`
					TotalYuan        Yuan     `json:"totalYuan"`
					ParticipantNames []string `json:"participantNames"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				req := service.AAIn{Title: in.Title, TotalCents: in.TotalYuan.Cents()}
				for _, n := range in.ParticipantNames {
					req.Participants = append(req.Participants, service.AAParticipantIn{Name: n})
				}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{exec: func() (any, error) {
					r, err := s.CreateAASplit(ctx, req)
					if err != nil {
						return nil, err
					}
					each := make([]string, 0, len(r.Participants))
					for _, p := range r.Participants {
						each = append(each, p.Name+" "+yuan(p.AmountCents))
					}
					return map[string]any{"成功": true, "收款单号": r.ID, "每人应付": each, "合计": yuan(req.TotalCents)}, nil
				}}, nil, nil
			}},

		// ===== 账单 =====
		{name: "getMonthlyBill", desc: "月度账单分析：收支总额、分类占比、大额商户、环比。缺省为本月",
			params: obj(map[string]any{"year": integer("年份，如 2026"), "month": integer("月份 1-12")}),
			run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ Year, Month int }](raw)
				if err != nil {
					return nil, err
				}
				now := time.Now().In(a.cfg.Location)
				if in.Year == 0 {
					in.Year = now.Year()
				}
				if in.Month == 0 {
					in.Month = int(now.Month())
				}
				v := service.MonthlyBillIn{Year: in.Year, Month: in.Month}
				if err := validate(&v); err != nil {
					return nil, err
				}
				b, err := s.MonthlyBill(ctx, in.Year, in.Month)
				if err != nil {
					return nil, err
				}
				cats := make([]string, 0, len(b.TopCategories))
				for _, c := range b.TopCategories {
					cats = append(cats, fmt.Sprintf("%s %s（%.1f%%）", c.Category, yuan(c.Cents), c.Pct))
				}
				cps := make([]string, 0, len(b.TopCounterparties))
				for _, c := range b.TopCounterparties {
					cps = append(cps, c.Name+" "+yuan(c.Cents))
				}
				return map[string]any{"月份": fmt.Sprintf("%d年%d月", b.Year, b.Month), "收入": yuan(b.TotalInCents), "支出": yuan(b.TotalOutCents),
					"笔数": b.Count, "分类": cats, "大额对手方": cps, "上月支出": yuan(b.PrevTotalOutCents), "环比百分比": b.MomPct}, nil
			}},
		{name: "detectAnomalies", desc: "扫描近期交易中的异常/可疑交易（夜间大额、陌生对手方、高频异常），用于安全提醒",
			params: obj(map[string]any{"days": integer("回溯天数 7-365，默认 90")}),
			run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ Days int }](raw)
				if err != nil {
					return nil, err
				}
				v := service.AnomaliesIn{Days: in.Days}
				if err := validate(&v); err != nil {
					return nil, err
				}
				list, err := s.DetectAnomalies(ctx, v.Days)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(list))
				for i, it := range list {
					if i >= 20 {
						break
					}
					out = append(out, map[string]any{"时间": a.loc(it.Tx.OccurredAt), "对手方": it.Tx.Counterparty, "金额": yuan(it.Tx.AmountCents), "原因": it.Reasons})
				}
				return map[string]any{"异常笔数": len(list), "明细": out}, nil
			}},

		// ===== 理财 =====
		{name: "getRiskProfile", desc: "查询用户最近一次（一年内有效）理财风险测评结果", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				p, err := s.LatestRiskProfile(ctx)
				if err != nil || p == nil {
					return map[string]any{"已测评": false, "说明": "未测评或已过期，按稳健型（最高 R2）进行适当性校验"}, err
				}
				return map[string]any{"已测评": true, "等级": p.Level, "分数": p.Score, "测评时间": a.loc(p.CreatedAt)}, nil
			}},
		{name: "submitRiskAssessment", confirm: true, desc: "提交风险测评结果（必须是用户本人逐题作答得出的分数，不得代为估算）。score 0-100",
			params: withConfirm(obj(map[string]any{"score": integer("测评总分 0-100")}, "score")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct{ Score int }](raw)
				if err != nil {
					return nil, nil, err
				}
				req := service.RiskIn{Score: in.Score, Answers: map[string]string{"source": "agent"}}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{summary: fmt.Sprintf("录入风险测评：%d 分", in.Score), exec: func() (any, error) {
					r, err := s.SubmitRiskAssessment(ctx, req)
					if err != nil {
						return nil, err
					}
					return map[string]any{"成功": true, "等级": r.Level, "分数": r.Score}, nil
				}}, nil, nil
			}},
		{name: "recommendWealth", desc: "根据用户风险测评结果推荐理财产品（含收益率、风险等级、起购金额、期限）", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				r, err := s.RecommendWealth(ctx, 4)
				if err != nil {
					return nil, err
				}
				level := "未测评（按稳健型）"
				if r.Profile != nil {
					level = r.Profile.Level
				}
				return map[string]any{"测评等级": level, "可购最高风险": r.MaxRisk, "推荐": wealthList(r.Products)}, nil
			}},
		{name: "purchaseWealth", confirm: true, desc: "申购理财产品。会做适当性校验（风险等级不可超过测评结果）和余额校验",
			params: withConfirm(obj(map[string]any{"productName": str("产品名称关键词或产品 ID"), "amountYuan": num("申购金额（元）")}, "productName", "amountYuan")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					ProductName string `json:"productName"`
					AmountYuan  Yuan   `json:"amountYuan"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				p, early, err := a.resolveWealthProduct(ctx, in.ProductName)
				if p == nil {
					return nil, early, err
				}
				req := service.PurchaseIn{ProductID: p.ID, AmountCents: in.AmountYuan.Cents()}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{summary: fmt.Sprintf("申购「%s」（%s，年化 %.2f%%）%s", p.Name, p.RiskLevel, float64(p.AnnualRateBps)/100, yuan(req.AmountCents)),
					exec: func() (any, error) {
						r, err := s.PurchaseWealth(ctx, req)
						if err != nil {
							return nil, err
						}
						return map[string]any{"成功": true, "产品": r.Product, "金额": yuan(req.AmountCents), "合并持仓": r.Merged, "活期余额": yuan(r.BalanceCents)}, nil
					}}, nil, nil
			}},
		{name: "redeemWealth", confirm: true, desc: "赎回理财持仓（默认全额赎回，含收益）",
			params: withConfirm(obj(map[string]any{"productName": str("持仓产品名称关键词"), "amountYuan": num("赎回金额（元），不传为全额")}, "productName")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					ProductName string `json:"productName"`
					AmountYuan  Yuan   `json:"amountYuan"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				hs, err := s.Holdings(ctx)
				if err != nil {
					return nil, nil, err
				}
				var hit []service.Holding
				for _, h := range hs {
					if h.Product != nil && strings.Contains(h.Product.Name, strings.TrimSpace(in.ProductName)) {
						hit = append(hit, h)
					}
				}
				if len(hit) != 1 {
					names := []string{}
					for _, h := range hs {
						if h.Product != nil {
							names = append(names, h.Product.Name+" "+yuan(h.AmountCents))
						}
					}
					return nil, map[string]any{"成功": false, "原因": "未能唯一匹配持仓，请用户明确产品", "当前持仓": names}, nil
				}
				h := hit[0]
				req := service.RedeemIn{HoldingID: h.ID}
				amt := h.AmountCents
				if c := in.AmountYuan.Cents(); c > 0 {
					req.AmountCents = &c
					amt = min(c, h.AmountCents)
				}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{summary: fmt.Sprintf("赎回「%s」%s", h.Product.Name, yuan(amt)), exec: func() (any, error) {
					r, err := s.RedeemWealth(ctx, req)
					if err != nil {
						return nil, err
					}
					return map[string]any{"成功": true, "产品": r.Product, "赎回本金": yuan(r.RedeemedCents), "收益": yuan(r.ProfitCents)}, nil
				}}, nil, nil
			}},

		// ===== 卡片 =====
		{name: "listCards", desc: "查询名下银行卡：类型、尾号、额度、状态、交易限制", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				cards, err := s.ListCards(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(cards))
				for _, c := range cards {
					m := map[string]any{"类型": cardTypeText(c.CardType), "尾号": c.Last4, "状态": c.Status, "单日限额": yuan(c.DailyLimitCents), "线上支付": c.OnlinePayEnabled}
					if c.CreditLimitCents != nil {
						m["信用额度"], m["已用额度"] = yuan(*c.CreditLimitCents), yuan(c.UsedCents)
					}
					out = append(out, m)
				}
				return out, nil
			}},
		{name: "applyCard", confirm: true, desc: "申请新卡（借记卡/信用卡）",
			params: withConfirm(obj(map[string]any{"cardType": enum("卡片类型", "debit", "credit")}, "cardType")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct{ CardType string }](raw)
				if err != nil {
					return nil, nil, err
				}
				req := service.ApplyCardIn{CardType: in.CardType}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{summary: "申请一张" + cardTypeText(in.CardType), exec: func() (any, error) {
					r, err := s.ApplyCard(ctx, req)
					if err != nil {
						return nil, err
					}
					return map[string]any{"成功": true, "尾号": r.Last4, "说明": r.Message}, nil
				}}, nil, nil
			}},
		{name: "manageCard", confirm: true, desc: "卡片管理：挂失/冻结/解冻/限制交易/解除限制/调整额度/开关线上支付。挂失不可撤销",
			params: withConfirm(obj(map[string]any{
				"cardQuery":  str("卡尾号或「信用卡」「借记卡」"),
				"action":     enum("操作", "freeze", "unfreeze", "reportLost", "restrict", "unrestrict", "setDailyLimit", "setCreditLimit", "toggleOnlinePay"),
				"amountYuan": num("调整额度时的目标金额（元）"),
				"enabled":    boolean("开关线上支付的目标状态")}, "action")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					CardQuery  string `json:"cardQuery"`
					Action     string `json:"action"`
					AmountYuan Yuan   `json:"amountYuan"`
					Enabled    *bool  `json:"enabled"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				c, early, err := a.resolveCard(ctx, in.CardQuery)
				if c == nil {
					return nil, early, err
				}
				label := cardTypeText(c.CardType) + " 尾号" + c.Last4
				switch in.Action {
				case "freeze", "unfreeze", "reportLost", "restrict", "unrestrict":
					req := service.SetCardStatusIn{CardID: c.ID, Action: in.Action}
					if err := validate(&req); err != nil {
						return nil, nil, err
					}
					names := map[string]string{"freeze": "临时冻结", "unfreeze": "解除冻结", "reportLost": "挂失（不可撤销）", "restrict": "限制交易", "unrestrict": "解除交易限制"}
					return &plan{summary: names[in.Action] + " " + label, exec: func() (any, error) {
						r, err := s.SetCardStatus(ctx, req)
						if err != nil {
							return nil, err
						}
						return map[string]any{"成功": true, "说明": r.Message}, nil
					}}, nil, nil
				case "setDailyLimit", "setCreditLimit":
					v := in.AmountYuan.Cents()
					req := service.AdjustLimitIn{CardID: c.ID}
					if in.Action == "setDailyLimit" {
						req.DailyLimitCents = &v
					} else {
						if c.CardType != "credit" {
							return nil, fail("借记卡没有信用额度，可调整单日限额"), nil
						}
						req.CreditLimitCents = &v
					}
					if err := validate(&req); err != nil {
						return nil, nil, err
					}
					return &plan{summary: fmt.Sprintf("将%s的%s调整为 %s", label, map[string]string{"setDailyLimit": "单日限额", "setCreditLimit": "信用额度"}[in.Action], yuan(v)),
						exec: func() (any, error) {
							if _, err := s.AdjustLimit(ctx, req); err != nil {
								return nil, err
							}
							return map[string]any{"成功": true, "卡片": label, "新额度": yuan(v)}, nil
						}}, nil, nil
				case "toggleOnlinePay":
					en := !c.OnlinePayEnabled
					if in.Enabled != nil {
						en = *in.Enabled
					}
					req := service.ToggleOnlinePayIn{CardID: c.ID, Enabled: en}
					return &plan{summary: fmt.Sprintf("%s%s的线上支付", map[bool]string{true: "开启", false: "关闭"}[en], label), exec: func() (any, error) {
						if _, err := s.ToggleOnlinePay(ctx, req); err != nil {
							return nil, err
						}
						return map[string]any{"成功": true, "卡片": label, "线上支付": en}, nil
					}}, nil, nil
				}
				return nil, fail("不支持的操作"), nil
			}},

		// ===== 跨场景 =====
		{name: "setupBirthdayGuard", confirm: true, desc: "创建生日守护：读取联系人生日 → 生日当月锁定指定金额活期 → 生日前 N 天用专款自动下单鲜花和蛋糕 → 生日当天解锁余款",
			params: withConfirm(obj(map[string]any{"contactQuery": str("联系人（人名/备注/关系，如「爱人」）"), "lockYuan": num("当月锁定金额（元），默认 1000"),
				"orderLeadDays": integer("生日前几天下单，1-7，默认 2")}, "contactQuery")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					ContactQuery  string `json:"contactQuery"`
					LockYuan      Yuan   `json:"lockYuan"`
					OrderLeadDays int    `json:"orderLeadDays"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				c, early, err := a.resolvePayee(ctx, in.ContactQuery)
				if c == nil {
					return nil, early, err
				}
				req := service.BirthdayIn{ContactQuery: c.Name, LockCents: in.LockYuan.Cents(), OrderLeadDays: in.OrderLeadDays}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{summary: fmt.Sprintf("为%s（生日 %s）开启生日守护：生日当月锁定 %s，提前 %d 天用专款下单鲜花蛋糕", c.Name, deref(c.Birthday), yuan(req.LockCents), req.OrderLeadDays),
					exec: func() (any, error) {
						r, err := s.CreateBirthdayGuard(ctx, req)
						if err != nil {
							return nil, err
						}
						return map[string]any{"成功": true, "任务号": r.ID, "联系人": r.Contact.Name, "已触发": r.Events}, nil
					}}, nil, nil
			}},
		{name: "listScenarios", desc: "查询所有跨场景守护任务及礼品订单状态", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				list, err := s.ListScenarios(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(list))
				for _, sc := range list {
					last := ""
					if len(sc.Log) > 0 {
						last = sc.Log[len(sc.Log)-1].Text
					}
					out = append(out, map[string]any{"任务": sc.Title, "状态": sc.Status, "生日": sc.Payload.Birthday, "锁定": yuan(sc.Payload.LockCents), "最新进展": last, "订单数": len(sc.Orders)})
				}
				return out, nil
			}},

		// ===== 外汇 =====
		{name: "getFxRates", desc: "查询实时外汇牌价（结汇价=银行买入，购汇价=银行卖出）", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				rs, err := s.FXRates(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]string, 0, len(rs))
				for _, r := range rs {
					out = append(out, fmt.Sprintf("%s%s 结汇 %s / 购汇 %s", r.Name, r.Currency, money.Rate(r.BuyBps), money.Rate(r.SellBps)))
				}
				return out, nil
			}},
		{name: "getFxWallets", desc: "查询多币种钱包余额及折合人民币", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				ws, err := s.FXWallets(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]string, 0, len(ws))
				for _, w := range ws {
					out = append(out, fmt.Sprintf("%s %s%s（其中锁定 %s）≈ %s", w.Currency, w.Symbol, money.YuanGrouped(w.BalanceForeignCents), money.YuanGrouped(w.LockedForeignCents), yuan(w.CnyCents)))
				}
				return out, nil
			}},
		{name: "getFxExposure", desc: "分析汇率风险敞口：各币种资产折人民币、已对冲比例、未对冲敞口", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				e, err := s.FXExposure(ctx)
				if err != nil {
					return nil, err
				}
				items := make([]string, 0, len(e.Items))
				for _, it := range e.Items {
					items = append(items, fmt.Sprintf("%s：%s，已对冲 %s（%.0f%%）", it.Currency, yuan(it.CnyCents), yuan(it.HedgedCnyCents), it.HedgeRatio*100))
				}
				return map[string]any{"外币资产合计": yuan(e.TotalCnyCents), "已对冲": yuan(e.TotalHedgedCnyCents), "未对冲敞口": yuan(e.OpenCnyCents), "对冲比例": fmt.Sprintf("%.0f%%", e.HedgeRatio*100), "明细": items}, nil
			}},
		a.fxTool("lockForwardRate", "远期锁汇：锁定未来交割汇率，冻结 5% 人民币保证金（结汇方向同时锁定对应外币）", false),
		a.fxTool("buyFxOption", "购买外汇期权：支付 1.5% 期权费（不退），获得到期按约定汇率交割的权利，适合回款时点不确定的敞口", true),
		{name: "listFxContracts", desc: "查询我的外汇衍生品合约（远期/期权）及状态", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				cs, err := s.FXContracts(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]string, 0, len(cs))
				for i, c := range cs {
					if i >= 20 {
						break
					}
					out = append(out, fmt.Sprintf("#%d %s %s %s @%s 到期 %s %s", c.ID, map[string]string{"forward": "远期", "option": "期权", "swap": "掉期"}[c.Type],
						c.Currency, money.YuanGrouped(c.ForeignAmountCents), money.Rate(c.LockedRateBps), a.loc(c.ExpiresAt), c.Status))
				}
				return out, nil
			}},

		// ===== 供应链金融 =====
		{name: "listScfProducts", desc: "查询供应链金融产品（跨境订单贷/海外仓仓单质押贷/出口退税贷/出口信保融资）", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				ps, err := s.SCFProducts(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]string, 0, len(ps))
				for _, p := range ps {
					out = append(out, fmt.Sprintf("%s：年化 %.2f%%，额度上限 %s，期限 %d 天。%s", p.Name, float64(p.RateBps)/100, yuan(p.MaxAmountCents), p.TermDays, deref(p.Description)))
				}
				return out, nil
			}},
		{name: "applyScfLoan", confirm: true, desc: "申请供应链金融融资（演示环境即时审批放款到活期）",
			params: withConfirm(obj(map[string]any{"productName": str("产品名关键词，如「订单贷」「仓单」「退税」「信保」"), "amountYuan": num("申请金额（元）"),
				"purpose": str("用途，如「黑五备货」"), "relatedRef": str("关联订单号/报关单号/退税期")}, "productName", "amountYuan")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					ProductName string `json:"productName"`
					AmountYuan  Yuan   `json:"amountYuan"`
					Purpose     string `json:"purpose"`
					RelatedRef  string `json:"relatedRef"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				p, early, err := a.resolveSCFProduct(ctx, in.ProductName)
				if p == nil {
					return nil, early, err
				}
				req := service.ApplyLoanIn{ProductID: p.ID, AmountCents: in.AmountYuan.Cents(), Purpose: in.Purpose, RelatedRef: in.RelatedRef}
				if err := validate(&req); err != nil {
					return nil, nil, err
				}
				return &plan{summary: fmt.Sprintf("申请「%s」%s，年化 %.2f%%，期限 %d 天", p.Name, yuan(req.AmountCents), float64(p.RateBps)/100, p.TermDays),
					exec: func() (any, error) {
						r, err := s.ApplyLoan(ctx, req)
						if err != nil {
							return nil, err
						}
						return map[string]any{"成功": true, "融资号": r.ID, "产品": r.Product, "放款金额": yuan(req.AmountCents), "到期日": a.loc(r.DueAt)}, nil
					}}, nil, nil
			}},
		{name: "repayScfLoan", confirm: true, desc: "归还供应链融资（本金+按天计息，从活期扣款）",
			params: withConfirm(obj(map[string]any{"productName": str("要归还的融资产品名关键词")}, "productName")),
			plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
				in, err := decode[struct {
					ProductName string `json:"productName"`
				}](raw)
				if err != nil {
					return nil, nil, err
				}
				loans, err := s.MyLoans(ctx)
				if err != nil {
					return nil, nil, err
				}
				var hit []service.SCFLoan
				for _, l := range loans {
					if l.Status != "repaid" && l.Product != nil && strings.Contains(l.Product.Name, strings.TrimSpace(in.ProductName)) {
						hit = append(hit, l)
					}
				}
				if len(hit) != 1 {
					return nil, fail(fmt.Sprintf("未能唯一匹配「%s」的在途融资", in.ProductName)), nil
				}
				l := hit[0]
				interest, days := service.LoanInterest(l.AmountCents, l.RateBps, l.AppliedAt, time.Now())
				req := service.RepayIn{LoanID: l.ID}
				return &plan{summary: fmt.Sprintf("提前结清「%s」：本金 %s + 利息约 %s（%d 天），合计约 %s", l.Product.Name, yuan(l.AmountCents), yuan(interest), days, yuan(l.AmountCents+interest)),
					exec: func() (any, error) {
						r, err := s.RepayLoan(ctx, req)
						if err != nil {
							return nil, err
						}
						return map[string]any{"成功": true, "说明": r.Message}, nil
					}}, nil, nil
			}},
		{name: "listScfLoans", desc: "查询我的供应链融资记录（在途/已结清/逾期）", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				ls, err := s.MyLoans(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]string, 0, len(ls))
				for i, l := range ls {
					if i >= 20 {
						break
					}
					name := ""
					if l.Product != nil {
						name = l.Product.Name
					}
					out = append(out, fmt.Sprintf("#%d %s %s 年化 %.2f%% 到期 %s %s", l.ID, name, yuan(l.AmountCents), float64(l.RateBps)/100, a.loc(l.DueAt), l.Status))
				}
				return out, nil
			}},

		// ===== 外贸 =====
		{name: "listShipments", desc: "查询外贸物流运单列表（承运商/平台/目的地/状态/最新轨迹）", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				ss, err := s.ListShipments(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(ss))
				for i, sh := range ss {
					if i >= 20 {
						break
					}
					latest := ""
					if n := len(sh.Milestones); n > 0 {
						latest = sh.Milestones[n-1].Text
					}
					out = append(out, map[string]any{"单号": sh.TrackingNo, "承运商": sh.Carrier, "平台": sh.Platform, "目的地": sh.Destination, "状态": sh.Status, "最新轨迹": latest})
				}
				return out, nil
			}},
		{name: "trackShipment", desc: "按运单号查询物流完整轨迹", params: obj(map[string]any{"trackingNo": str("运单号")}, "trackingNo"),
			run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					TrackingNo string `json:"trackingNo"`
				}](raw)
				if err != nil {
					return nil, err
				}
				return s.TrackShipment(ctx, in.TrackingNo)
			}},
		{name: "listCustoms", desc: "查询海关报关单状态（申报/查验/放行）及关联退税（金额字段单位为分）", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) { return s.ListCustoms(ctx) }},
		{name: "getTaxRefunds", desc: "查询出口退税进度（申报期/金额/进度百分比/状态，金额字段单位为分）", params: obj(map[string]any{}),
			run: func(ctx context.Context, _ json.RawMessage) (any, error) { return s.ListTaxRefunds(ctx) }},
	}
}

func (a *Agent) fxTool(name, desc string, option bool) tool {
	return tool{name: name, desc: desc, confirm: true,
		params: withConfirm(obj(map[string]any{
			"currency":      enum("外币币种", "USD", "EUR", "GBP", "HKD", "JPY"),
			"foreignAmount": num("外币金额（如 50000 表示 5 万美元）"),
			"tenorDays":     integer("期限天数 7-365"),
			"direction":     enum("sell_foreign=结汇（卖外币收人民币，默认），buy_foreign=购汇", "sell_foreign", "buy_foreign"),
		}, "currency", "foreignAmount", "tenorDays")),
		plan: func(ctx context.Context, raw json.RawMessage) (*plan, any, error) {
			in, err := decode[struct {
				Currency      string `json:"currency"`
				ForeignAmount Yuan   `json:"foreignAmount"`
				TenorDays     int    `json:"tenorDays"`
				Direction     string `json:"direction"`
			}](raw)
			if err != nil {
				return nil, nil, err
			}
			req := service.ContractIn{Currency: in.Currency, ForeignAmountCents: in.ForeignAmount.Cents(), TenorDays: in.TenorDays, Direction: in.Direction}
			if err := validate(&req); err != nil {
				return nil, nil, err
			}
			kind, fee := "远期锁汇", "冻结 5% 保证金"
			if option {
				kind, fee = "外汇期权", "支付 1.5% 期权费（不退）"
			}
			dir := map[string]string{"sell_foreign": "结汇", "buy_foreign": "购汇"}[req.Direction]
			return &plan{summary: fmt.Sprintf("%s：%s %s %s，期限 %d 天，按当前牌价锁定，%s", kind, dir, req.Currency, money.YuanGrouped(req.ForeignAmountCents), req.TenorDays, fee),
				exec: func() (any, error) {
					var r *service.ContractOut
					var err error
					if option {
						r, err = a.svc.CreateOption(ctx, req)
					} else {
						r, err = a.svc.CreateForward(ctx, req)
					}
					if err != nil {
						return nil, err
					}
					return map[string]any{"成功": true, "合约号": r.ID, "锁定汇率": r.LockedRate, "说明": r.Message}, nil
				}}, nil, nil
		}}
}

func wealthList(ps []service.WealthProduct) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		term := "灵活申赎"
		if p.TermDays > 0 {
			term = fmt.Sprintf("%d 天", p.TermDays)
		}
		out = append(out, fmt.Sprintf("%s｜%s｜业绩基准 %.2f%%｜起购 %s｜%s", p.Name, p.RiskLevel, float64(p.AnnualRateBps)/100, yuan(p.MinAmountCents), term))
	}
	return out
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func memoText(m string) string {
	if m == "" {
		return ""
	}
	return "，备注「" + m + "」"
}
