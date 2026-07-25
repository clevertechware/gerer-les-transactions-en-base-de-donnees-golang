CREATE TABLE IF NOT EXISTS companies (
    id uuid not null primary key default uuidv7(),

    name varchar(255) not null,
    address varchar(255),

    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    deleted_at timestamptz
);

CREATE TABLE IF NOT EXISTS users (
    id uuid not null primary key default uuidv7(),
    first_name varchar(255) not null,
    last_name varchar(255) not null,
    email varchar(255) not null,
    username varchar(255) not null,

    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    deleted_at timestamptz
);

CREATE TABLE IF NOT EXISTS user_companies (
    user_id uuid not null,
    company_id uuid not null,

    role varchar(255),

    constraint company_user_pk primary key (user_id, company_id)
);