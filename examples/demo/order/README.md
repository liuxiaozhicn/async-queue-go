# Scenario 1: Order + Payment Callback

This demo keeps only 2 business routes:

- `POST /order/create`: create order and schedule active payment-query task
- `POST /payment/callback`: simulate callback; if callback arrives first, cancel the scheduled query task

It uses a dedicated Redis channel prefix: `order:payment`.

Queue behavior:

- After order creation, query job is scheduled with delay
- If callback is missing, active query runs
- Active query randomly returns paid / unpaid
- If still unpaid, queue retry continues until message max attempts is reached

## Run

```bash
go run ./examples/demo/order
```

## API Examples

Create order:

```bash
curl -s -X POST http://127.0.0.1:8080/order/create \
  -H "Content-Type: application/json" \
  -d '{"order_no":"ORD-1001"}'
```

Response contains `job_id`.  
Later callback uses this scheduled message id to cancel pending query.

Payment callback:

```bash
curl -s -X POST http://127.0.0.1:8080/payment/callback \
  -H "Content-Type: application/json" \
  -d '{"order_no":"ORD-1001"}'
```
