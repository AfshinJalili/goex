CREATE TABLE IF NOT EXISTS trades (
  id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  symbol text NOT NULL,
  maker_order_id uuid NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  taker_order_id uuid NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  price numeric NOT NULL,
  quantity numeric NOT NULL,
  executed_at timestamptz NOT NULL DEFAULT now(),
  CHECK (price > 0),
  CHECK (quantity > 0),
  CHECK (maker_order_id <> taker_order_id)
);

CREATE INDEX IF NOT EXISTS idx_trades_symbol_executed_at ON trades(symbol, executed_at);
CREATE INDEX IF NOT EXISTS idx_trades_maker_order_id ON trades(maker_order_id);
CREATE INDEX IF NOT EXISTS idx_trades_taker_order_id ON trades(taker_order_id);
