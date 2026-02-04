CREATE TABLE IF NOT EXISTS orders (
  id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  client_order_id text NOT NULL,
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  symbol text NOT NULL,
  side text NOT NULL,
  type text NOT NULL,
  price numeric,
  quantity numeric NOT NULL,
  filled_quantity numeric NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'pending',
  time_in_force text NOT NULL DEFAULT 'GTC',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (quantity > 0),
  CHECK (filled_quantity >= 0),
  CHECK (filled_quantity <= quantity),
  CHECK (price IS NULL OR price >= 0),
  CHECK (side IN ('buy','sell')),
  CHECK (type IN ('market','limit')),
  CHECK (status IN ('pending','open','filled','cancelled','rejected','expired')),
  CHECK (time_in_force IN ('GTC','IOC','FOK')),
  CHECK (
    (type = 'market' AND price IS NULL) OR
    (type = 'limit' AND price IS NOT NULL)
  )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_client_order_account ON orders(client_order_id, account_id);
CREATE INDEX IF NOT EXISTS idx_orders_account_status ON orders(account_id, status);
CREATE INDEX IF NOT EXISTS idx_orders_symbol_status ON orders(symbol, status);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at);
