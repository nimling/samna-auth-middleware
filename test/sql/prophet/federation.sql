BEGIN;
SELECT plan(34);

CREATE FUNCTION pg_temp.local_app() RETURNS UUID AS $$
    SELECT '50000000-0000-4000-8000-0000000000f1'::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.mirror_app() RETURNS UUID AS $$
    SELECT '50000000-0000-4000-8000-0000000000f2'::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.peer_org() RETURNS UUID AS $$
    SELECT '10000000-0000-4000-8000-0000000000f3'::UUID
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.actor() RETURNS UUID AS $$
    SELECT (SELECT sa_created_by FROM claimius.samna_app WHERE id = pg_temp.local_app())
$$ LANGUAGE sql;

CREATE FUNCTION pg_temp.apply(p_operation TEXT, p_event_type TEXT, p_payload JSONB) RETURNS BOOLEAN AS $$
DECLARE
    v_applied BOOLEAN;
BEGIN
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    v_applied := claimius.apply_peer_event('sqltest_peer', 'sqltest_fed_mirror', p_operation, p_event_type, p_payload);
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
    RETURN v_applied;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION pg_temp.apply_local(p_event_type TEXT, p_payload JSONB) RETURNS BOOLEAN AS $$
DECLARE
    v_applied BOOLEAN;
BEGIN
    PERFORM set_config('claimius.replay_mode', 'true', TRUE);
    v_applied := claimius.apply_peer_event('sqltest_peer', 'sqltest_fed_local', 'upsert', p_event_type, p_payload);
    PERFORM set_config('claimius.replay_mode', 'false', TRUE);
    RETURN v_applied;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION pg_temp.claim(p_id UUID, p_app UUID, p_owner UUID) RETURNS JSONB AS $$
    SELECT jsonb_build_object(
        'id', p_id, 'app_id', p_app, 'name', 'Peer claim', 'sa_access', 4, 'inherits', TRUE, 'type', 'user',
        'sa_owner_id', p_owner, 'sa_root_id', p_owner, 'sa_created_by', pg_temp.actor(),
        'sa_created_at', '2026-01-01T00:00:00Z', 'sa_updated_at', '2026-01-01T00:00:00Z')
$$ LANGUAGE sql;

DO $$
DECLARE
    v_system JSONB;
BEGIN
    v_system := claimius.upsert_system_app(
        p_system_app_slug         => 'sqltest_home',
        p_system_app_name         => 'SQL federation test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    VALUES (pg_temp.local_app(), 'sqltest_fed_local', 'Local app', 'key', 'seed',
            (v_system -> 'organization' ->> 'id')::UUID, (v_system -> 'user_claim' ->> 'id')::UUID);
    PERFORM claimius.upsert_app_object(pg_temp.local_app(), 'claimius.organization', (v_system -> 'organization' ->> 'id')::UUID);

    INSERT INTO claimius.prophet_peer (prophet, endpoint, app_slug, app_secret, sa_created_by)
    VALUES ('sqltest_peer', 'http://peer.test', 'sqltest_fed_mirror', 'secret', pg_temp.actor());
END $$;

CREATE TEMP TABLE local_claim AS
SELECT (claimius.insert_claim(
    p_app_id        => pg_temp.local_app(),
    p_name          => 'Local claim',
    p_description   => NULL,
    p_sa_access     => 4,
    p_sa_owner_id   => (SELECT sa_owner_id FROM claimius.samna_app WHERE id = pg_temp.local_app()),
    p_sa_root_id    => (SELECT sa_owner_id FROM claimius.samna_app WHERE id = pg_temp.local_app()),
    p_sa_created_by => pg_temp.actor()
) -> 'claim' ->> 'id')::UUID AS id;

SELECT ok(
    pg_temp.apply('upsert', 'claimius.samna_app', jsonb_build_object(
        'id', pg_temp.mirror_app(), 'slug', 'sqltest_fed_mirror', 'name', 'Mirrored app', 'private_key', 'key',
        'private_seed', 'seed', 'sa_owner_id', pg_temp.peer_org(), 'sa_created_by', pg_temp.actor(), 'home_prophet', NULL)),
    'a peer''s app row applies'
);

SELECT is(
    (SELECT home_prophet FROM claimius.samna_app WHERE id = pg_temp.mirror_app()),
    'sqltest_peer',
    'a mirrored app keeps the peer it came from as its home'
);

