-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    email text COLLATE "C" NOT NULL,
    password_hash text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    email_verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT users_email_canonical CHECK (
        octet_length(email) BETWEEN 1 AND 254
        AND octet_length(email) = char_length(email)
        AND email = lower(email)
        AND email = btrim(email, E' \t\r\n')
    ),
    CONSTRAINT users_password_hash_length CHECK (octet_length(password_hash) BETWEEN 1 AND 1024),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled'))
);

-- +goose Down
DROP TABLE users;
