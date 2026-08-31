-- 0013_role_permissions.sql
-- Per-role page access control. Admin can manage which pages each
-- role (e.g. 'user', 'admin') can access. The page key matches the
-- URL prefix of each module's routes; the Go side provides the
-- human-readable label + icon separately.
--
-- Defaults seed:
--   user  → dashboard, notes (the only two pages Hiro wants by default)
--   admin → all pages
-- The admin role's "all" is modelled as separate rows per page, so
-- toggling a single admin page is just a delete of one row.

IF OBJECT_ID('role_permissions') IS NULL
CREATE TABLE role_permissions (
    role_name NVARCHAR(32) NOT NULL,
    page_key  NVARCHAR(32) NOT NULL,
    PRIMARY KEY (role_name, page_key)
);

-- Seed user role: dashboard + notes only
IF NOT EXISTS (SELECT 1 FROM role_permissions WHERE role_name = 'user' AND page_key = 'dashboard')
    INSERT INTO role_permissions (role_name, page_key) VALUES ('user', 'dashboard');
IF NOT EXISTS (SELECT 1 FROM role_permissions WHERE role_name = 'user' AND page_key = 'notes')
    INSERT INTO role_permissions (role_name, page_key) VALUES ('user', 'notes');

-- Seed admin role: full access to all five pages
IF NOT EXISTS (SELECT 1 FROM role_permissions WHERE role_name = 'admin' AND page_key = 'dashboard')
    INSERT INTO role_permissions (role_name, page_key) VALUES ('admin', 'dashboard');
IF NOT EXISTS (SELECT 1 FROM role_permissions WHERE role_name = 'admin' AND page_key = 'licenses')
    INSERT INTO role_permissions (role_name, page_key) VALUES ('admin', 'licenses');
IF NOT EXISTS (SELECT 1 FROM role_permissions WHERE role_name = 'admin' AND page_key = 'notes')
    INSERT INTO role_permissions (role_name, page_key) VALUES ('admin', 'notes');
IF NOT EXISTS (SELECT 1 FROM role_permissions WHERE role_name = 'admin' AND page_key = 'users')
    INSERT INTO role_permissions (role_name, page_key) VALUES ('admin', 'users');
IF NOT EXISTS (SELECT 1 FROM role_permissions WHERE role_name = 'admin' AND page_key = 'audit')
    INSERT INTO role_permissions (role_name, page_key) VALUES ('admin', 'audit');

-- Rollback (if ever needed):
-- DROP TABLE role_permissions;