SELECT ok(
    pg_temp.apply('upsert', 'claimius.organization', jsonb_build_object(
        'id', pg_temp.peer_org(), 'name', 'Peer org', 'type', 'company', 'private_seed', 'aa',
        'sa_owner_id', pg_temp.peer_org(), 'sa_root_id', pg_temp.peer_org(), 'sa_level', 0,
        'sa_created_by', pg_temp.actor())),
    'a peer''s organization applies'
);

SELECT ok(
    claimius.app_has_object(pg_temp.mirror_app(), 'claimius.organization', pg_temp.peer_org()),
    'a mirrored organization is linked to the mirrored app'
);

SELECT ok(
    pg_temp.apply('upsert', 'claimius.claim', pg_temp.claim('20000000-0000-4000-8000-0000000000f4', pg_temp.mirror_app(), pg_temp.peer_org())),
    'a peer''s claim applies'
);

SELECT ok(
    EXISTS (SELECT 1 FROM claimius.access_diff WHERE app_id = pg_temp.mirror_app() AND kind = 'object'
              AND object_type = 'claimius.claim' AND object_id = '20000000-0000-4000-8000-0000000000f4'),
    'an applied claim raises the area of its claim object on the mirror'
);

SELECT ok(
    NOT pg_temp.apply('upsert', 'claimius.claim', pg_temp.claim((SELECT id FROM local_claim), pg_temp.mirror_app(), pg_temp.peer_org())),
    'a mirrored claim whose id a local app holds is refused'
);

SELECT is(
    (SELECT app_id FROM claimius.claim WHERE id = (SELECT id FROM local_claim)),
    pg_temp.local_app(),
    'the refused row leaves the local row as it was'
);

SELECT is(
    (SELECT count(*) FROM claimius.peer_collision WHERE object_id = (SELECT id FROM local_claim) AND prophet = 'sqltest_peer'),
    1::BIGINT,
    'the refused row is recorded with the peer it came from'
);

SELECT ok(
    NOT pg_temp.apply('upsert', 'claimius.organization', jsonb_build_object(
        'id', (SELECT sa_owner_id FROM claimius.samna_app WHERE id = pg_temp.local_app()), 'name', 'Hijack', 'private_seed', '',
        'sa_owner_id', pg_temp.peer_org(), 'sa_root_id', pg_temp.peer_org(), 'sa_created_by', pg_temp.actor())),
    'a mirrored organization whose id a local app reaches is refused'
);

SELECT ok(
    NOT pg_temp.apply_local('claimius.claim', pg_temp.claim(gen_random_uuid(), pg_temp.local_app(), pg_temp.peer_org())),
    'a peer''s row of an app this prophet is home for is refused'
);

SELECT is(claimius.get_app_home(pg_temp.mirror_app()), 'sqltest_peer', 'a mirrored app is decided by its peer');
SELECT is(claimius.get_app_home(pg_temp.local_app()), NULL, 'a local app is decided here');

SELECT throws_ok(
    format('INSERT INTO claimius.claim (app_id, name, sa_access, sa_owner_id, sa_root_id, sa_created_by) VALUES (%L, %L, 4, %L, %L, %L)',
           pg_temp.mirror_app(), 'Routed here', pg_temp.peer_org(), pg_temp.peer_org(), pg_temp.actor()),
    'P0409',
    NULL,
    'a write outside replay to an app a peer decides is refused'
);

SELECT lives_ok(
    format('UPDATE claimius.claim SET name = %L WHERE id = %L', 'Local renamed', (SELECT id FROM local_claim)),
    'a write to an app decided here passes'
);

SELECT is(claimius.check_sync_home(pg_temp.local_app(), NULL), NULL, 'an unbound consumer of an app homed here is served');
SELECT is(claimius.check_sync_home(pg_temp.local_app(), 'sqltest_home'), NULL, 'a consumer bound here is served');
SELECT is(claimius.check_sync_home(pg_temp.local_app(), 'elsewhere'), 'elsewhere', 'a consumer bound to another prophet without a handover keeps it');
SELECT is(claimius.check_sync_home(pg_temp.mirror_app(), NULL), 'sqltest_peer', 'a mirror sends consumers of the app to its home');

SELECT throws_ok(
    format('SELECT claimius.insert_app_handover(%L, %L, %L)', pg_temp.local_app(), 'unknown_peer', pg_temp.actor()),
    'P0409', NULL, 'an app is offered only to an active peer'
);

