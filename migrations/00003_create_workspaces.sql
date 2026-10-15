-- +goose Up
CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    CONSTRAINT workspaces_name_check CHECK (
        char_length(name) BETWEEN 1 AND 100
        AND octet_length(name) <= 400
        AND name = btrim(name, E' \t\r\n')
        AND name !~ E'[\x01-\x1f\x7f]'
    )
);
CREATE TABLE workspace_members (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    CONSTRAINT workspace_members_pkey PRIMARY KEY (workspace_id, user_id),
    CONSTRAINT workspace_members_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    CONSTRAINT workspace_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT workspace_members_role_check CHECK (role IN ('owner', 'member'))
);
CREATE INDEX workspace_members_user_workspace_idx ON workspace_members(user_id, workspace_id);

-- +goose Down
DROP TABLE workspace_members;
DROP TABLE workspaces;
