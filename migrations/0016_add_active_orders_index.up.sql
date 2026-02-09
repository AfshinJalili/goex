CREATE INDEX IF NOT EXISTS idx_orders_active_status
ON orders (status)
WHERE status IN ('pending', 'open');
