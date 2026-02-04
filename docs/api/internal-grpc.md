# Internal gRPC APIs

This document describes internal service contracts. These are not exposed publicly.

## Order Ingest Service
```
service OrderIngest {
  rpc SubmitOrder(SubmitOrderRequest) returns (SubmitOrderResponse);
  rpc CancelOrder(CancelOrderRequest) returns (CancelOrderResponse);
  rpc GetOrder(GetOrderRequest) returns (Order);
}
```

Key fields:
- `client_order_id` (idempotency key)
- `symbol`, `side`, `type`, `price`, `quantity`
- `time_in_force`

## Matching Engine
```
service MatchingEngine {
  rpc LoadSnapshot(LoadSnapshotRequest) returns (LoadSnapshotResponse);
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
}
```
Matching consumes Kafka topics; gRPC used for control plane and snapshots.

## Ledger Service
```
service Ledger {
  rpc ApplySettlement(ApplySettlementRequest) returns (ApplySettlementResponse);
  rpc GetBalance(GetBalanceRequest) returns (GetBalanceResponse);
}
```

## Wallet Service Adapter
```
service Wallet {
  rpc CreateWithdrawal(CreateWithdrawalRequest) returns (CreateWithdrawalResponse);
  rpc GetWithdrawal(GetWithdrawalRequest) returns (Withdrawal);
}
```

## Risk Service
```
service Risk {
  rpc PreTradeCheck(PreTradeCheckRequest) returns (PreTradeCheckResponse);
}
```

## Compliance Service
```
service Compliance {
  rpc EvaluateTransaction(EvaluateTransactionRequest) returns (EvaluateTransactionResponse);
  rpc ReportSuspiciousActivity(ReportSuspiciousActivityRequest) returns (ReportResponse);
}
```

## Common Types
```
message Money { string asset = 1; string amount = 2; }
message Order { string id = 1; string symbol = 2; string status = 3; }
message Balance { string account_id = 1; string asset = 2; string available = 3; string locked = 4; }
```
