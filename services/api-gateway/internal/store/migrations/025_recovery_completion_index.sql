-- Recovery assessment looks at terminal workflow completion time, not the
-- purchase's original creation time. Keep pending workflows out of this index.
CREATE INDEX IF NOT EXISTS purchase_workflows_terminal_completed_idx
 ON purchase_workflows(updated_at DESC, transaction_id)
 WHERE state IN ('SUCCESS','FAILED');
