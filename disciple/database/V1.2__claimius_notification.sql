CREATE TABLE IF NOT EXISTS claimius.notification (
                                                     id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                     user_id         UUID NOT NULL,
                                                     title           TEXT NOT NULL,
                                                     description     TEXT,
                                                     style           JSONB NOT NULL DEFAULT '{}'::jsonb,
                                                     type            TEXT NOT NULL,
                                                     link            TEXT,
                                                     object_id       UUID,
                                                     object_type     TEXT,
                                                     sa_created_by   UUID,
                                                     sa_owner_id     UUID,
                                                     read_at         TIMESTAMPTZ,
                                                     closed_at       TIMESTAMPTZ,
                                                     repeats_at      INTERVAL,
                                                     sa_deleted_at   TIMESTAMPTZ,
                                                     sa_created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                                     sa_updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE claimius.notification IS 'Per user notification. One row per recipient.';
COMMENT ON COLUMN claimius.notification.user_id IS 'Recipient samna user_id. The fan out key.';
COMMENT ON COLUMN claimius.notification.style IS 'Per notification presentation customization as JSON.';
COMMENT ON COLUMN claimius.notification.object_id IS 'Target object the notification points at, when any.';
COMMENT ON COLUMN claimius.notification.object_type IS 'Qualified object type of object_id, when any.';
COMMENT ON COLUMN claimius.notification.sa_created_by IS 'User that triggered the notification, when a user did.';
COMMENT ON COLUMN claimius.notification.sa_owner_id IS 'Owning organization in a managed environment, when any.';
COMMENT ON COLUMN claimius.notification.repeats_at IS 'Recurrence interval for repeating notifications.';

CREATE INDEX IF NOT EXISTS idx_notification_user ON claimius.notification (user_id, sa_created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_object ON claimius.notification (object_id, object_type);
CREATE INDEX IF NOT EXISTS idx_notification_sa_deleted_at ON claimius.notification (sa_deleted_at);
