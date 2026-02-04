# Postgres Schema (Core)

## Users
```
users(
  id uuid pk,
  email text unique,
  password_hash text,
  status text,
  kyc_level text,
  mfa_enabled boolean,
  mfa_secret text,
  created_at timestamptz,
  updated_at timestamptz
)
```

## Accounts
```
accounts(
  id uuid pk,
  user_id uuid fk users,
  type text, -- spot, fee, treasury
  created_at timestamptz
)
```

## Assets
```
assets(
  id uuid pk,
  symbol text unique,
  precision int,
  status text
)
```

## Markets
```
markets(
  id uuid pk,
  symbol text unique, -- BTC-USD
  base_asset text,
  quote_asset text,
  status text
)
```

## Orders
```
orders(
  id uuid pk,
  client_order_id text,
  account_id uuid fk accounts,
  symbol text,
  side text,
  type text,
  price numeric,
  quantity numeric,
  filled_quantity numeric,
  status text,
  time_in_force text,
  created_at timestamptz,
  updated_at timestamptz
)
```

## Trades
```
trades(
  id uuid pk,
  symbol text,
  maker_order_id uuid,
  taker_order_id uuid,
  price numeric,
  quantity numeric,
  executed_at timestamptz
)
```

## Ledger Accounts
```
ledger_accounts(
  id uuid pk,
  account_id uuid fk accounts,
  asset text,
  balance_available numeric,
  balance_locked numeric,
  updated_at timestamptz
)
```

## Ledger Entries (Immutable)
```
ledger_entries(
  id uuid pk,
  ledger_account_id uuid fk ledger_accounts,
  entry_type text, -- debit or credit
  amount numeric,
  reference_type text,
  reference_id uuid,
  created_at timestamptz
)
```

## Fees
```
fee_tiers(
  id uuid pk,
  name text,
  maker_fee_bps int,
  taker_fee_bps int,
  min_volume numeric
)
```

## Withdrawals
```
withdrawals(
  id uuid pk,
  account_id uuid,
  asset text,
  address text,
  amount numeric,
  status text,
  created_at timestamptz,
  updated_at timestamptz
)
```

## Deposits
```
deposits(
  id uuid pk,
  account_id uuid,
  asset text,
  amount numeric,
  txid text,
  status text,
  created_at timestamptz,
  updated_at timestamptz
)
```

## Audit Logs
```
audit_logs(
  id uuid pk,
  actor_id uuid,
  actor_type text,
  action text,
  entity_type text,
  entity_id uuid,
  created_at timestamptz,
  metadata jsonb
)
```

## API Keys
```
api_keys(
  id uuid pk,
  user_id uuid fk users,
  prefix text,
  key_hash text,
  scopes text[],
  ip_whitelist jsonb,
  last_used_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz,
  updated_at timestamptz
)
```

## Refresh Tokens
```
refresh_tokens(
  id uuid pk,
  user_id uuid fk users,
  token_hash text,
  expires_at timestamptz,
  revoked_at timestamptz,
  replaced_by uuid,
  created_at timestamptz,
  created_ip text,
  user_agent text
)
```
