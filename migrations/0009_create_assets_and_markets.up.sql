CREATE TABLE IF NOT EXISTS assets (
  id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  symbol text NOT NULL UNIQUE,
  precision int NOT NULL,
  status text NOT NULL DEFAULT 'active',
  CHECK (precision >= 0)
);

CREATE TABLE IF NOT EXISTS markets (
  id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  symbol text NOT NULL UNIQUE,
  base_asset text NOT NULL REFERENCES assets(symbol),
  quote_asset text NOT NULL REFERENCES assets(symbol),
  status text NOT NULL DEFAULT 'active',
  CHECK (base_asset <> quote_asset)
);

CREATE INDEX IF NOT EXISTS idx_markets_status ON markets(status);
