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
  fxWallets,
  fxRates,
  fxContracts,
  scfProducts,
  scfLoans,
  shipments,
  customsDeclarations,
  taxRefunds,
} from "./schema";

const NOW = new Date("2026-11-02T09:30:00+08:00");
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
  await db.delete(taxRefunds);
  await db.delete(customsDeclarations);
  await db.delete(shipments);
  await db.delete(scfLoans);
  await db.delete(scfProducts);
  await db.delete(fxContracts);
  await db.delete(fxRates);
  await db.delete(fxWallets);
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

  // ============ 主账户：跨境电商卖家 ============
  // 锁定部分 = 远期锁汇保证金 ¥17,712.50
  await db.insert(accounts).values({
    holderName: "陈航 · 跨洋科技",
    balanceCents: 48652040,
    lockedCents: 1771250,
  });

  // ============ 多币种钱包 ============
  await db.insert(fxWallets).values([
    { currency: "USD", balanceForeignCents: 6230050 }, // $62,300.50
    { currency: "EUR", balanceForeignCents: 1852000 }, // €18,520.00
  ]);

  // ============ 外汇牌价（1 外币 = rateBps/10000 CNY） ============
  await db.insert(fxRates).values([
    { pair: "USDCNY", currency: "USD", buyRateBps: 70850, sellRateBps: 71130 },
    { pair: "EURCNY", currency: "EUR", buyRateBps: 77200, sellRateBps: 77560 },
    { pair: "GBPCNY", currency: "GBP", buyRateBps: 90800, sellRateBps: 91250 },
    { pair: "HKDCNY", currency: "HKD", buyRateBps: 9108, sellRateBps: 9142 },
    { pair: "JPYCNY", currency: "JPY", buyRateBps: 468, sellRateBps: 472 },
  ]);

  // ============ 外汇衍生品合约 ============
  await db.insert(fxContracts).values([
    {
      type: "forward",
      currency: "USD",
      foreignAmountCents: 5000000, // $50,000
      lockedRateBps: 70850,
      direction: "sell_foreign",
      marginCents: 1771250, // 50000 × 7.0850 × 5%
      expiresAt: day(30),
      status: "active",
      createdAt: day(-12),
    },
    {
      type: "forward",
      currency: "USD",
      foreignAmountCents: 3000000,
      lockedRateBps: 70960,
      direction: "sell_foreign",
      marginCents: 0,
      expiresAt: day(-40),
      status: "settled",
      createdAt: day(-70),
    },
  ]);

  // ============ 供应链金融产品 ============
  await db.insert(scfProducts).values([
    {
      code: "ORDER_LOAN",
      name: "跨境订单贷",
      rateBps: 680,
      maxAmountCents: 50000000, // ¥500,000
      termDays: 90,
      tags: "凭平台订单,随借随还,线上秒批",
      description: "凭亚马逊/速卖通已发货未回款订单融资，按订单金额 80% 授信，回款自动归还。",
    },
    {
      code: "WAREHOUSE",
      name: "海外仓仓单质押贷",
      rateBps: 750,
      maxAmountCents: 100000000, // ¥1,000,000
      termDays: 180,
      tags: "库存质押,海外仓,大额",
      description: "以海外仓在库货物仓单质押，按货值 60% 放款，适合备货旺季资金周转。",
    },
    {
      code: "TAX_REBATE",
      name: "出口退税贷",
      rateBps: 560,
      maxAmountCents: 30000000, // ¥300,000
      termDays: 60,
      tags: "退税过桥,低息,快到账",
      description: "以已申报未到账的出口退税款为质押，提前拿到退税资金，年化利率更低。",
    },
    {
      code: "CREDIT_INS",
      name: "出口信保融资",
      rateBps: 620,
      maxAmountCents: 80000000, // ¥800,000
      termDays: 120,
      tags: "中信保保单,赊销订单",
      description: "凭中信保短期出口信用保险保单，为赊销（OA）订单提供应收账款融资。",
    },
  ]);

  // ============ 在途/已还融资 ============
  const products = await db.query.scfProducts.findMany();
  const orderLoanId = products.find((p) => p.code === "ORDER_LOAN")!.id;
  const rebateLoanId = products.find((p) => p.code === "TAX_REBATE")!.id;
  await db.insert(scfLoans).values([
    {
      productId: orderLoanId,
      amountCents: 12000000, // ¥120,000
      rateBps: 680,
      purpose: "黑五备货采购",
      relatedRef: "AMZ-ORDER-88213",
      status: "disbursed",
      appliedAt: day(-25),
      dueAt: day(65),
    },
    {
      productId: rebateLoanId,
      amountCents: 8000000,
      rateBps: 560,
      purpose: "8 月退税过桥",
      relatedRef: "2025-08",
      status: "repaid",
      appliedAt: day(-95),
      dueAt: day(-35),
      repaidAt: day(-38),
    },
  ]);

  // ============ 物流运单 ============
  await db.insert(shipments).values([
    {
      trackingNo: "YT88362012",
      carrier: "云途物流",
      platform: "Amazon 北美站",
      destination: "美国·洛杉矶 ONT8 仓",
      goodsDesc: "蓝牙耳机 × 1,200 件",
      weightKg: "486kg",
      freightCents: 2136000,
      status: "in_transit",
      milestones: JSON.stringify([
        { time: "10-24 14:20", text: "深圳揽收，货物已入云途仓", location: "深圳" },
        { time: "10-26 09:05", text: "出口报关放行（蛇口港）", location: "深圳蛇口" },
        { time: "10-28 22:40", text: "装船离港，船名 COSCO SHIPPING UNIVERSE", location: "太平洋航线" },
        { time: "11-01 06:12", text: "抵达洛杉矶港，等待卸船", location: "洛杉矶" },
      ]),
      etaAt: day(9),
      createdAt: day(-9),
    },
    {
      trackingNo: "4PX77120908",
      carrier: "递四方",
      platform: "速卖通",
      destination: "德国·法兰克福",
      goodsDesc: "手机壳套装 × 860 件",
      weightKg: "152kg",
      freightCents: 684000,
      status: "customs_clearance",
      milestones: JSON.stringify([
        { time: "10-27 11:30", text: "东莞仓揽收", location: "东莞" },
        { time: "10-29 16:45", text: "香港机场起飞，航班 CX880", location: "香港" },
        { time: "10-31 08:20", text: "抵达法兰克福，转海关查验", location: "法兰克福" },
        { time: "11-02 07:55", text: "海关查验中（抽中人工查验）", location: "法兰克福海关" },
      ]),
      etaAt: day(6),
      createdAt: day(-6),
    },
    {
      trackingNo: "SFIN33012871",
      carrier: "顺丰国际",
      platform: "Temu",
      destination: "日本·东京",
      goodsDesc: "智能手环 × 300 件",
      weightKg: "45kg",
      freightCents: 189000,
      status: "delivered",
      milestones: JSON.stringify([
        { time: "10-25 10:10", text: "深圳揽收", location: "深圳" },
        { time: "10-26 19:30", text: "宝安机场起飞", location: "深圳" },
        { time: "10-27 08:40", text: "东京羽田清关放行", location: "东京" },
        { time: "10-28 15:22", text: "签收完成（Temu 集货仓）", location: "东京" },
      ]),
      etaAt: day(-4),
      createdAt: day(-8),
    },
    {
      trackingNo: "YW55690234",
      carrier: "燕文物流",
      platform: "独立站",
      destination: "英国·伦敦",
      goodsDesc: "宠物用品 × 540 件",
      weightKg: "310kg",
      freightCents: 1176000,
      status: "label_created",
      milestones: JSON.stringify([{ time: "11-02 09:00", text: "运单已创建，等待揽收", location: "深圳" }]),
      etaAt: day(14),
      createdAt: day(0),
    },
  ]);

  // ============ 海关报关单 ============
  await db.insert(customsDeclarations).values([
    {
      declNo: "5304202611882033",
      port: "深圳蛇口海关",
      goodsDesc: "蓝牙耳机（出口退税 13%）",
      valueUsdCents: 8640000, // $86,400
      status: "released",
      declaredAt: day(-8),
      releasedAt: day(-7),
    },
    {
      declNo: "5304202611885617",
      port: "宁波舟山港海关",
      goodsDesc: "手机壳套装",
      valueUsdCents: 2380000,
      status: "inspecting",
      declaredAt: day(-2),
    },
    {
      declNo: "5304202611889901",
      port: "深圳前海海关",
      goodsDesc: "宠物用品",
      valueUsdCents: 4120000,
      status: "declared",
      declaredAt: day(0),
    },
  ]);

  // ============ 出口退税 ============
  await db.insert(taxRefunds).values([
    { period: "2025-08", amountCents: 4238000, status: "paid", progress: 100, declNo: "5304202611774521", updatedAt: day(-38) },
    { period: "2025-09", amountCents: 3815000, status: "approved", progress: 85, declNo: "5304202611778903", updatedAt: day(-6) },
    { period: "2025-10", amountCents: 4572000, status: "reviewing", progress: 55, declNo: "5304202611882033", updatedAt: day(-3) },
  ]);

  // ============ 联系人 ============
  const insertedContacts = await db
    .insert(contacts)
    .values([
      { name: "林悦", phone: "138****6621", bankName: "招商银行", cardTail: "8842", relation: "爱人", birthday: "11-18", note: "喜欢向日葵和草莓蛋糕" },
      { name: "王强", phone: "137****3309", bankName: "灵犀银行", cardTail: "2201", relation: "合伙人", birthday: "03-24" },
      { name: "李厂长", phone: "136****8874", bankName: "中国银行", cardTail: "5566", relation: "供应商", note: "深圳华强北电子厂" },
      { name: "张经理", phone: "135****2210", bankName: "建设银行", cardTail: "9012", relation: "物流商", note: "云途物流客户经理" },
      { name: "James Miller", phone: "+1 310****882", bankName: "Bank of America", cardTail: "4408", relation: "海外仓", note: "洛杉矶 ONT8 仓运营" },
    ])
    .$returningId();
  const cid = Object.fromEntries(
    ["林悦", "王强", "李厂长", "张经理", "James Miller"].map((n, i) => [n, insertedContacts[i].id]),
  );

  // ============ 交易流水（近 3 个月，跨境电商经营 + 日常） ============
  type Tx = {
    direction: "in" | "out";
    amountCents: number;
    category: string;
    counterparty: string;
    memo?: string;
    channel?: string;
    at: Date;
    anomaly?: string;
  };
  const txs: Tx[] = [
    // —— 电商回款（半月一次） ——
    { direction: "in", amountCents: 12866000, category: "电商回款", counterparty: "Amazon.com Services", memo: "北美站 10 月下半月回款", channel: "跨境收款", at: day(-13, 10) },
    { direction: "in", amountCents: 9642000, category: "电商回款", counterparty: "Amazon.com Services", memo: "北美站 10 月上半月回款", channel: "跨境收款", at: day(-28, 10) },
    { direction: "in", amountCents: 11328000, category: "电商回款", counterparty: "Amazon.com Services", memo: "北美站 9 月下半月回款", channel: "跨境收款", at: day(-43, 10) },
    { direction: "in", amountCents: 8895000, category: "电商回款", counterparty: "Amazon.com Services", memo: "北美站 9 月上半月回款", channel: "跨境收款", at: day(-58, 10) },
    { direction: "in", amountCents: 2310000, category: "电商回款", counterparty: "速卖通 AliExpress", memo: "欧洲站回款", channel: "跨境收款", at: day(-17, 11) },
    { direction: "in", amountCents: 1874000, category: "电商回款", counterparty: "Payoneer 派安盈", memo: "美元余额提现结汇", channel: "跨境收款", at: day(-6, 14) },
    { direction: "in", amountCents: 4238000, category: "退税", counterparty: "国家税务总局深圳市税务局", memo: "2025-08 出口退税到账", channel: "国库", at: day(-38, 9) },
    // —— 经营支出 ——
    { direction: "out", amountCents: 2136000, category: "物流", counterparty: "云途物流", memo: "YT88362012 运费", at: day(-9, 15) },
    { direction: "out", amountCents: 684000, category: "物流", counterparty: "递四方速递", memo: "4PX77120908 运费", at: day(-6, 16) },
    { direction: "out", amountCents: 189000, category: "物流", counterparty: "顺丰国际", memo: "SFIN33012871 运费", at: day(-8, 11) },
    { direction: "out", amountCents: 960000, category: "物流", counterparty: "云途物流", memo: "10 月上旬批次运费", at: day(-24, 15) },
    { direction: "out", amountCents: 35600000, category: "采购", counterparty: "深圳华强北电子厂（李厂长）", memo: "蓝牙耳机黑五备货", at: day(-26, 14) },
    { direction: "out", amountCents: 18200000, category: "采购", counterparty: "深圳华强北电子厂（李厂长）", memo: "手机壳原料款", at: day(-52, 14) },
    { direction: "out", amountCents: 9800000, category: "采购", counterparty: "东莞宠物用品厂", memo: "宠物用品 540 件", at: day(-3, 10) },
    { direction: "out", amountCents: 4260000, category: "推广", counterparty: "Amazon Advertising", memo: "10 月 PPC 广告费", at: day(-12, 9) },
    { direction: "out", amountCents: 3150000, category: "推广", counterparty: "Amazon Advertising", memo: "9 月 PPC 广告费", at: day(-42, 9) },
    { direction: "out", amountCents: 1880000, category: "关税", counterparty: "洛杉矶海关（代缴）", memo: "蓝牙耳机关税", at: day(-1, 8) },
    { direction: "out", amountCents: 2400000, category: "仓储", counterparty: "James Miller（ONT8 海外仓）", memo: "10 月仓租+操作费", at: day(-10, 13) },
    { direction: "out", amountCents: 2400000, category: "仓储", counterparty: "James Miller（ONT8 海外仓）", memo: "9 月仓租+操作费", at: day(-40, 13) },
    // —— 定时/固定 ——
    { direction: "out", amountCents: 1200000, category: "转账", counterparty: "王强", memo: "10 月工资", at: day(-17, 10) },
    { direction: "out", amountCents: 1200000, category: "转账", counterparty: "王强", memo: "9 月工资", at: day(-48, 10) },
    { direction: "out", amountCents: 1200000, category: "转账", counterparty: "王强", memo: "8 月工资", at: day(-79, 10) },
    // —— 日常消费 ——
    { direction: "out", amountCents: 26800, category: "餐饮", counterparty: "海底捞（海上世界店）", memo: "团队聚餐", at: day(-5, 19) },
    { direction: "out", amountCents: 8900, category: "餐饮", counterparty: "星巴克（科技园店）", at: day(-2, 9) },
    { direction: "out", amountCents: 15600, category: "餐饮", counterparty: "八合里牛肉火锅", at: day(-20, 19) },
    { direction: "out", amountCents: 129900, category: "购物", counterparty: "京东", memo: "iPhone 配件", at: day(-7, 21) },
    { direction: "out", amountCents: 34900, category: "购物", counterparty: "山姆会员店", at: day(-15, 16) },
    { direction: "out", amountCents: 4500, category: "交通", counterparty: "滴滴出行", at: day(-4, 8) },
    { direction: "out", amountCents: 18600, category: "交通", counterparty: "中石化加油站", at: day(-18, 18) },
    { direction: "out", amountCents: 56000, category: "住房", counterparty: "自如公寓", memo: "11 月房租", at: day(-1, 10) },
    { direction: "in", amountCents: 520000, category: "转账", counterparty: "林悦", memo: "家用补贴", at: day(-9, 20) },
    // —— 异常交易（4 笔） ——
    { direction: "out", amountCents: 520000, category: "其他", counterparty: "深夜便利店扫码", memo: "凌晨大额支出", channel: "扫码支付", at: day(-3, 3, 42), anomaly: "凌晨 0-5 点大额交易" },
    { direction: "out", amountCents: 2800000, category: "转账", counterparty: "某某商贸有限公司", memo: "货款？", at: day(-2, 22), anomaly: "陌生对手方大额转账" },
    { direction: "out", amountCents: 128000, category: "娱乐", counterparty: "星光 KTV", at: day(-4, 21), anomaly: "当日娱乐类高频大额累计" },
    { direction: "out", amountCents: 89900, category: "娱乐", counterparty: "环球影城旗舰店", at: day(-4, 22), anomaly: "当日娱乐类高频大额累计" },
    { direction: "out", amountCents: 128600, category: "娱乐", counterparty: "夜宴酒吧", at: day(-4, 23), anomaly: "当日娱乐类高频大额累计" },
  ];
  await db.insert(transactions).values(
    txs.map((t) => ({
      direction: t.direction,
      amountCents: t.amountCents,
      category: t.category,
      counterparty: t.counterparty,
      memo: t.memo,
      channel: t.channel ?? "手机银行",
      occurredAt: t.at,
      isAnomaly: !!t.anomaly,
      anomalyReason: t.anomaly,
    })),
  );

  // ============ 定时转账 ============
  await db.insert(scheduledTransfers).values([
    { contactId: cid["王强"], amountCents: 1200000, memo: "11 月工资", executeAt: day(13, 10), status: "pending" },
  ]);

  // ============ AA 收款 ============
  await db.insert(aaSplits).values([
    {
      title: "部门聚餐 · 海底捞",
      totalCents: 26800,
      participants: JSON.stringify([
        { name: "王强", amountCents: 6700, paid: true },
        { name: "陈晨", amountCents: 6700, paid: true },
        { name: "刘洋", amountCents: 6700, paid: false },
        { name: "赵敏", amountCents: 6700, paid: false },
      ]),
      status: "collecting",
      createdAt: day(-5, 21),
    },
  ]);

  // ============ 银行卡 ============
  await db.insert(cards).values([
    { cardType: "debit", bankName: "灵犀银行", last4: "6688", dailyLimitCents: 5000000, status: "normal", onlinePayEnabled: true },
    {
      cardType: "credit",
      bankName: "灵犀银行",
      last4: "8899",
      creditLimitCents: 10000000,
      usedCents: 1258000,
      dailyLimitCents: 3000000,
      status: "normal",
      onlinePayEnabled: true,
    },
  ]);

  // ============ 理财产品 ============
  const wp = await db
    .insert(wealthProducts)
    .values([
      { name: "灵犀日日盈", type: "cash", riskLevel: "R1", annualRateBps: 185, minAmountCents: 100, termDays: 0, tags: "灵活申赎,货基增强", description: "跨境收款闲置资金好去处，T+0 赎回实时到账。" },
      { name: "稳利 180 天固收", type: "bond", riskLevel: "R2", annualRateBps: 268, minAmountCents: 100000, termDays: 180, tags: "纯债,持有到期", description: "主投利率债与高等级信用债，适合账期闲置资金。" },
      { name: "汇金双债增强", type: "bond", riskLevel: "R2", annualRateBps: 295, minAmountCents: 100000, termDays: 365, tags: "固收+,转债增强", description: "纯债打底 + 可转债增强，追求稳健超额。" },
      { name: "均衡配置混合 A", type: "mixed", riskLevel: "R3", annualRateBps: 420, minAmountCents: 100000, termDays: 0, tags: "股债平衡", description: "股债 4:6 动态平衡，波动适中。" },
      { name: "纳斯达克 100 指数 QDII", type: "index", riskLevel: "R4", annualRateBps: 0, minAmountCents: 10000, termDays: 0, tags: "美股,科技,美元资产", description: "跟踪纳斯达克 100，美元资产天然对冲汇率敞口。" },
      { name: "积存金", type: "gold", riskLevel: "R3", annualRateBps: 0, minAmountCents: 100, termDays: 0, tags: "黄金,抗通胀", description: "1 元起投实物黄金积存，可提金可赎回。" },
    ])
    .$returningId();

  // ============ 理财持仓 ============
  await db.insert(holdings).values([
    { productId: wp[0].id, amountCents: 20000000, profitCents: 182300, purchasedAt: day(-60) },
    { productId: wp[1].id, amountCents: 30000000, profitCents: 402500, purchasedAt: day(-80) },
  ]);

  // ============ 风险测评 ============
  await db.insert(riskProfiles).values([
    {
      score: 46,
      level: "稳健型",
      answers: JSON.stringify({
        "您的计划投资期限是？": "1-3 年",
        "您能接受的最大本金回撤是？": "5% 以内",
        "您的收入稳定性如何？": "基本稳定",
        "您的投资经验？": "买过债基/理财",
      }),
      createdAt: day(-70),
    },
  ]);

  // ============ 生日守护（armed，生日月 11 月将自动锁款） ============
  await db.insert(scenarios).values([
    {
      type: "birthday_guard",
      title: "林悦生日守护",
      contactId: cid["林悦"],
      payload: JSON.stringify({
        birthday: "11-18",
        lockCents: 100000,
        orderLeadDays: 2,
        gifts: [
          { kind: "flower", budgetCents: 26800, merchant: "花加", itemName: "向日葵花束" },
          { kind: "cake", budgetCents: 19800, merchant: "诺心", itemName: "6 寸草莓蛋糕" },
        ],
      }),
      status: "armed",
      log: JSON.stringify([{ at: day(-2, 20).toISOString(), text: "守护任务已创建，进入生日当月将自动锁定 ¥1,000 专款" }]),
      createdAt: day(-2, 20),
    },
  ]);

  console.log("✅ Seed 完成：跨境电商主体（陈航 · 跨洋科技）+ 多币种 + 供应链金融 + 外汇衍生品 + 外贸一体化数据已就绪");
  process.exit(0);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
