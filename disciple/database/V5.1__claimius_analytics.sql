-- ============================================================================
-- Claimius V5.1 Analytics
-- ----------------------------------------------------------------------------
-- A disciple holds no capture. It forwards every consent decision and every
-- capture batch to the prophet on its app key, and it reads capture back from
-- the prophet, the same way it reads the audit log.
--
-- Capture is app owned data: every row carries the app_id of the app that
-- recorded it. A disciple mirrors exactly one app and carries no app_id column
-- on any replica table, so capture has no shape here. Cross app reading is a
-- live app_object link on the prophet from the reading app to the recording
-- app's samna_app row, resolved by claimius.capture_app_ids at read time.
-- ============================================================================

DROP TABLE IF EXISTS claimius.analytics_event;
DROP TABLE IF EXISTS claimius.analytics_session;
DROP TABLE IF EXISTS claimius.consent;
