-- Historical attribution never replaces the true operator record.
CREATE TABLE migration_operations (
 id uuid PRIMARY KEY,
 operator_id uuid NOT NULL REFERENCES accounts(id),
 session_id uuid NOT NULL,
 acting_as uuid NOT NULL REFERENCES accounts(id),
 operation text NOT NULL CHECK(operation IN ('create','edit')),
 kind text NOT NULL,
 target_id uuid NOT NULL,
 before_data jsonb,
 after_data jsonb NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX migration_operations_target ON migration_operations(kind,target_id,recorded_at);
CREATE FUNCTION immutable_migration_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Migration operator records are immutable'; END;
$$;
CREATE TRIGGER migration_operations_immutable BEFORE UPDATE OR DELETE ON migration_operations FOR EACH ROW EXECUTE FUNCTION immutable_migration_operation();
