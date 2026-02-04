CREATE TABLE IF NOT EXISTS fee_tiers (
  id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  name text NOT NULL UNIQUE,
  maker_fee_bps int NOT NULL,
  taker_fee_bps int NOT NULL,
  min_volume numeric NOT NULL DEFAULT 0,
  CHECK (maker_fee_bps >= 0),
  CHECK (taker_fee_bps >= 0),
  CHECK (min_volume >= 0)
);

CREATE INDEX IF NOT EXISTS idx_fee_tiers_min_volume ON fee_tiers(min_volume);

INSERT INTO fee_tiers (name, maker_fee_bps, taker_fee_bps, min_volume)
VALUES ('default', 10, 20, 0)
ON CONFLICT (name) DO NOTHING;
