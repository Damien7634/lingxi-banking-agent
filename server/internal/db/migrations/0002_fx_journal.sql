-- 0002: journal for foreign-currency wallet movements (FX settlement), so every
-- wallet balance change is traceable just like the CNY transactions table.
CREATE TABLE IF NOT EXISTS fx_wallet_journal (
  id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id             BIGINT UNSIGNED NOT NULL,
  wallet_id           BIGINT UNSIGNED NOT NULL,
  currency            CHAR(3)      NOT NULL,
  direction           ENUM('in','out') NOT NULL,
  amount_cents        BIGINT       NOT NULL,
  balance_after_cents BIGINT       NOT NULL,
  biz_type            VARCHAR(32)  NOT NULL,
  biz_ref             VARCHAR(64)  NOT NULL DEFAULT '',
  memo                VARCHAR(255) NOT NULL DEFAULT '',
  request_id          VARCHAR(64)  NOT NULL DEFAULT '',
  occurred_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_fxj_user_time (user_id, occurred_at),
  CONSTRAINT ck_fxj_amount CHECK (amount_cents > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
