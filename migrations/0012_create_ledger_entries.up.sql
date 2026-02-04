CREATE TABLE IF NOT EXISTS ledger_entries (
  id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  ledger_account_id uuid NOT NULL REFERENCES ledger_accounts(id) ON DELETE CASCADE,
  entry_type text NOT NULL,
  amount numeric NOT NULL,
  reference_type text NOT NULL,
  reference_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (amount > 0),
  CHECK (entry_type IN ('debit', 'credit'))
);

COMMENT ON TABLE ledger_entries IS 'Immutable ledger entries for double-entry accounting';

CREATE INDEX IF NOT EXISTS idx_ledger_entries_account_created_at ON ledger_entries(ledger_account_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_reference ON ledger_entries(reference_type, reference_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_entries_reference_account ON ledger_entries(reference_type, reference_id, ledger_account_id);
