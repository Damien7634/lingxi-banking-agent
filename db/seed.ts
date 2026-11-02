import "dotenv/config";
import { getDb } from "../api/queries/connection";
import {
  accounts,
  transactions,
  contacts,
  scheduledTransfers,
  aaSplits,
  cards,
  wealthProducts,
  holdings,
  riskProfiles,
  scenarios,
  giftOrders,
  agentMessages,
} from "./schema";

const NOW = new Date("2026-11-01T09:30:00+08:00");
const day = (offset: number, h = 12, m = 0) => {
  const d = new Date(NOW);
  d.setDate(d.getDate() + offset);
  d.setHours(h, m, 0, 0);
  return d;
};

async function main() {
  const db = getDb();

  // 清空旧数据（演示种子脚本，幂等重跑）
  await db.delete(agentMessages);
  await db.delete(giftOrders);
  await db.delete(scenarios);
  await db.delete(riskProfiles);
  await db.delete(holdings);
  await db.delete(wealthProducts);
  await db.delete(cards);
  await db.delete(aaSplits);
  await db.delete(scheduledTransfers);
  await db.delete(contacts);
  await db.delete(transactions);
  await db.delete(accounts);

  // ---------- 账户 ----------
  await db.insert(accounts).values({
    holderName: "张伟",
    balanceCents: 8652040, // ¥86,520.40
    lockedCents: 0,
  });

  // ---------- 收款人 ----------
  await db.insert(contacts).values([
    {
      name: "李娜",
      phone: "13800138000",
      bankName: "招商银行",
      cardTail: "6688",
      relation: "爱人",
      birthday: "11-18",
      note: "老婆",
    },
    {
      name: "王强",
      phone: "13912345678",
      bankName: "工商银行",
      cardTail: "2210",
      relation: "同事",
      birthday: null,
      note: "工位旁边",
    },
    {
      name: "陈晨",
      phone: "13798765432",
      bankName: "建设银行",
      cardTail: "8899",
      relation: "朋友",
      birthday: "03-22",
      note: "大学室友",
    },
    {
      name: "周敏",
      phone: "13655556666",
      bankName: "建设银行",
      cardTail: "3301",
      relation: "房东",
      birthday: null,
      note: "每月5号交房租",
    },
    {
      name: "张秀兰",
      phone: "13900001111",
      bankName: "农业银行",
      cardTail: "1122",
      relation: "家人",
      birthday: "07-09",
      note: "妈妈",
    },
  ]);
  const allContacts = await db.query.contacts.findMany();
  const byName = Object.fromEntries(allContacts.map((c) => [c.name, c]));

  // ---------- 交易流水（近 3 个月） ----------
  type Tx = typeof transactions.$inferInsert;
  const txs: Tx[] = [];
  const add = (t: Tx) => txs.push(t);

  // 每月固定收入
  for (const m of [8, 9, 10]) {
    add({
      direction: "in",
      amountCents: 2850000,
      category: "工资",
      counterparty: "云启科技有限公司",
      memo: `${m}月工资`,
      channel: "代发",
      occurredAt: new Date(`2026-${String(m).padStart(2, "0")}-10T10:05:00+08:00`),
    });
    add({
      direction: "out",
      amountCents: 450000,
      category: "住房",
      counterparty: "周敏",
      memo: "房租",
      channel: "手机银行",
      occurredAt: new Date(`2026-${String(m).padStart(2, "0")}-05T09:12:00+08:00`),
    });
  }

  const spend: Array<[number, number, string, string, string, string?, number?, number?]> = [
    // [dayOffset, amountCents, category, counterparty, memo, channel, hour, minute]
    [-88, 8600, "餐饮", "海底捞火锅(大悦城店)", "部门聚餐", "支付宝", 19, 42],
    [-85, 15200, "购物", "京东", "空气炸锅", "微信", 21, 3],
    [-82, 4600, "交通", "滴滴出行", "加班打车", "微信", 22, 48],
    [-80, 12900, "餐饮", "星巴克(国贸店)", "咖啡豆+早餐", "支付宝", 8, 35],
    [-78, 38000, "购物", "Apple Store官方旗舰店", "AirPods Pro", "支付宝", 15, 20],
    [-75, 21000, "娱乐", "万达影城", "电影票x3", "微信", 19, 10],
    [-73, 9800, "医疗", "同仁堂药店", "感冒药", "微信", 11, 26],
    [-70, 66000, "教育", "得到App", "年度会员", "支付宝", 9, 5],
    [-68, 5200, "餐饮", "盒马鲜生", "周末采购", "支付宝", 18, 55],
    [-66, 120000, "转账", "张秀兰", "生活费", "手机银行", 10, 30],
    [-63, 19900, "购物", "优衣库旗舰店", "秋冬外套", "支付宝", 14, 8],
    [-60, 8800, "交通", "中国石化", "加油", "微信", 7, 50],
    [-57, 15600, "餐饮", "西贝莜面村", "家庭聚餐", "微信", 12, 40],
    [-54, 320000, "购物", "苏宁易购", "戴森吸尘器", "刷卡", 16, 22],
    [-51, 7400, "餐饮", "美团外卖", "外卖", "美团", 19, 33],
    [-48, 26000, "娱乐", "Steam", "游戏", "微信", 21, 15],
    [-45, 45000, "医疗", "协和医院", "体检", "微信", 8, 20],
    [-42, 13200, "购物", "天猫超市", "日用品", "支付宝", 20, 44],
    [-39, 9100, "餐饮", "海底捞火锅(望京店)", "朋友聚餐AA", "微信", 19, 5],
    [-36, 18800, "交通", "12306", "高铁票 北京-上海", "12306", 9, 41],
    [-33, 26600, "购物", "小米有品", "智能音箱", "微信", 13, 18],
    [-30, 5200, "餐饮", "瑞幸咖啡", "咖啡", "微信", 9, 12],
    [-27, 210000, "教育", "VIPKID", "孩子英语课", "支付宝", 20, 30],
    [-24, 11600, "餐饮", "麦当劳", "午餐", "支付宝", 12, 25],
    [-21, 39800, "娱乐", "大麦网", "演唱会门票", "支付宝", 15, 55],
    [-18, 8600, "餐饮", "盒马鲜生", "生鲜", "支付宝", 18, 20],
    [-15, 52000, "转账", "李娜", "家用", "手机银行", 10, 0],
    [-12, 14400, "购物", "拼多多", "纸巾洗衣液", "微信", 16, 38],
    [-9, 22600, "餐饮", "四季民福烤鸭店", "客户宴请", "刷卡", 19, 48],
    [-7, 5800, "交通", "北京地铁", "交通卡充值", "支付宝", 8, 15],
    [-5, 11200, "餐饮", "星巴克(金融街店)", "早餐", "微信", 8, 40],
    [-3, 29900, "购物", "京东", "机械键盘", "微信", 22, 10],
    [-1, 6400, "餐饮", "肯德基", "晚餐", "支付宝", 18, 55],
  ];
  for (const [off, amt, cat, cp, memo, ch, h, m] of spend) {
    add({ direction: "out", amountCents: amt, category: cat, counterparty: cp, memo, channel: ch ?? "支付宝", occurredAt: day(off, h ?? 12, m ?? 0) });
  }

  // 理财申赎流水
  add({ direction: "out", amountCents: 2000000, category: "理财", counterparty: "招银理财", memo: "申购 日日盈", channel: "手机银行", occurredAt: day(-70, 10, 15) });
  add({ direction: "in", amountCents: 500000, category: "理财", counterparty: "招银理财", memo: "赎回 日日盈", channel: "手机银行", occurredAt: day(-20, 14, 30) });

  // ---------- 异常交易 ----------
  add({
    direction: "out",
    amountCents: 380000,
    category: "购物",
    counterparty: "AMAZON UK * MARKETPLACE",
    memo: "境外线上消费",
    channel: "刷卡",
    isAnomaly: true,
    anomalyReason: "凌晨 2:37 境外大额消费，与常用时段、常用地区不符",
    occurredAt: day(-16, 2, 37),
  });
  add({
    direction: "out",
    amountCents: 64800,
    category: "娱乐",
    counterparty: "某网络游戏平台",
    memo: "游戏充值",
    channel: "微信",
    isAnomaly: true,
    anomalyReason: "单日游戏充值多笔、累计 ¥906，频次与金额远超历史均值",
    occurredAt: day(-11, 23, 58),
  });
  add({
    direction: "out",
    amountCents: 1200000,
    category: "转账",
    counterparty: "*德柱(尾号 9921)",
    memo: "转账",
    channel: "手机银行",
    isAnomaly: true,
    anomalyReason: "向无历史往来账户转账 ¥12,000，金额超过日常转账 90 分位",
    occurredAt: day(-28, 21, 14),
  });
  add({
    direction: "out",
    amountCents: 25800,
    category: "娱乐",
    counterparty: "某网络游戏平台",
    memo: "游戏充值",
    channel: "微信",
    isAnomaly: true,
    anomalyReason: "单日游戏充值多笔、累计 ¥906，频次与金额远超历史均值",
    occurredAt: day(-11, 22, 41),
  });

  await db.insert(transactions).values(txs);

  // ---------- 银行卡 ----------
  await db.insert(cards).values([
    {
      cardType: "debit",
      bankName: "灵犀银行",
      last4: "8886",
      creditLimitCents: null,
      usedCents: 0,
      dailyLimitCents: 5000000,
      status: "normal",
      onlinePayEnabled: true,
    },
    {
      cardType: "credit",
      bankName: "灵犀银行",
      last4: "5210",
      creditLimitCents: 5000000, // 5万额度
      usedCents: 1365000,
      dailyLimitCents: 2000000,
      status: "normal",
      onlinePayEnabled: true,
    },
  ]);

  // ---------- 理财产品 ----------
  await db.insert(wealthProducts).values([
    {
      name: "灵每日日盈",
      type: "cash",
      riskLevel: "R1",
      annualRateBps: 168,
      minAmountCents: 100,
      termDays: 0,
      tags: "灵活申赎,货基增强",
      description: "现金管理类，T+0 快速赎回，适合放零钱。",
    },
    {
      name: "灵每安稳 90 天",
      type: "bond",
      riskLevel: "R2",
      annualRateBps: 245,
      minAmountCents: 100000,
      termDays: 90,
      tags: "纯债,封闭90天",
      description: "中短债策略，波动小，持有 90 天到期可续。",
    },
    {
      name: "灵每丰利一年定开",
      type: "bond",
      riskLevel: "R2",
      annualRateBps: 298,
      minAmountCents: 100000,
      termDays: 365,
      tags: "固收+,定开",
      description: "固收打底 + 少量权益增强，一年定开。",
    },
    {
      name: "灵犀平衡混合 A",
      type: "mixed",
      riskLevel: "R3",
      annualRateBps: 412,
      minAmountCents: 10000,
      termDays: 0,
      tags: "股债平衡,灵活申赎",
      description: "股债 5:5 动态平衡，追求中长期稳健增值。",
    },
    {
      name: "中证 500 指数增强",
      type: "index",
      riskLevel: "R4",
      annualRateBps: 856,
      minAmountCents: 10000,
      termDays: 0,
      tags: "指数增强,高波动",
      description: "跟踪中证 500 并力争超额，波动较大。",
    },
    {
      name: "积存金（黄金）",
      type: "gold",
      riskLevel: "R3",
      annualRateBps: 0,
      minAmountCents: 10000,
      termDays: 0,
      tags: "黄金,抗通胀",
      description: "按克积存实物黄金，价格随金价浮动。",
    },
  ]);

  // ---------- 持仓 ----------
  const products = await db.query.wealthProducts.findMany();
  const prod = (n: string) => products.find((p) => p.name === n)!;
  await db.insert(holdings).values([
    {
      productId: prod("灵每日日盈").id,
      amountCents: 1500000,
      profitCents: 18540,
      purchasedAt: day(-70, 10, 15),
    },
    {
      productId: prod("灵每安稳 90 天").id,
      amountCents: 5000000,
      profitCents: 61230,
      purchasedAt: day(-55, 15, 0),
    },
  ]);

  // ---------- 风险测评（已做过一次：稳健型） ----------
  await db.insert(riskProfiles).values({
    score: 46,
    level: "稳健型",
    answers: JSON.stringify({ horizon: "1-3年", loss: "可接受5%以内回撤", income: "稳定" }),
    createdAt: day(-100, 11, 0),
  });

  // ---------- AA 收款示例 ----------
  await db.insert(aaSplits).values({
    title: "部门聚餐 · 海底捞",
    totalCents: 103200,
    participants: JSON.stringify([
      { name: "王强", phone: "13912345678", amountCents: 25800, paid: true },
      { name: "陈晨", phone: "13798765432", amountCents: 25800, paid: false },
      { name: "刘洋", phone: "13511112222", amountCents: 25800, paid: true },
      { name: "赵倩", phone: "13633334444", amountCents: 25800, paid: false },
    ]),
    status: "collecting",
    createdAt: day(-2, 20, 30),
  });

  // ---------- 定时转账示例 ----------
  await db.insert(scheduledTransfers).values({
    contactId: byName["周敏"].id,
    amountCents: 450000,
    memo: "11月房租",
    executeAt: day(4, 9, 0),
    status: "pending",
    createdAt: day(-1, 21, 0),
  });

  console.log("✅ Seed 完成");
  process.exit(0);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
