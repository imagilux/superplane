-- Installation-admin toggle to disable email/password login at runtime, on top
-- of the ENABLE_PASSWORD_LOGIN env and BLOCK_SIGNUP.
ALTER TABLE installation_metadata ADD COLUMN password_login_disabled boolean NOT NULL DEFAULT false;
