-- Split the legacy 'login' action into 'login_success' and 'login_failure'
-- so the audit badge and detail modal can color-code them green / red.
--
-- Order matters: the CHECK constraint must be widened BEFORE the UPDATE
-- statements, otherwise the new action values would violate the existing
-- constraint. The runner applies the whole file in a single transaction,
-- so this ordering is safe.

-- 1. Widen the CHECK constraint to allow the new actions.
IF EXISTS (SELECT 1 FROM sys.check_constraints WHERE name = 'CK_audit_log_action')
BEGIN
    ALTER TABLE audit_log DROP CONSTRAINT CK_audit_log_action;
    ALTER TABLE audit_log ADD CONSTRAINT CK_audit_log_action
        CHECK (action IN ('create','update','retired','delete',
                          'login','login_success','login_failure',
                          'logout','export'));
END
ELSE
BEGIN
    IF OBJECT_ID('audit_log') IS NOT NULL
        ALTER TABLE audit_log ADD CONSTRAINT CK_audit_log_action
            CHECK (action IN ('create','update','retired','delete',
                              'login','login_success','login_failure',
                              'logout','export'));
END;

-- 2. Backfill: rows with a failure marker in the JSON payload become login_failure.
IF EXISTS (SELECT 1 FROM sys.tables WHERE name = 'audit_log')
BEGIN
    UPDATE audit_log
       SET action = 'login_failure'
     WHERE action = 'login'
       AND (
           changes LIKE '%"result":"failure"%'
           OR changes LIKE '%"result": "failure"%'
           OR changes LIKE '%failure%'
       );

    -- 3. Anything still 'login' is a success.
    UPDATE audit_log
       SET action = 'login_success'
     WHERE action = 'login';
END;