SELECT throws_ok(
    format('SELECT claimius.insert_app_handover(%L, %L, %L)', pg_temp.mirror_app(), 'sqltest_peer', pg_temp.actor()),
    'P0409', NULL, 'only the home offers an app'
);

CREATE TEMP TABLE offer AS
SELECT (claimius.insert_app_handover(pg_temp.local_app(), 'sqltest_peer', pg_temp.actor()) -> 'app_handover') AS row;

SELECT is(
    (SELECT status FROM claimius.app_handover WHERE id = (SELECT (row ->> 'id')::UUID FROM offer)),
    'offered',
    'an offer starts offered'
);

SELECT throws_ok(
    format('UPDATE claimius.claim SET name = %L WHERE id = %L', 'Frozen', (SELECT id FROM local_claim)),
    'P0409', NULL, 'an offered app takes no write on the prophet that offered it'
);

SELECT throws_ok(
    format('SELECT claimius.insert_app_handover(%L, %L, %L)', pg_temp.local_app(), 'sqltest_peer', pg_temp.actor()),
    'P0409', NULL, 'an app under offer is not offered twice'
);

SELECT throws_ok(
    format('SELECT claimius.update_app_handover(%L, %L)', (SELECT (row ->> 'id')::UUID FROM offer), pg_temp.actor()),
    'P0409', NULL, 'the prophet that offered an app does not accept it'
);

SELECT is(claimius.check_sync_home(pg_temp.local_app(), NULL), NULL, 'the offering prophet serves the feed until the acceptance reaches it');

SELECT ok(
    pg_temp.apply_local('claimius.samna_app', (SELECT to_jsonb(a) FROM claimius.samna_app a WHERE a.id = pg_temp.local_app()) - 'home_prophet')
    AND pg_temp.apply_local('claimius.app_handover', (SELECT row || '{"status": "accepted"}' FROM offer)),
    'the target''s feed carries the app and the accepted handover back'
);

SELECT is(
    (SELECT ARRAY[h.status, a.home_prophet] FROM claimius.app_handover h JOIN claimius.samna_app a ON a.id = h.app_id
     WHERE h.id = (SELECT (row ->> 'id')::UUID FROM offer)),
    ARRAY['accepted', 'sqltest_peer'],
    'the acceptance moves the app''s home to the target'
);

SELECT is(claimius.check_sync_home(pg_temp.local_app(), 'sqltest_home'), 'sqltest_peer', 'the old home refuses a disciple still bound to it');

SELECT ok(
    pg_temp.apply('upsert', 'claimius.app_handover', jsonb_build_object(
        'id', '60000000-0000-4000-8000-0000000000f5', 'app_id', pg_temp.mirror_app(), 'from_prophet', 'sqltest_peer',
        'to_prophet', 'sqltest_home', 'status', 'offered', 'sa_created_by', pg_temp.actor())),
    'the home''s offer to this prophet arrives on the app''s feed'
);

SELECT is(claimius.check_sync_home(pg_temp.mirror_app(), 'sqltest_peer'), 'sqltest_peer', 'before the acceptance the target does not serve the home''s disciple');

SELECT is(
    (SELECT claimius.update_app_handover('60000000-0000-4000-8000-0000000000f5', pg_temp.actor()) -> 'app_handover' ->> 'status'),
    'accepted',
    'the target accepts the offer'
);

SELECT is(
    ARRAY[claimius.check_sync_home(pg_temp.mirror_app(), 'sqltest_peer'), claimius.check_sync_home(pg_temp.mirror_app(), 'elsewhere'), claimius.get_app_home(pg_temp.mirror_app())],
    ARRAY[NULL, 'elsewhere', NULL],
    'the new home serves the disciple the handover moved and no disciple of another prophet'
);

SELECT ok(
    NOT pg_temp.apply('upsert', 'claimius.claim', pg_temp.claim('20000000-0000-4000-8000-0000000000f4', pg_temp.mirror_app(), pg_temp.peer_org()))
    AND (SELECT count(*) = 0 FROM claimius.get_sync_events(pg_temp.mirror_app(), 0, 0, 1000) WHERE event_type = 'claimius.app_handover')
    AND (SELECT count(*) = 1 FROM claimius.get_sync_events(pg_temp.mirror_app(), 0, 0, 1000, TRUE) WHERE event_type = 'claimius.app_handover'),
    'the old home''s feed no longer applies, and the acceptance travels to peers only'
);

SELECT * FROM finish();
ROLLBACK;
