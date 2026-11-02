-- 0001_init: full schema. Amounts are BIGINT minor units (分 / 外币分); rates are
-- basis points. Every per-customer table carries user_id and every lookup must
-- filter on it. Timestamps are UTC DATETIME(3).

-- ============ 身份与安全 ============
CREATE TABLE IF NOT EXISTS users (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  username        VARCHAR(64)  NOT NULL,
  password_hash   VARCHAR(255) NOT NULL,
  display_name    VARCHAR(64)  NOT NULL,
  status          ENUM('active','disabled') NOT NULL DEFAULT 'active',
  failed_attempts INT          NOT NULL DEFAULT 0,
  locked_until    DATETIME(3)  NULL,
  last_login_at   DATETIME(3)  NULL,
  created_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- token itself is never stored: id_hash = hex(sha256(token))
CREATE TABLE IF NOT EXISTS sessions (
  id_hash      CHAR(64)        NOT NULL PRIMARY KEY,
  user_id      BIGINT UNSIGNED NOT NULL,
  created_at   DATETIME(3)     NOT NULL,
  last_seen_at DATETIME(3)     NOT NULL,
  expires_at   DATETIME(3)     NOT NULL,
  ip           VARCHAR(45)     NOT NULL DEFAULT '',
  user_agent   VARCHAR(255)    NOT NULL DEFAULT '',
  revoked_at   DATETIME(3)     NULL,
  KEY idx_sessions_user (user_id),
  CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS audit_logs (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id     BIGINT UNSIGNED NULL,
  action      VARCHAR(64)  NOT NULL,
  target      VARCHAR(128) NOT NULL DEFAULT '',
  params      JSON         NULL,
  result      ENUM('ok','fail') NOT NULL,
  error       VARCHAR(255) NOT NULL DEFAULT '',
  ip          VARCHAR(45)  NOT NULL DEFAULT '',
  user_agent  VARCHAR(255) NOT NULL DEFAULT '',
  request_id  VARCHAR(64)  NOT NULL DEFAULT '',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_audit_user_time (user_id, created_at),
  KEY idx_audit_action_time (action, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS idempotency_keys (
  user_id      BIGINT UNSIGNED NOT NULL,
  idem_key     VARCHAR(64)  NOT NULL,
  path         VARCHAR(128) NOT NULL,
  request_hash CHAR(64)     NOT NULL,
  response     MEDIUMTEXT   NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (user_id, idem_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 账户与流水 ============
CREATE TABLE IF NOT EXISTS accounts (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id       BIGINT UNSIGNED NOT NULL,
  holder_name   VARCHAR(64) NOT NULL,
  balance_cents BIGINT      NOT NULL DEFAULT 0,
  locked_cents  BIGINT      NOT NULL DEFAULT 0,
  updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_accounts_user (user_id),
  CONSTRAINT fk_accounts_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT ck_accounts_balance CHECK (balance_cents >= 0 AND locked_cents >= 0 AND locked_cents <= balance_cents)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS transactions (
  id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id             BIGINT UNSIGNED NOT NULL,
  account_id          BIGINT UNSIGNED NOT NULL,
  direction           ENUM('in','out') NOT NULL,
  amount_cents        BIGINT       NOT NULL,
  balance_after_cents BIGINT       NOT NULL,
  category            VARCHAR(32)  NOT NULL DEFAULT '其他',
  counterparty        VARCHAR(128) NOT NULL,
  memo                VARCHAR(255) NULL,
  channel             VARCHAR(32)  NOT NULL DEFAULT '手机银行',
  status              ENUM('success','pending','failed') NOT NULL DEFAULT 'success',
  is_anomaly          BOOLEAN      NOT NULL DEFAULT FALSE,
  anomaly_reason      VARCHAR(255) NULL,
  biz_type            VARCHAR(32)  NOT NULL DEFAULT '',
  biz_ref             VARCHAR(64)  NOT NULL DEFAULT '',
  request_id          VARCHAR(64)  NOT NULL DEFAULT '',
  occurred_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_tx_user_time (user_id, occurred_at),
  CONSTRAINT fk_tx_account FOREIGN KEY (account_id) REFERENCES accounts(id),
  CONSTRAINT ck_tx_amount CHECK (amount_cents > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS contacts (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id    BIGINT UNSIGNED NOT NULL,
  name       VARCHAR(64)  NOT NULL,
  phone      VARCHAR(20)  NULL,
  bank_name  VARCHAR(64)  NULL,
  card_tail  VARCHAR(8)   NULL,
  relation   VARCHAR(32)  NULL,
  birthday   VARCHAR(5)   NULL,
  note       VARCHAR(255) NULL,
  KEY idx_contacts_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS scheduled_transfers (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id      BIGINT UNSIGNED NOT NULL,
  contact_id   BIGINT UNSIGNED NOT NULL,
  amount_cents BIGINT       NOT NULL,
  memo         VARCHAR(255) NULL,
  execute_at   DATETIME(3)  NOT NULL,
  status       ENUM('pending','executed','cancelled','failed') NOT NULL DEFAULT 'pending',
  fail_reason  VARCHAR(255) NULL,
  tx_id        BIGINT UNSIGNED NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_sched_user (user_id),
  KEY idx_sched_due (status, execute_at),
  CONSTRAINT ck_sched_amount CHECK (amount_cents > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS aa_splits (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id      BIGINT UNSIGNED NOT NULL,
  title        VARCHAR(128) NOT NULL,
  total_cents  BIGINT       NOT NULL,
  participants JSON         NOT NULL,
  status       ENUM('collecting','done','cancelled') NOT NULL DEFAULT 'collecting',
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_aa_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 卡片 ============
CREATE TABLE IF NOT EXISTS cards (
  id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id            BIGINT UNSIGNED NOT NULL,
  card_type          ENUM('debit','credit') NOT NULL,
  bank_name          VARCHAR(64) NOT NULL,
  last4              CHAR(4)     NOT NULL,
  credit_limit_cents BIGINT      NULL,
  used_cents         BIGINT      NOT NULL DEFAULT 0,
  daily_limit_cents  BIGINT      NOT NULL DEFAULT 5000000,
  status             ENUM('normal','frozen','lost','restricted') NOT NULL DEFAULT 'normal',
  online_pay_enabled BOOLEAN     NOT NULL DEFAULT TRUE,
  created_at         DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_cards_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 理财 ============
CREATE TABLE IF NOT EXISTS wealth_products (
  id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name             VARCHAR(128) NOT NULL,
  type             ENUM('cash','bond','mixed','index','gold') NOT NULL,
  risk_level       ENUM('R1','R2','R3','R4','R5') NOT NULL,
  annual_rate_bps  INT          NOT NULL,
  min_amount_cents BIGINT       NOT NULL DEFAULT 100,
  term_days        INT          NOT NULL DEFAULT 0,
  tags             VARCHAR(128) NULL,
  description      VARCHAR(255) NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS holdings (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id      BIGINT UNSIGNED NOT NULL,
  product_id   BIGINT UNSIGNED NOT NULL,
  amount_cents BIGINT      NOT NULL,
  profit_cents BIGINT      NOT NULL DEFAULT 0,
  status       ENUM('holding','redeemed') NOT NULL DEFAULT 'holding',
  purchased_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  redeemed_at  DATETIME(3) NULL,
  KEY idx_holdings_user (user_id, status),
  CONSTRAINT ck_holdings_amount CHECK (amount_cents >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS risk_profiles (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id    BIGINT UNSIGNED NOT NULL,
  score      INT         NOT NULL,
  level      VARCHAR(16) NOT NULL,
  answers    JSON        NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_risk_user (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 跨场景任务 ============
CREATE TABLE IF NOT EXISTS scenarios (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id    BIGINT UNSIGNED NOT NULL,
  type       ENUM('birthday_guard') NOT NULL,
  title      VARCHAR(128) NOT NULL,
  contact_id BIGINT UNSIGNED NOT NULL,
  payload    JSON         NOT NULL,
  status     ENUM('armed','locked','ordered','done','cancelled') NOT NULL DEFAULT 'armed',
  log        JSON         NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_scenarios_user (user_id),
  KEY idx_scenarios_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gift_orders (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id      BIGINT UNSIGNED NOT NULL,
  scenario_id  BIGINT UNSIGNED NOT NULL,
  kind         ENUM('flower','cake') NOT NULL,
  merchant     VARCHAR(64)  NOT NULL,
  item_name    VARCHAR(128) NOT NULL,
  amount_cents BIGINT       NOT NULL,
  deliver_at   DATETIME(3)  NOT NULL,
  status       ENUM('scheduled','placed','delivered','cancelled') NOT NULL DEFAULT 'scheduled',
  tx_id        BIGINT UNSIGNED NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_gift_scenario (scenario_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ Agent ============
CREATE TABLE IF NOT EXISTS agent_messages (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id    BIGINT UNSIGNED NOT NULL,
  role       ENUM('user','assistant') NOT NULL,
  content    TEXT        NOT NULL,
  tools      JSON        NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_agent_msg_user (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Server-side confirmation for high-risk agent actions. token is a 5-minute,
-- single-use, user- and turn-bound nonce (not a credential), kept in clear so
-- the next turn can re-offer it to the model.
CREATE TABLE IF NOT EXISTS agent_confirmations (
  token       VARCHAR(64)     NOT NULL PRIMARY KEY,
  user_id     BIGINT UNSIGNED NOT NULL,
  tool        VARCHAR(64)     NOT NULL,
  params_hash CHAR(64)        NOT NULL,
  params_json JSON            NOT NULL,
  summary     VARCHAR(512)    NOT NULL,
  turn_no     INT             NOT NULL,
  created_at  DATETIME(3)     NOT NULL,
  expires_at  DATETIME(3)     NOT NULL,
  used_at     DATETIME(3)     NULL,
  KEY idx_confirm_user (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 外汇 ============
CREATE TABLE IF NOT EXISTS fx_wallets (
  id                    BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id               BIGINT UNSIGNED NOT NULL,
  currency              CHAR(3)     NOT NULL,
  balance_foreign_cents BIGINT      NOT NULL DEFAULT 0,
  locked_foreign_cents  BIGINT      NOT NULL DEFAULT 0,
  updated_at            DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_wallet_user_ccy (user_id, currency),
  CONSTRAINT ck_wallet_balance CHECK (balance_foreign_cents >= 0 AND locked_foreign_cents >= 0 AND locked_foreign_cents <= balance_foreign_cents)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS fx_rates (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  pair          VARCHAR(10) NOT NULL,
  currency      CHAR(3)     NOT NULL,
  buy_rate_bps  INT         NOT NULL,
  sell_rate_bps INT         NOT NULL,
  updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_rates_ccy (currency)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS fx_contracts (
  id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id              BIGINT UNSIGNED NOT NULL,
  type                 ENUM('forward','option','swap') NOT NULL,
  currency             CHAR(3)     NOT NULL,
  foreign_amount_cents BIGINT      NOT NULL,
  locked_rate_bps      INT         NOT NULL,
  direction            ENUM('sell_foreign','buy_foreign') NOT NULL DEFAULT 'sell_foreign',
  margin_cents         BIGINT      NOT NULL DEFAULT 0,
  premium_cents        BIGINT      NOT NULL DEFAULT 0,
  expires_at           DATETIME(3) NOT NULL,
  status               ENUM('active','settled','cancelled') NOT NULL DEFAULT 'active',
  created_at           DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_fxc_user (user_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 供应链金融 ============
CREATE TABLE IF NOT EXISTS scf_products (
  id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  code             VARCHAR(32)  NOT NULL,
  name             VARCHAR(64)  NOT NULL,
  rate_bps         INT          NOT NULL,
  max_amount_cents BIGINT       NOT NULL,
  term_days        INT          NOT NULL,
  tags             VARCHAR(128) NULL,
  description      VARCHAR(255) NULL,
  UNIQUE KEY uk_scf_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS scf_loans (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id        BIGINT UNSIGNED NOT NULL,
  product_id     BIGINT UNSIGNED NOT NULL,
  amount_cents   BIGINT       NOT NULL,
  rate_bps       INT          NOT NULL,
  purpose        VARCHAR(255) NULL,
  related_ref    VARCHAR(64)  NULL,
  status         ENUM('disbursed','repaid','overdue') NOT NULL DEFAULT 'disbursed',
  interest_cents BIGINT       NOT NULL DEFAULT 0,
  applied_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  due_at         DATETIME(3)  NOT NULL,
  repaid_at      DATETIME(3)  NULL,
  KEY idx_loans_user (user_id, status),
  CONSTRAINT ck_loan_amount CHECK (amount_cents > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============ 外贸一体化 ============
CREATE TABLE IF NOT EXISTS shipments (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id       BIGINT UNSIGNED NOT NULL,
  tracking_no   VARCHAR(32)  NOT NULL,
  carrier       VARCHAR(64)  NOT NULL,
  platform      VARCHAR(32)  NOT NULL,
  destination   VARCHAR(64)  NOT NULL,
  goods_desc    VARCHAR(128) NULL,
  weight_kg     VARCHAR(16)  NULL,
  freight_cents BIGINT       NOT NULL DEFAULT 0,
  status        ENUM('label_created','in_transit','customs_clearance','delivered','exception') NOT NULL DEFAULT 'label_created',
  milestones    JSON         NOT NULL,
  eta_at        DATETIME(3)  NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_ship_user (user_id),
  KEY idx_ship_tracking (user_id, tracking_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS customs_declarations (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id         BIGINT UNSIGNED NOT NULL,
  decl_no         VARCHAR(32)  NOT NULL,
  port            VARCHAR(64)  NOT NULL,
  goods_desc      VARCHAR(128) NULL,
  value_usd_cents BIGINT       NOT NULL,
  status          ENUM('declared','inspecting','released','closed') NOT NULL DEFAULT 'declared',
  declared_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  released_at     DATETIME(3)  NULL,
  KEY idx_customs_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS tax_refunds (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id      BIGINT UNSIGNED NOT NULL,
  period       VARCHAR(8)  NOT NULL,
  amount_cents BIGINT      NOT NULL,
  status       ENUM('filing','reviewing','approved','paid') NOT NULL DEFAULT 'filing',
  progress     INT         NOT NULL DEFAULT 0,
  decl_no      VARCHAR(32) NULL,
  updated_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_refunds_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
