-- Every invariant this file pushes into the schema is an invariant the
-- application no longer has to defend by hand, and one less reason to open a
-- transaction. That is the "C" of ACID, put to work.

-- 1. Referential integrity between a membership and the rows it points at.
--    Without these, an orphan membership is a state the database happily
--    accepts and the application has to check for.
ALTER TABLE user_companies
    ADD CONSTRAINT user_companies_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    ADD CONSTRAINT user_companies_company_id_fkey
        FOREIGN KEY (company_id) REFERENCES companies (id) ON DELETE CASCADE;

-- Foreign keys are not indexed automatically by PostgreSQL. user_id is already
-- covered by the leading column of the primary key; company_id is not.
CREATE INDEX user_companies_company_id_idx ON user_companies (company_id);

-- A membership always carries a role.
UPDATE user_companies SET role = 'member' WHERE role IS NULL;
ALTER TABLE user_companies
    ALTER COLUMN role SET DEFAULT 'member',
    ALTER COLUMN role SET NOT NULL;

-- 2. Uniqueness. Partial, because a soft-deleted user must not keep reserving
--    an address someone else could legitimately reuse.
CREATE UNIQUE INDEX users_email_key
    ON users (email) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_username_key
    ON users (username) WHERE deleted_at IS NULL;

-- 3. Verification state, driven by the slow third party in cmd/remote.
--    verification_status is what makes the corrected UPDATE idempotent:
--    "... WHERE verification_status = 'pending'" both detects a race and makes
--    a replay a no-op, without taking an explicit lock.
ALTER TABLE companies
    ADD COLUMN verification_status varchar(16) NOT NULL DEFAULT 'pending',
    ADD COLUMN verification_ref varchar(64),
    ADD COLUMN verified_at timestamptz,
    -- 4. Seat quota: a read (count the members) followed by a write (insert one
    --    more) is exactly the shape that produces a serialization anomaly under
    --    concurrency. This column is what makes that demonstrable.
    ADD COLUMN seat_limit int NOT NULL DEFAULT 3;

ALTER TABLE companies
    ADD CONSTRAINT companies_verification_status_check
        CHECK (verification_status IN ('pending', 'verified', 'rejected')),
    ADD CONSTRAINT companies_seat_limit_check
        CHECK (seat_limit > 0),
    -- A verified company must carry its provider reference, and only a verified
    -- one may. The application never has to assert this.
    ADD CONSTRAINT companies_verification_ref_check
        CHECK (
            (verification_status = 'verified' AND verification_ref IS NOT NULL AND verified_at IS NOT NULL)
            OR (verification_status <> 'verified' AND verification_ref IS NULL AND verified_at IS NULL)
        );

CREATE INDEX companies_verification_status_idx
    ON companies (verification_status) WHERE deleted_at IS NULL;
