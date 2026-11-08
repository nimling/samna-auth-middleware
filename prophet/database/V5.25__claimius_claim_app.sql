UPDATE claimius.user_claim uc
SET sa_deleted_at = now(), sa_updated_at = now()
FROM claimius.claim c
WHERE c.id = uc.claim_id
  AND uc.app_id <> c.app_id
  AND uc.sa_deleted_at IS NULL
  AND EXISTS (
      SELECT 1 FROM claimius.user_claim live
      WHERE live.claim_id = uc.claim_id AND live.user_id = uc.user_id
        AND live.app_id = c.app_id AND live.sa_deleted_at IS NULL
  );

UPDATE claimius.user_claim uc
SET app_id = c.app_id, sa_updated_at = now()
FROM claimius.claim c
WHERE c.id = uc.claim_id
  AND uc.app_id <> c.app_id;

UPDATE claimius.claim_object co
SET app_id = c.app_id, sa_updated_at = now()
FROM claimius.claim c
WHERE c.id = co.claim_id
  AND co.app_id <> c.app_id;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'claim_app_id_id_key') THEN
        ALTER TABLE claimius.claim ADD CONSTRAINT claim_app_id_id_key UNIQUE (app_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'claim_object_app_claim_fkey') THEN
        ALTER TABLE claimius.claim_object ADD CONSTRAINT claim_object_app_claim_fkey FOREIGN KEY (app_id, claim_id) REFERENCES claimius.claim (app_id, id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'user_claim_app_claim_fkey') THEN
        ALTER TABLE claimius.user_claim ADD CONSTRAINT user_claim_app_claim_fkey FOREIGN KEY (app_id, claim_id) REFERENCES claimius.claim (app_id, id);
    END IF;
END $$;

COMMENT ON CONSTRAINT claim_object_app_claim_fkey ON claimius.claim_object IS 'A binding lives in the app of its claim, so the feed and the snapshot of an app never carry a binding whose claim they do not.';
COMMENT ON CONSTRAINT user_claim_app_claim_fkey ON claimius.user_claim IS 'A grant lives in the app of its claim, so the feed and the snapshot of an app never carry a grant whose claim they do not.';
