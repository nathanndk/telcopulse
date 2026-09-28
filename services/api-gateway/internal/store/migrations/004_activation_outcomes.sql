CREATE TABLE package.activation_outcomes (
 transaction_id text PRIMARY KEY, request jsonb NOT NULL,
 status text NOT NULL CHECK(status IN ('SUCCESS','FAILED')),
 error_code text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO package.activation_outcomes(transaction_id,request,status)
 SELECT transaction_id,request,'SUCCESS' FROM package.activations;
