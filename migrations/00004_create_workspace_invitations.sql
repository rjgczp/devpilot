-- +goose Up
-- Volatile default is evaluated per existing row, not once for the table.
ALTER TABLE workspace_members ADD COLUMN membership_id uuid NOT NULL DEFAULT uuidv7();
ALTER TABLE workspace_members ADD CONSTRAINT workspace_members_membership_id_key UNIQUE (membership_id);
CREATE TABLE workspace_invitations (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    invitee_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (statement_timestamp() + interval '168 hours'),
    CONSTRAINT workspace_invitations_status_check CHECK (status IN ('pending','accepted','declined','revoked','expired')),
    CONSTRAINT workspace_invitations_not_self CHECK (invitee_user_id <> created_by_user_id),
    CONSTRAINT workspace_invitations_time_check CHECK (expires_at > created_at AND updated_at >= created_at AND updated_at <= expires_at)
);
CREATE UNIQUE INDEX workspace_invitations_pending_key ON workspace_invitations(workspace_id,invitee_user_id) WHERE status='pending';
CREATE INDEX workspace_invitations_workspace_idx ON workspace_invitations(workspace_id,id);
CREATE INDEX workspace_invitations_recipient_idx ON workspace_invitations(invitee_user_id,id);
CREATE INDEX workspace_invitations_workspace_pending_idx ON workspace_invitations(workspace_id,id) INCLUDE(expires_at) WHERE status='pending';
CREATE INDEX workspace_invitations_recipient_pending_idx ON workspace_invitations(invitee_user_id,id) INCLUDE(expires_at) WHERE status='pending';
CREATE INDEX workspace_invitations_expiry_idx ON workspace_invitations(workspace_id,expires_at,id) WHERE status='pending';
CREATE INDEX workspace_invitations_creator_idx ON workspace_invitations(created_by_user_id);

-- +goose Down
DROP TABLE workspace_invitations;
ALTER TABLE workspace_members DROP COLUMN membership_id;
