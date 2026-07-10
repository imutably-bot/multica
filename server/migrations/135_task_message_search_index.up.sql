-- GIN indexes on task_message content/output for LIKE '%keyword%' queries
-- (pg_bigm), so issue search can also match against agent execution logs
-- (see buildSearchQuery in server/internal/handler/issue.go). Only created
-- when pg_bigm is installed, matching the pattern used for issue/comment
-- search indexes (032/033).
DO $$
BEGIN
  CREATE INDEX idx_task_message_content_bigm ON task_message USING gin (COALESCE(content, '') gin_bigm_ops);
  CREATE INDEX idx_task_message_output_bigm ON task_message USING gin (COALESCE(output, '') gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram indexes on task_message (pg_bigm not installed)';
END
$$;
