CREATE OR REPLACE VIEW samna_auth.active_claim AS
SELECT uc.id AS id,
       c.id AS claim_id,
       uc.user_id AS user_id,
       c.name AS claim_name,
       c.description AS claim_description,
       c.sa_access_level AS sa_access_level,
       c.type AS claim_type,
       uc.sa_created_by AS sa_created_by,
       c.sa_created_by AS claim_sa_created_by,
       c.sa_owner_id AS claim_sa_owner_id,
       uc.sa_owner_id AS user_sa_owner_id,
       c.sa_root_id AS sa_root_id
FROM samna_auth.user_claim uc
INNER JOIN samna_auth.claim c ON uc.claim_id = c.id
WHERE uc.sa_deleted_at IS NULL
  AND c.sa_deleted_at IS NULL
  AND (uc.starts_at IS NULL OR uc.starts_at <= CURRENT_TIMESTAMP)
  AND (uc.ends_at IS NULL OR uc.ends_at >= CURRENT_TIMESTAMP);
