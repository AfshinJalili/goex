DROP TABLE IF EXISTS refresh_tokens;

ALTER TABLE users
  DROP COLUMN IF EXISTS mfa_enabled,
  DROP COLUMN IF EXISTS mfa_secret;
