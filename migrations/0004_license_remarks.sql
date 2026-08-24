-- add remarks free-text column to licenses
ALTER TABLE licenses ADD remarks NVARCHAR(MAX) NULL;
