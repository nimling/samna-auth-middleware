CREATE OR REPLACE FUNCTION claimius.get_secret_value(
    p_user_id    UUID,
    p_secret_id  UUID
) RETURNS TABLE (value TEXT, app_id UUID) AS $$
DECLARE
    v_secret claimius.samna_secret;
BEGIN
    SELECT s.* INTO v_secret
    FROM claimius.samna_secret s
    WHERE s.id = p_secret_id
      AND s.sa_deleted_at IS NULL;

    IF NOT FOUND OR NOT claimius.check_user_active(v_secret.app_id, p_user_id) THEN
        RETURN;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM claimius.get_access(p_user_id, v_secret.app_id, p_secret_id, 'claimius.samna_secret', 4)) THEN
        RETURN;
    END IF;

    RETURN QUERY SELECT v_secret.value, v_secret.app_id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = claimius, pg_temp;

COMMENT ON FUNCTION claimius.get_secret_value(p_user_id uuid, p_secret_id uuid) IS 'The stored value of one secret, still ciphertext under the seed of the app that wrote it, with that app''s id, when p_user_id is active in the app and reaches the secret with read through get_access: the claim of its creator that insert_secret bound, or any claim the user holds on it. No row for an inactive user, a user without read, an unknown and a deleted secret. The seed never reaches the database; the service opens the value in process with the seed of app_id.';
