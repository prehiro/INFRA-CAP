-- 0012_notes_accent.sql
-- Add accent_color column to notes for the customizable color stripe
-- at the top of each card.
--
-- Allowed values (kept as text rather than enum so we can extend without
-- a schema change): NULL/empty = use default, otherwise a CSS color string
-- like '#dc2626' or 'rgb(220,38,38)'. The Go model sanitises to a
-- strict whitelist before persisting, so the column is safe to be
-- NVARCHAR(20).
--
-- Rollback (if ever needed):
-- ALTER TABLE notes DROP COLUMN accent_color;

ALTER TABLE notes ADD accent_color NVARCHAR(20) NULL;
