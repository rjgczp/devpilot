-- +goose Up
-- Goose's transaction holds this lock through backfill and trigger installation.
LOCK TABLE http_monitors IN SHARE ROW EXCLUSIVE MODE;
CREATE TABLE monitor_activation_states (
    workspace_id uuid NOT NULL,
    project_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    desired_state text NOT NULL DEFAULT 'disabled',
    activation_configuration_version bigint,
    activation_generation bigint NOT NULL DEFAULT 1 CHECK (activation_generation > 0),
    next_due_at timestamptz CHECK (next_due_at IS NULL),
    next_enable_allowed_at timestamptz,
    last_transition_kind text NOT NULL DEFAULT 'never_activated',
    last_actor_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    PRIMARY KEY (workspace_id,project_id,monitor_id),
    FOREIGN KEY (workspace_id,project_id,monitor_id)
        REFERENCES http_monitors(workspace_id,project_id,id) ON DELETE RESTRICT,
    CHECK (updated_at >= created_at),
    CHECK (activation_configuration_version IS NULL OR activation_configuration_version > 0),
    CHECK (
      (desired_state='enabled' AND activation_configuration_version IS NOT NULL
       AND next_enable_allowed_at IS NOT NULL AND last_actor_user_id IS NOT NULL
       AND last_transition_kind='owner_enabled')
      OR (desired_state='disabled' AND activation_configuration_version IS NULL AND (
        (last_transition_kind='owner_paused' AND last_actor_user_id IS NOT NULL)
        OR (last_transition_kind IN ('never_activated','configuration_changed') AND last_actor_user_id IS NULL)
      ))
    )
);
CREATE INDEX monitor_activation_enabled_idx ON monitor_activation_states(workspace_id,monitor_id)
    WHERE desired_state='enabled';
CREATE INDEX monitor_activation_actor_idx ON monitor_activation_states(last_actor_user_id)
    WHERE last_actor_user_id IS NOT NULL;
INSERT INTO monitor_activation_states(workspace_id,project_id,monitor_id)
    SELECT workspace_id,project_id,id FROM http_monitors;

-- +goose StatementBegin
CREATE FUNCTION monitor_activation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP IN ('DELETE','TRUNCATE') THEN
        RAISE EXCEPTION 'activation state cannot be removed' USING ERRCODE='23514';
    END IF;
    IF ROW(NEW.workspace_id,NEW.project_id,NEW.monitor_id,NEW.created_at)
       IS DISTINCT FROM ROW(OLD.workspace_id,OLD.project_id,OLD.monitor_id,OLD.created_at)
       OR NEW.updated_at < OLD.updated_at
       OR (OLD.next_enable_allowed_at IS NOT NULL AND
           (NEW.next_enable_allowed_at IS NULL OR NEW.next_enable_allowed_at < OLD.next_enable_allowed_at)) THEN
        RAISE EXCEPTION 'activation identity time or cooling is immutable' USING ERRCODE='23514';
    END IF;
    IF NEW IS DISTINCT FROM OLD AND NEW.activation_generation IS DISTINCT FROM OLD.activation_generation+1 THEN
        RAISE EXCEPTION 'activation generation must advance once' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER monitor_activation_guard BEFORE UPDATE OR DELETE ON monitor_activation_states
    FOR EACH ROW EXECUTE FUNCTION monitor_activation_guard();
CREATE TRIGGER monitor_activation_no_truncate BEFORE TRUNCATE ON monitor_activation_states
    FOR EACH STATEMENT EXECUTE FUNCTION monitor_activation_guard();

-- +goose StatementBegin
CREATE FUNCTION monitor_activation_initialize() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO monitor_activation_states(workspace_id,project_id,monitor_id)
        VALUES(NEW.workspace_id,NEW.project_id,NEW.id);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER monitor_activation_initialize AFTER INSERT ON http_monitors
    FOR EACH ROW EXECUTE FUNCTION monitor_activation_initialize();

-- +goose StatementBegin
CREATE FUNCTION monitor_configuration_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.version IS DISTINCT FROM OLD.version THEN
        IF NEW.version IS DISTINCT FROM OLD.version+1 THEN
            RAISE EXCEPTION 'configuration version must advance once' USING ERRCODE='23514';
        END IF;
    ELSIF ROW(NEW.name,NEW.url,NEW.method,NEW.interval_seconds,NEW.timeout_seconds,NEW.expected_status)
          IS DISTINCT FROM ROW(OLD.name,OLD.url,OLD.method,OLD.interval_seconds,OLD.timeout_seconds,OLD.expected_status) THEN
        RAISE EXCEPTION 'configuration changes require a revision' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER monitor_configuration_revision_guard BEFORE UPDATE ON http_monitors
    FOR EACH ROW EXECUTE FUNCTION monitor_configuration_revision_guard();

-- +goose StatementBegin
CREATE FUNCTION monitor_activation_invalidate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.version IS DISTINCT FROM OLD.version THEN
        UPDATE monitor_activation_states SET desired_state='disabled',activation_configuration_version=NULL,
            activation_generation=activation_generation+1,last_transition_kind='configuration_changed',
            last_actor_user_id=NULL,updated_at=GREATEST(updated_at,statement_timestamp())
            WHERE workspace_id=NEW.workspace_id AND project_id=NEW.project_id AND monitor_id=NEW.id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'activation state is missing' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER monitor_activation_invalidate AFTER UPDATE ON http_monitors
    FOR EACH ROW EXECUTE FUNCTION monitor_activation_invalidate();

-- +goose Down
DROP TRIGGER monitor_activation_invalidate ON http_monitors;
DROP TRIGGER monitor_configuration_revision_guard ON http_monitors;
DROP TRIGGER monitor_activation_initialize ON http_monitors;
DROP TABLE monitor_activation_states;
DROP FUNCTION monitor_activation_invalidate();
DROP FUNCTION monitor_configuration_revision_guard();
DROP FUNCTION monitor_activation_initialize();
DROP FUNCTION monitor_activation_guard();
