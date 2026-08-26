-- Allow the 'retired' action in the existing audit_log CHECK constraint (Fase 2b hardening).
IF EXISTS (SELECT 1 FROM sys.check_constraints WHERE name = 'CK_audit_log_action')
BEGIN
    ALTER TABLE audit_log DROP CONSTRAINT CK_audit_log_action;
    ALTER TABLE audit_log ADD CONSTRAINT CK_audit_log_action
        CHECK (action IN ('create','update','retired','delete','login','logout','export'));
END
ELSE
BEGIN
    -- Table created by 0005 already includes 'retired'; nothing to do. But ensure the
    -- constraint exists for safety on fresh installs that skipped the dropped version.
    IF OBJECT_ID('audit_log') IS NOT NULL
        ALTER TABLE audit_log ADD CONSTRAINT CK_audit_log_action
            CHECK (action IN ('create','update','retired','delete','login','logout','export'));
END
