CREATE TABLE IF NOT EXISTS customers (
 id text PRIMARY KEY, name text NOT NULL, msisdn text NOT NULL UNIQUE,
 balance_idr bigint NOT NULL CHECK (balance_idr >= 0)
);
CREATE TABLE IF NOT EXISTS packages (
 id text PRIMARY KEY, name text NOT NULL, data_gb integer NOT NULL,
 days integer NOT NULL, price_idr bigint NOT NULL CHECK (price_idr > 0)
);
CREATE TABLE IF NOT EXISTS transactions (
 id text PRIMARY KEY, trace_id text NOT NULL UNIQUE,
 idempotency_key text NOT NULL UNIQUE, request_hash text NOT NULL,
 customer_id text NOT NULL REFERENCES customers(id), package_id text NOT NULL REFERENCES packages(id),
 environment text NOT NULL CHECK (environment IN ('development','staging')),
 status text NOT NULL CHECK (status IN ('SUCCESS','FAILED')),
 created_at timestamptz NOT NULL DEFAULT now(), result jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS transactions_environment_created_idx ON transactions(environment, created_at DESC, id);
CREATE TABLE IF NOT EXISTS payments (
 id text PRIMARY KEY, transaction_id text NOT NULL UNIQUE REFERENCES transactions(id),
 method text NOT NULL, amount_idr bigint NOT NULL CHECK(amount_idr > 0), status text NOT NULL
);
CREATE TABLE IF NOT EXISTS entitlements (
 id text PRIMARY KEY, transaction_id text NOT NULL UNIQUE REFERENCES transactions(id),
 customer_id text NOT NULL REFERENCES customers(id), package_id text NOT NULL REFERENCES packages(id),
 expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_logs (
 id bigserial PRIMARY KEY, actor text NOT NULL, action text NOT NULL,
 resource text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), details jsonb NOT NULL
);
INSERT INTO customers VALUES
 ('cus-001','Ayu Pratama','628123450123',250000),
 ('cus-002','Bima Santoso','628123450456',100000),
 ('cus-003','Citra Wijaya','628123450789',10000)
 ON CONFLICT DO NOTHING;
INSERT INTO packages VALUES
 ('pkg-3','Internet Essential',3,7,15000),
 ('pkg-10','Internet Everyday',10,30,50000),
 ('pkg-25','Internet Unlimited Days',25,30,100000)
 ON CONFLICT DO NOTHING;
