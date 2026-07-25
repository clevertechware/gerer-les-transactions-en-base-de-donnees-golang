-- PostgreSQL data prefill script for existing schema with UUID IDs
-- Populates with:
--   - 100,000 users
--   - 200,000 companies
--   - User-company relationships with distribution:
--     * 70,000 users linked to 1 company each
--     * 20,000 users linked to 2 companies each
--     * 10,000 users linked to 3 companies each
--   - Total: ~140,000 relationships

\timing on

-- ============================================
-- CLEAR EXISTING DATA (but keep schema)
-- ============================================

SET session_replication_role = 'replica';

TRUNCATE TABLE user_companies CASCADE;
TRUNCATE TABLE users CASCADE;
TRUNCATE TABLE companies CASCADE;

SET session_replication_role = 'origin';


-- ============================================
-- POPULATE COMPANIES (200,000)
-- ============================================

DO $$
DECLARE
    batch_size INTEGER := 5000;
    total INTEGER := 200000;
    batches INTEGER;
    i INTEGER;
    start_idx INTEGER;
    end_idx INTEGER;
    industry_names TEXT[] := ARRAY['Tech', 'Finance', 'Healthcare', 'Retail', 'Manufacturing'];
BEGIN
    batches := CEIL(total::FLOAT / batch_size);
    
    FOR i IN 1..batches LOOP
        start_idx := (i - 1) * batch_size + 1;
        end_idx := LEAST(i * batch_size, total);
        
        INSERT INTO companies (id, name, address, verification_status, seat_limit, created_at, updated_at)
        SELECT 
            uuidv7(),
            'Company ' || seq || ' - ' || industry_names[(seq % 5) + 1] || ' Corp',
            'Address ' || seq || ', Business District, City',
            'verified',
            (random() * 100)::integer + 1,
            NOW() - (random() * interval '365 days'),
            NOW()
        FROM generate_series(start_idx, end_idx) AS seq;
        
        COMMIT;
        RAISE NOTICE 'Inserted companies batch % of %', i, batches;
    END LOOP;
END $$;


-- ============================================
-- POPULATE USERS (100,000)
-- ============================================

DO $$
DECLARE
    batch_size INTEGER := 5000;
    total INTEGER := 100000;
    batches INTEGER;
    i INTEGER;
    start_idx INTEGER;
    end_idx INTEGER;
    first_names TEXT[] := ARRAY['John', 'Jane', 'Michael', 'Emily', 'David', 'Sarah', 'Robert', 'Jennifer', 'Thomas', 'Lisa'];
    last_names TEXT[] := ARRAY['Smith', 'Johnson', 'Williams', 'Brown', 'Jones', 'Garcia', 'Miller', 'Davis', 'Rodriguez', 'Martinez'];
BEGIN
    batches := CEIL(total::FLOAT / batch_size);
    
    FOR i IN 1..batches LOOP
        start_idx := (i - 1) * batch_size + 1;
        end_idx := LEAST(i * batch_size, total);
        
        INSERT INTO users (id, email, first_name, last_name, username, created_at, updated_at)
        SELECT 
            uuidv7(),
            'user.' || seq || '@example.com',
            first_names[(seq % 10) + 1],
            last_names[(seq % 10) + 1],
            'user_' || seq,
            NOW() - (random() * interval '365 days'),
            NOW()
        FROM generate_series(start_idx, end_idx) AS seq;
        
        COMMIT;
        RAISE NOTICE 'Inserted users batch % of %', i, batches;
    END LOOP;
END $$;


-- ============================================
-- POPULATE USER-COMPANY RELATIONSHIPS
-- ============================================

-- Create temporary tables to map sequence numbers to UUIDs
CREATE TEMPORARY TABLE user_sequence AS
SELECT id AS user_id, (row_number() OVER ()) AS seq_num FROM users;

CREATE TEMPORARY TABLE company_sequence AS
SELECT id AS company_id, (row_number() OVER ()) AS seq_num FROM companies;

CREATE INDEX idx_user_seq ON user_sequence(seq_num);
CREATE INDEX idx_company_seq ON company_sequence(seq_num);

-- For users 1-70,000: 1 company each
DO $$
DECLARE
    batch_size INTEGER := 5000;
    total_users INTEGER := 70000;
    batches INTEGER;
    i INTEGER;
    start_seq INTEGER;
    end_seq INTEGER;
BEGIN
    batches := CEIL(total_users::FLOAT / batch_size);
    
    FOR i IN 1..batches LOOP
        start_seq := (i - 1) * batch_size + 1;
        end_seq := LEAST(i * batch_size, total_users);
        
        INSERT INTO user_companies (user_id, company_id, role)
        SELECT 
            us.user_id,
            cs.company_id,
            'member'
        FROM user_sequence us
        JOIN company_sequence cs ON cs.seq_num = ((us.seq_num * 17 + 31) % 200000) + 1
        WHERE us.seq_num BETWEEN start_seq AND end_seq;
        
        COMMIT;
        RAISE NOTICE 'Inserted 1-company relationships batch % of %', i, batches;
    END LOOP;
END $$;


