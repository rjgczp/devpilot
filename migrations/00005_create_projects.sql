-- +goose Up
CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT projects_workspace_id_id_key UNIQUE (workspace_id,id),
    CONSTRAINT projects_name_check CHECK (
        char_length(name) BETWEEN 1 AND 100 AND octet_length(name) <= 400
        AND name = btrim(name, E' \t\r\n') AND name !~ E'[\x01-\x1f\x7f]'
    ),
    CONSTRAINT projects_description_check CHECK (
        char_length(description) <= 2000 AND octet_length(description) <= 8000
        AND description !~ E'[\x01-\x08\x0b-\x1f\x7f]'
    ),
    CONSTRAINT projects_version_check CHECK (version > 0),
    CONSTRAINT projects_time_check CHECK (updated_at >= created_at)
);
CREATE INDEX projects_created_by_user_idx ON projects(created_by_user_id);
-- +goose StatementBegin
CREATE FUNCTION projects_immutable_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id,NEW.workspace_id,NEW.created_by_user_id,NEW.created_at)
       IS DISTINCT FROM ROW(OLD.id,OLD.workspace_id,OLD.created_by_user_id,OLD.created_at) THEN
        RAISE EXCEPTION 'project identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER projects_immutable_identity BEFORE UPDATE ON projects
    FOR EACH ROW EXECUTE FUNCTION projects_immutable_identity();

-- +goose Down
DROP TABLE projects;
DROP FUNCTION projects_immutable_identity();
