CREATE TABLE IF NOT EXISTS claimius.profile_item (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id        UUID NOT NULL REFERENCES claimius.samna_app(id),
    user_id       UUID NOT NULL,
    object_type   TEXT NOT NULL,
    object_id     UUID NOT NULL,
    field         TEXT NOT NULL,
    label         TEXT NOT NULL,
    value         TEXT NOT NULL,
    link          TEXT,
    "index"       INTEGER NOT NULL DEFAULT 0,
    sa_deleted_at TIMESTAMPTZ,
    sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sa_updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'profile_item_shape_check') THEN
        ALTER TABLE claimius.profile_item ADD CONSTRAINT profile_item_shape_check CHECK (
            char_length(object_type) BETWEEN 1 AND 120
            AND field ~ '^[a-z][a-z0-9_.]{0,63}$'
            AND char_length(label) BETWEEN 1 AND 80
            AND char_length(value) BETWEEN 1 AND 500
            AND (link IS NULL OR (link ~ '^https://' AND char_length(link) <= 2048))
        );
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_profile_item_key
    ON claimius.profile_item (app_id, user_id, object_type, object_id, field) WHERE sa_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_profile_item_user
    ON claimius.profile_item (user_id, app_id, "index") WHERE sa_deleted_at IS NULL;

COMMENT ON TABLE claimius.profile_item IS 'A field of an object a disciple flags for the public profile of one of its users: the prophet stores the label and value the disciple sends and may show them on the user''s public profile page. Keyed by app, user, object and field; a flag is withdrawn by a soft delete. Never synced to a disciple.';
COMMENT ON COLUMN claimius.profile_item.field IS 'The field of the object the value comes from, lower case, dots for nesting.';
COMMENT ON COLUMN claimius.profile_item.link IS 'An optional https url the value links to on the profile page.';

DROP TRIGGER IF EXISTS tg_update_timestamp ON claimius.profile_item;
CREATE TRIGGER tg_update_timestamp BEFORE UPDATE ON claimius.profile_item
    FOR EACH ROW EXECUTE FUNCTION claimius.update_timestamp();

CREATE OR REPLACE FUNCTION claimius.upsert_profile_item(
    p_app_id      UUID,
    p_user_id     UUID,
    p_object_type TEXT,
    p_object_id   UUID,
    p_field       TEXT,
    p_label       TEXT,
    p_value       TEXT,
    p_link        TEXT DEFAULT NULL,
    p_index       INTEGER DEFAULT 0
) RETURNS claimius.profile_item AS $$
DECLARE
    v_row claimius.profile_item;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM claimius.samna_user u
        WHERE u.user_id = p_user_id AND u.app_id = p_app_id AND u.sa_deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'user % has no live row in app %', p_user_id, p_app_id USING ERRCODE = 'P0404';
    END IF;

    INSERT INTO claimius.profile_item (app_id, user_id, object_type, object_id, field, label, value, link, "index")
    VALUES (p_app_id, p_user_id, p_object_type, p_object_id, p_field, p_label, p_value, NULLIF(p_link, ''), coalesce(p_index, 0))
    ON CONFLICT (app_id, user_id, object_type, object_id, field) WHERE sa_deleted_at IS NULL DO UPDATE SET
        label   = EXCLUDED.label,
        value   = EXCLUDED.value,
        link    = EXCLUDED.link,
        "index" = EXCLUDED."index"
    RETURNING * INTO v_row;
    RETURN v_row;
EXCEPTION WHEN check_violation THEN
    RAISE EXCEPTION 'profile item refused: %', SQLERRM USING ERRCODE = '22023';
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION claimius.upsert_profile_item(UUID, UUID, TEXT, UUID, TEXT, TEXT, TEXT, TEXT, INTEGER) IS 'Flags one field of one object for the public profile of a user of the app, or replaces the label, value, link and index of the live flag with the same key. Refuses with P0404 a user with no live row in the app and with 22023 a field, label, value or link outside the shape check.';

CREATE OR REPLACE FUNCTION claimius.delete_profile_item(
    p_app_id      UUID,
    p_user_id     UUID,
    p_object_type TEXT,
    p_object_id   UUID,
    p_field       TEXT DEFAULT NULL
) RETURNS SETOF claimius.profile_item AS $$
    UPDATE claimius.profile_item
       SET sa_deleted_at = now()
     WHERE app_id = p_app_id
       AND user_id = p_user_id
       AND object_type = p_object_type
       AND object_id = p_object_id
       AND (p_field IS NULL OR field = p_field)
       AND sa_deleted_at IS NULL
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION claimius.delete_profile_item(UUID, UUID, TEXT, UUID, TEXT) IS 'Withdraws the live flags of one object for a user of the app, one field or, with no field, every field of the object. Returns the withdrawn rows.';

CREATE OR REPLACE FUNCTION claimius.get_profile_items(
    p_user_id UUID,
    p_app_id  UUID DEFAULT NULL
) RETURNS TABLE (
    id            UUID,
    app_id        UUID,
    app_slug      TEXT,
    app_name      TEXT,
    user_id       UUID,
    object_type   TEXT,
    object_id     UUID,
    field         TEXT,
    label         TEXT,
    value         TEXT,
    link          TEXT,
    "index"       INTEGER,
    sa_created_at TIMESTAMPTZ,
    sa_updated_at TIMESTAMPTZ
) AS $$
    SELECT p.id, p.app_id, a.slug::TEXT, a.name::TEXT, p.user_id, p.object_type, p.object_id, p.field,
           p.label, p.value, p.link, p."index", p.sa_created_at, p.sa_updated_at
    FROM claimius.profile_item p
    JOIN claimius.samna_app a ON a.id = p.app_id AND a.sa_deleted_at IS NULL
    JOIN claimius.samna_user u ON u.user_id = p.user_id AND u.app_id = p.app_id AND u.sa_deleted_at IS NULL
    WHERE p.user_id = p_user_id
      AND p.sa_deleted_at IS NULL
      AND (p_app_id IS NULL OR p.app_id = p_app_id)
    ORDER BY a.name, p."index", p.sa_created_at;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION claimius.get_profile_items(UUID, UUID) IS 'The live profile flags of a user, of one app or of every live app the user still has a live row in, with the app''s slug and name, ordered by app name, index and creation.';
