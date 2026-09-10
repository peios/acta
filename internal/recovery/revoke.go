// Package recovery contains shared production cutover safeguards.
package recovery

// RevokeSQL runs after restoring a database and before enabling integrations.
// It is deliberately idempotent so interrupted recovery can repeat it safely.
const RevokeSQL = `BEGIN;
DELETE FROM security_flows;
DELETE FROM oauth_requests;
DELETE FROM device_requests;
DELETE FROM setup_grants;
DELETE FROM account_links;
DELETE FROM browser_sessions;
DELETE FROM push_deliveries;
UPDATE notifications SET read_at=clock_timestamp(),resolved_at=clock_timestamp() WHERE read_at IS NULL OR resolved_at IS NULL;
COMMIT;`
