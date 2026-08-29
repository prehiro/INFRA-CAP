-- 0010: Make sure the gid column is wide enough (NVARCHAR(50)) to hold
-- arbitrary identifiers the admin types in. Older installs from before
-- 0008 was finalised may have a NVARCHAR(7) column; this widens it to
-- 50 chars while staying NULLable (so a re-run is harmless).
IF EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID('users') AND name = 'gid'
      AND max_length < 100  -- NVARCHAR(50) = 100 bytes
)
BEGIN
    ALTER TABLE users ALTER COLUMN gid NVARCHAR(50) NULL;
END;
