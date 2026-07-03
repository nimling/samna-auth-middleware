ALTER TABLE claimius.sync_event ADD COLUMN IF NOT EXISTS app_id UUID;

UPDATE claimius.sync_event
SET app_id = (payload ->> 'app_id')::uuid
WHERE app_id IS NULL AND payload ? 'app_id';

CREATE INDEX IF NOT EXISTS idx_sync_event_app_seq ON claimius.sync_event (app_id, seq);
