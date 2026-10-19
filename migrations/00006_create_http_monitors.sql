-- +goose Up
CREATE TABLE http_monitors (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    workspace_id uuid NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    method text NOT NULL DEFAULT 'GET',
    interval_seconds integer NOT NULL DEFAULT 60,
    timeout_seconds integer NOT NULL DEFAULT 10,
    expected_status integer NOT NULL DEFAULT 200,
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    CONSTRAINT http_monitors_project_fkey FOREIGN KEY (workspace_id,project_id)
        REFERENCES projects(workspace_id,id) ON DELETE RESTRICT,
    CONSTRAINT http_monitors_tenant_id_key UNIQUE (workspace_id,project_id,id),
    CONSTRAINT http_monitors_name_check CHECK (
        char_length(name) BETWEEN 1 AND 100 AND octet_length(name) <= 400
        AND name = btrim(name, E' \t\r\n') AND name !~ E'[\x01-\x1f\x7f]'
    ),
    -- Deliberately a DB subset, not a URL parser or SSRF safety claim.
    CONSTRAINT http_monitors_url_check CHECK (
        octet_length(url) BETWEEN 1 AND 2048 AND url COLLATE "C" ~ '^https?://[!-~]+$'
    ),
    CONSTRAINT http_monitors_method_check CHECK (method = 'GET'),
    CONSTRAINT http_monitors_interval_check CHECK (interval_seconds IN (60,300,900,1800,3600)),
    CONSTRAINT http_monitors_timeout_check CHECK (timeout_seconds BETWEEN 1 AND 30 AND timeout_seconds < interval_seconds),
    CONSTRAINT http_monitors_status_check CHECK (expected_status BETWEEN 200 AND 599),
    CONSTRAINT http_monitors_version_check CHECK (version > 0),
    CONSTRAINT http_monitors_time_check CHECK (updated_at >= created_at)
);
CREATE INDEX http_monitors_created_by_user_idx ON http_monitors(created_by_user_id);
-- +goose StatementBegin
CREATE FUNCTION http_monitors_immutable_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id,NEW.workspace_id,NEW.project_id,NEW.created_by_user_id,NEW.created_at)
       IS DISTINCT FROM ROW(OLD.id,OLD.workspace_id,OLD.project_id,OLD.created_by_user_id,OLD.created_at) THEN
        RAISE EXCEPTION 'monitor identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER http_monitors_immutable_identity BEFORE UPDATE ON http_monitors
    FOR EACH ROW EXECUTE FUNCTION http_monitors_immutable_identity();

-- +goose Down
DROP TABLE http_monitors;
DROP FUNCTION http_monitors_immutable_identity();
