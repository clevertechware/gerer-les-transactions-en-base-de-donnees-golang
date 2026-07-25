DROP INDEX IF EXISTS companies_verification_status_idx;

ALTER TABLE companies
    DROP CONSTRAINT IF EXISTS companies_verification_ref_check,
    DROP CONSTRAINT IF EXISTS companies_seat_limit_check,
    DROP CONSTRAINT IF EXISTS companies_verification_status_check;

ALTER TABLE companies
    DROP COLUMN IF EXISTS seat_limit,
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS verification_ref,
    DROP COLUMN IF EXISTS verification_status;

DROP INDEX IF EXISTS users_username_key;
DROP INDEX IF EXISTS users_email_key;

ALTER TABLE user_companies
    ALTER COLUMN role DROP NOT NULL,
    ALTER COLUMN role DROP DEFAULT;

DROP INDEX IF EXISTS user_companies_company_id_idx;

ALTER TABLE user_companies
    DROP CONSTRAINT IF EXISTS user_companies_company_id_fkey,
    DROP CONSTRAINT IF EXISTS user_companies_user_id_fkey;
