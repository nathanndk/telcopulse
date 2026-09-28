CREATE SCHEMA subscriber;
CREATE SCHEMA package;
CREATE SCHEMA payment;
CREATE SCHEMA notification;
-- Existing records remain intact. Each extracted service owns its schema.
CREATE TABLE subscriber.profiles AS SELECT id,name,msisdn FROM customers;
ALTER TABLE subscriber.profiles ADD PRIMARY KEY(id);
ALTER TABLE subscriber.profiles ADD COLUMN account_state text NOT NULL DEFAULT 'ACTIVE';
CREATE TABLE payment.accounts AS SELECT id AS customer_id,balance_idr FROM customers;
ALTER TABLE payment.accounts ADD PRIMARY KEY(customer_id);
ALTER TABLE payment.accounts ADD CHECK(balance_idr>=0);
CREATE TABLE package.catalog (LIKE packages INCLUDING ALL);
INSERT INTO package.catalog SELECT * FROM packages;
CREATE TABLE payment.reservations (
 transaction_id text PRIMARY KEY, request jsonb NOT NULL,
 customer_id text NOT NULL REFERENCES payment.accounts(customer_id),
 amount_idr bigint NOT NULL CHECK(amount_idr>0), method text NOT NULL,
 status text NOT NULL CHECK(status IN ('RESERVED','CONFIRMED','RELEASED','FAILED')),
 error_code text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE package.activations (
 transaction_id text PRIMARY KEY, request jsonb NOT NULL,
 customer_id text NOT NULL, package_id text NOT NULL REFERENCES package.catalog(id),
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE notification.deliveries (
 transaction_id text PRIMARY KEY, request jsonb NOT NULL,
 status text NOT NULL DEFAULT 'DELIVERED', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE purchase_workflows (
 idempotency_key text PRIMARY KEY, transaction_id text NOT NULL UNIQUE,
 request jsonb NOT NULL, result jsonb NOT NULL, operation jsonb NOT NULL DEFAULT '{}',
 state text NOT NULL DEFAULT 'PROCESSING' CHECK(state IN ('PROCESSING','SUCCESS','FAILED')),
 attempts integer NOT NULL DEFAULT 0, last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX purchase_workflows_pending_idx ON purchase_workflows(updated_at) WHERE state='PROCESSING';
-- Extracted catalogs are addressed through APIs, not cross-service foreign keys.
ALTER TABLE transactions DROP CONSTRAINT transactions_customer_id_fkey;
ALTER TABLE transactions DROP CONSTRAINT transactions_package_id_fkey;
