ALTER TABLE claimius.samna_app ADD COLUMN IF NOT EXISTS sa_parent_id UUID;
ALTER TABLE claimius.samna_app ADD COLUMN IF NOT EXISTS sa_root_id UUID;

UPDATE claimius.samna_app
SET sa_parent_id = coalesce(sa_parent_id, id),
    sa_root_id   = coalesce(sa_root_id, id)
WHERE sa_parent_id IS NULL OR sa_root_id IS NULL;

ALTER TABLE claimius.samna_app ALTER COLUMN sa_parent_id SET NOT NULL;
ALTER TABLE claimius.samna_app ALTER COLUMN sa_root_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'samna_app_sa_parent_id_fkey') THEN
        ALTER TABLE claimius.samna_app ADD CONSTRAINT samna_app_sa_parent_id_fkey FOREIGN KEY (sa_parent_id) REFERENCES claimius.samna_app(id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'samna_app_sa_root_id_fkey') THEN
        ALTER TABLE claimius.samna_app ADD CONSTRAINT samna_app_sa_root_id_fkey FOREIGN KEY (sa_root_id) REFERENCES claimius.samna_app(id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_samna_app_sa_parent_id ON claimius.samna_app (sa_parent_id);
CREATE INDEX IF NOT EXISTS idx_samna_app_sa_root_id ON claimius.samna_app (sa_root_id);

COMMENT ON COLUMN claimius.samna_app.sa_parent_id IS 'Parent app. Equals id at root. An Initiate app is another frontend of its root: own slug, redirect URIs, pages and style, while users, claims, organizations and the sync resolve on the root.';
COMMENT ON COLUMN claimius.samna_app.sa_root_id IS 'Root app. Equals id at root. Tokens for an Initiate app''s login are issued for the root.';

DO $$
DECLARE
    v_root UUID;
BEGIN
    PERFORM claimius._register_table_info('claimius.samna_app', TRUE);
    PERFORM claimius._attach_self_root_trigger('claimius.samna_app');
    FOR v_root IN SELECT id FROM claimius.samna_app WHERE id = sa_root_id AND sa_deleted_at IS NULL LOOP
        PERFORM claimius.build_parenthood_tree('claimius.samna_app', v_root);
    END LOOP;
END $$;
