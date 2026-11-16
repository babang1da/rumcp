-- Users of the service, one per Yandex account.
create table users (
    id uuid primary key default gen_random_uuid(),
    yandex_id text not null unique,
    login text not null default '',
    email text not null default '',
    name text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- Connected Yandex account of a user. Tokens are AES-GCM sealed by the service,
-- the plaintext token never reaches the database.
create table accounts (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references users (id) on delete cascade,
    provider text not null default 'yandex',
    access_token_enc text not null,
    refresh_token_enc text,
    scope text not null default '',
    expires_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (user_id, provider)
);

-- OAuth 2.1 clients registered dynamically by MCP clients (Claude, ChatGPT, Cursor).
create table oauth_clients (
    client_id text primary key,
    client_secret_hash text,
    client_name text not null default '',
    redirect_uris text[] not null,
    grant_types text[] not null default '{authorization_code,refresh_token}',
    response_types text[] not null default '{code}',
    token_endpoint_auth_method text not null default 'none',
    created_at timestamptz not null default now()
);

-- Authorization codes, access and refresh tokens. Only SHA-256 hashes are stored.
create table oauth_tokens (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references users (id) on delete cascade,
    client_id text not null references oauth_clients (client_id) on delete cascade,
    kind text not null check (kind in ('code', 'access', 'refresh')),
    token_hash text not null unique,
    code_challenge text,
    code_challenge_method text,
    redirect_uri text,
    scope text not null default '',
    expires_at timestamptz not null,
    consumed_at timestamptz,
    created_at timestamptz not null default now()
);

create index oauth_tokens_user_idx on oauth_tokens (user_id);

-- Every MCP tool call, including failed ones. The epic metric is counted here.
create table call_log (
    id bigserial primary key,
    user_id uuid not null references users (id) on delete cascade,
    connector text not null,
    tool text not null,
    status text not null,
    ts timestamptz not null default now()
);

create index call_log_user_ts_idx on call_log (user_id, ts desc);

-- Requests for connectors that are not released yet (Wordstat, Direct).
create table leads (
    id uuid primary key default gen_random_uuid(),
    email text not null,
    service text not null,
    user_id uuid references users (id) on delete set null,
    created_at timestamptz not null default now()
);

create index leads_email_service_idx on leads (email, service);