-- For users 70,001-90,000: 2 companies each
DO $$
DECLARE
    batch_size INTEGER := 5000;
    total_users INTEGER := 20000;
    batches INTEGER;
    i INTEGER;
    start_seq INTEGER;
    end_seq INTEGER;
BEGIN
    batches := CEIL(total_users::FLOAT / batch_size);
    
    FOR i IN 1..batches LOOP
        start_seq := (i - 1) * batch_size + 1;
        end_seq := LEAST(i * batch_size, total_users);
        
        -- First company
        INSERT INTO user_companies (user_id, company_id, role)
        SELECT 
            us.user_id,
            cs.company_id,
            'member'
        FROM user_sequence us
        JOIN company_sequence cs ON cs.seq_num = ((us.seq_num * 17 + 31) % 200000) + 1
        WHERE us.seq_num BETWEEN start_seq AND end_seq;
        
        -- Second company (different from first)
        INSERT INTO user_companies (user_id, company_id, role)
        SELECT 
            us.user_id,
            cs.company_id,
            'member'
        FROM user_sequence us
        JOIN company_sequence cs ON cs.seq_num = (((us.seq_num * 17 + 31) + 13) % 200000) + 1
        WHERE us.seq_num BETWEEN start_seq AND end_seq
        AND (((us.seq_num * 17 + 31) % 200000) + 1) != (((us.seq_num * 17 + 31) + 13) % 200000) + 1
        ON CONFLICT (user_id, company_id) DO NOTHING;
        
        COMMIT;
        RAISE NOTICE 'Inserted 2-company relationships batch % of %', i, batches;
    END LOOP;
END $$;


-- For users 90,001-100,000: 3 companies each
DO $$
DECLARE
    batch_size INTEGER := 5000;
    total_users INTEGER := 10000;
    batches INTEGER;
    i INTEGER;
    start_seq INTEGER;
    end_seq INTEGER;
BEGIN
    batches := CEIL(total_users::FLOAT / batch_size);
    
    FOR i IN 1..batches LOOP
        start_seq := (i - 1) * batch_size + 1;
        end_seq := LEAST(i * batch_size, total_users);
        
        -- First company
        INSERT INTO user_companies (user_id, company_id, role)
        SELECT 
            us.user_id,
            cs.company_id,
            'member'
        FROM user_sequence us
        JOIN company_sequence cs ON cs.seq_num = ((us.seq_num * 17 + 31) % 200000) + 1
        WHERE us.seq_num BETWEEN start_seq AND end_seq;
        
        -- Second company
        INSERT INTO user_companies (user_id, company_id, role)
        SELECT 
            us.user_id,
            cs.company_id,
            'member'
        FROM user_sequence us
        JOIN company_sequence cs ON cs.seq_num = (((us.seq_num * 17 + 31) + 13) % 200000) + 1
        WHERE us.seq_num BETWEEN start_seq AND end_seq
        AND (((us.seq_num * 17 + 31) % 200000) + 1) != (((us.seq_num * 17 + 31) + 13) % 200000) + 1
        ON CONFLICT (user_id, company_id) DO NOTHING;
        
        -- Third company
        INSERT INTO user_companies (user_id, company_id, role)
        SELECT 
            us.user_id,
            cs.company_id,
            'member'
        FROM user_sequence us
        JOIN company_sequence cs ON cs.seq_num = (((us.seq_num * 17 + 31) + 26) % 200000) + 1
        WHERE us.seq_num BETWEEN start_seq AND end_seq
        AND (((us.seq_num * 17 + 31) % 200000) + 1) != (((us.seq_num * 17 + 31) + 26) % 200000) + 1
        AND (((us.seq_num * 17 + 31) + 13) % 200000) + 1 != (((us.seq_num * 17 + 31) + 26) % 200000) + 1
        ON CONFLICT (user_id, company_id) DO NOTHING;
        
        COMMIT;
        RAISE NOTICE 'Inserted 3-company relationships batch % of %', i, batches;
    END LOOP;
END $$;


-- ============================================
-- VERIFICATION QUERIES
-- ============================================

-- Summary statistics
SELECT 'Total Users' AS metric, COUNT(*)::text AS count FROM users
UNION ALL
SELECT 'Total Companies' AS metric, COUNT(*)::text AS count FROM companies
UNION ALL
SELECT 'Total Relationships' AS metric, COUNT(*)::text AS count FROM user_companies;

-- Distribution of users by number of companies
WITH user_company_counts AS (
    SELECT 
        user_id,
        COUNT(company_id) AS company_count
    FROM user_companies
    GROUP BY user_id
)
SELECT 
    CASE 
        WHEN company_count = 1 THEN '1 company'
        WHEN company_count = 2 THEN '2 companies'
        WHEN company_count = 3 THEN '3 companies'
        ELSE 'Other (' || company_count || ')'
    END AS category,
    COUNT(*)::text AS user_count,
    SUM(company_count)::text AS total_relationships
FROM user_company_counts
GROUP BY company_count, category
ORDER BY company_count;

-- Clean up temp tables
DROP TABLE IF EXISTS user_sequence;
DROP TABLE IF EXISTS company_sequence;
