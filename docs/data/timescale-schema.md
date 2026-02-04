# TimescaleDB Schema (Time-Series)

## Ticks
```
market_ticks(
  time timestamptz,
  symbol text,
  price numeric,
  quantity numeric,
  side text
)
```
Hypertable on `time` partitioned by `symbol`.

## Candles
```
market_candles(
  time timestamptz,
  symbol text,
  interval text,
  open numeric,
  high numeric,
  low numeric,
  close numeric,
  volume numeric
)
```
Hypertable on `time`.

## Market Metrics
```
market_metrics(
  time timestamptz,
  symbol text,
  trades_per_sec numeric,
  order_book_depth numeric,
  spread numeric
)
```
Hypertable on `time`.
