CREATE TABLE IF NOT EXISTS balance_reservations (
  id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  order_id uuid NOT NULL UNIQUE,
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  asset text NOT NULL,
  amount numeric NOT NULL,
  consumed_amount numeric NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (amount > 0),
  CHECK (consumed_amount >= 0),
  CHECK (consumed_amount <= amount),
  CHECK (status IN ('active','released','consumed'))
);

CREATE INDEX IF NOT EXISTS idx_balance_reservations_account_asset ON balance_reservations(account_id, asset);
CREATE INDEX IF NOT EXISTS idx_balance_reservations_status ON balance_reservations(status);
