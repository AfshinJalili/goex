CREATE TABLE IF NOT EXISTS ledger_accounts (
  id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  asset text NOT NULL,
  balance_available numeric NOT NULL DEFAULT 0,
  balance_locked numeric NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_accounts_account_asset ON ledger_accounts(account_id, asset);
