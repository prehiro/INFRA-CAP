-- 0014_user_page_access.sql
-- Per-user page access overrides. A user with a row in this table
-- uses the row's granted value for that page; users with no rows
-- inherit from their role's role_permissions entry.
--
-- Model: additive override. Rows where granted=1 ADD access on top
-- of the role default. Rows where granted=0 are reserved for
-- future denial support; for now the page handler ignores them
-- (we don't expose a "deny" UI yet). The granted column is here
-- so we don't need a migration to add denial later.
--
-- Example: a 'user' role with role_permissions = {dashboard, notes}.
-- Insert (user_id=42, page_key='licenses', granted=1) → user 42
-- can additionally access /licenses, in addition to dashboard
-- and notes from the role default.

IF OBJECT_ID('user_page_access', 'U') IS NULL
BEGIN
    CREATE TABLE user_page_access (
        id          INT IDENTITY(1,1) PRIMARY KEY,
        user_id     INT NOT NULL,
        page_key    NVARCHAR(50) NOT NULL,
        granted     BIT NOT NULL DEFAULT 1,
        created_at  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT uq_user_page UNIQUE (user_id, page_key),
        CONSTRAINT fk_upa_user FOREIGN KEY (user_id)
            REFERENCES users(id) ON DELETE CASCADE
    );
    CREATE INDEX ix_upa_user ON user_page_access (user_id);
END;
