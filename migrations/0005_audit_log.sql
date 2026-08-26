-- Audit trail: cross-module activity log.
IF OBJECT_ID('audit_log') IS NULL
BEGIN
    CREATE TABLE audit_log (
        id          INT IDENTITY(1,1) PRIMARY KEY,
        actor_id    INT NULL,
        actor_name  NVARCHAR(150) NULL,
        action      NVARCHAR(20) NOT NULL
                    CONSTRAINT CK_audit_log_action CHECK (action IN ('create','update','delete','login','logout','export')),
        entity      NVARCHAR(50) NOT NULL,
        entity_id   NVARCHAR(50) NULL,
        changes     NVARCHAR(MAX) NULL,
        ip          NVARCHAR(45) NULL,
        created_at  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME()
    );

    CREATE INDEX IX_audit_log_created_at ON audit_log(created_at DESC);
    CREATE INDEX IX_audit_log_entity ON audit_log(entity, entity_id);
    CREATE INDEX IX_audit_log_action ON audit_log(action);
END;
