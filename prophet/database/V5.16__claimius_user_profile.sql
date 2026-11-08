ALTER TABLE claimius.samna_user ADD COLUMN IF NOT EXISTS bio TEXT;
ALTER TABLE claimius.samna_user ADD COLUMN IF NOT EXISTS job_title TEXT;
ALTER TABLE claimius.samna_user ADD COLUMN IF NOT EXISTS company TEXT;
ALTER TABLE claimius.samna_user ADD COLUMN IF NOT EXISTS website TEXT;
ALTER TABLE claimius.samna_user ADD COLUMN IF NOT EXISTS linkedin TEXT;

COMMENT ON COLUMN claimius.samna_user.bio IS 'Optional short public text the user writes about themselves.';
COMMENT ON COLUMN claimius.samna_user.job_title IS 'Optional job title or role.';
COMMENT ON COLUMN claimius.samna_user.company IS 'Optional company or employer name.';
COMMENT ON COLUMN claimius.samna_user.website IS 'Optional personal or professional website URL.';
COMMENT ON COLUMN claimius.samna_user.linkedin IS 'Optional LinkedIn profile URL.';
