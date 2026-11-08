CREATE UNLOGGED TABLE IF NOT EXISTS claimius.rate_limit (
    key text PRIMARY KEY,
    tat timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_rate_limit_tat ON claimius.rate_limit (tat);

GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.rate_limit TO claimius_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON claimius.rate_limit TO claimius_reader;

CREATE OR REPLACE FUNCTION claimius.rate_limit_gcra(
    p_key    text,
    p_rate   double precision,
    p_burst  double precision,
    p_period double precision,
    p_cost   double precision
) RETURNS TABLE (
    allowed     boolean,
    remaining   integer,
    retry_after double precision,
    reset_after double precision
) AS $$
DECLARE
    v_emission     double precision := p_period / p_rate;
    v_increment    double precision := (p_period / p_rate) * p_cost;
    v_burst_offset double precision := (p_period / p_rate) * p_burst;
    v_now          double precision := extract(epoch from clock_timestamp());
    v_tat          double precision;
    v_new_tat      double precision;
    v_allow_at     double precision;
    v_diff         double precision;
    v_remaining    double precision;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(p_key));

    SELECT extract(epoch from tat) INTO v_tat
    FROM claimius.rate_limit WHERE key = p_key;

    IF v_tat IS NULL THEN
        v_tat := v_now;
    END IF;
    v_tat := greatest(v_tat, v_now);

    v_new_tat := v_tat + v_increment;
    v_allow_at := v_new_tat - v_burst_offset;
    v_diff := v_now - v_allow_at;
    v_remaining := floor(v_diff / v_emission);

    IF v_remaining < 0 THEN
        allowed := false;
        remaining := 0;
        retry_after := -v_diff;
        reset_after := v_tat - v_now;
        RETURN NEXT;
        RETURN;
    END IF;

    reset_after := v_new_tat - v_now;
    IF reset_after > 0 THEN
        INSERT INTO claimius.rate_limit (key, tat)
        VALUES (p_key, to_timestamp(v_new_tat))
        ON CONFLICT (key) DO UPDATE SET tat = to_timestamp(v_new_tat);
    END IF;

    allowed := true;
    remaining := v_remaining::integer;
    retry_after := -1;
    RETURN NEXT;
END;
$$ LANGUAGE plpgsql;

GRANT EXECUTE ON FUNCTION claimius.rate_limit_gcra(text, double precision, double precision, double precision, double precision) TO claimius_writer;
GRANT EXECUTE ON FUNCTION claimius.rate_limit_gcra(text, double precision, double precision, double precision, double precision) TO claimius_reader;
