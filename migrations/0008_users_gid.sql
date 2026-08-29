-- 0008: Add the GID column to users. GID is a short identifier the admin
-- types in manually (e.g. employee number). No UNIQUE constraint on purpose
-- so the placeholder 'n/a' can be used for users without a real GID.
-- Admin's GID is hard-pinned to '29384' per Hiro.
-- Column stays NULLable so the backfill below can run in one shot.

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID('users') AND name = 'gid'
)
BEGIN
    ALTER TABLE users ADD gid NVARCHAR(50) NULL;
END;

-- Backfill: admin -> 29384, everyone else -> 'n/a'. This is what the
-- handler does for new users too (default 'n/a' when the form field is
-- blank), so existing rows land in the same shape.
UPDATE users SET gid = '29384' WHERE username = 'admin' AND (gid IS NULL OR gid = '');
UPDATE users SET gid = 'n/a'  WHERE gid IS NULL OR gid = '';
