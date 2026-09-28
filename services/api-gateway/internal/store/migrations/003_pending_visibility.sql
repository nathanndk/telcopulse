CREATE VIEW operation_transactions AS
 SELECT id,trace_id,environment,status,created_at,result FROM transactions
 UNION ALL
 SELECT transaction_id,result->>'trace_id',result->>'environment','PROCESSING',created_at,
        jsonb_set(result,'{status}','"PROCESSING"'::jsonb)
 FROM purchase_workflows WHERE state='PROCESSING';
