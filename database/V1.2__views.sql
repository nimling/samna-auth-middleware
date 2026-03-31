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

CREATE OR REPLACE VIEW samna_auth.active_object AS
SELECT
    co.object_id,
    co.object_type,
    co.claim_id,
    co.sa_level,
    co.inherits,
    co.sa_root_id,
    co.sa_owner_id,
    co.ref_id,
    co.reason,
    co.function,
    co.sa_created_at,
    co.sa_updated_at,
    co.sa_deleted_at
FROM samna_auth.claim_object co
WHERE co.sa_deleted_at IS NULL;

CREATE OR REPLACE VIEW samna_auth.active_users AS
WITH shared_access AS (
    SELECT DISTINCT
        ao.object_id AS access_object_id,
        ac.user_id,
        ac.claim_id,
        ac.sa_access_level,
        ao.sa_owner_id
    FROM samna_auth.active_object ao
    INNER JOIN samna_auth.active_claim ac ON ac.claim_id = ao.claim_id
    WHERE ao.object_type IN ('organization', 'location')
)
SELECT DISTINCT
    uc.user_id AS target_user_id,
    sa.user_id AS user_id,
    sa.claim_id,
    sa.sa_owner_id,
    sa.sa_access_level,
    uc.sa_created_at,
    uc.sa_updated_at,
    uc.sa_deleted_at
FROM samna_auth.user_claim uc
INNER JOIN shared_access sa ON sa.access_object_id = uc.sa_owner_id
WHERE uc.sa_deleted_at IS NULL
  AND uc.user_id != sa.user_id;
