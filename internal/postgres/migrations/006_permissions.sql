ALTER TABLE accounts ADD COLUMN direct_permissions text[] NOT NULL DEFAULT ARRAY['own.username','own.display_name'];
ALTER TABLE accounts ADD COLUMN require_mfa boolean NOT NULL DEFAULT false;
ALTER TABLE accounts ADD COLUMN mfa_enrolled boolean NOT NULL DEFAULT false;
ALTER TABLE accounts ADD COLUMN permissions_version bigint NOT NULL DEFAULT 1;
UPDATE accounts SET direct_permissions=ARRAY['site.superuser'] WHERE is_admin;
ALTER TABLE accounts DROP COLUMN is_admin;
