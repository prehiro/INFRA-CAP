-- Public notes module (Phase 4, Notes feature).
-- OneNote-style: every authenticated user can create + read + edit notes.
-- Delete: owner-only (the user who created the note). Admin does NOT get
-- implicit delete (enforced at handler level).
-- is_private: when 1, the note is visible + editable only by its owner.

IF OBJECT_ID('notes') IS NULL
BEGIN
    CREATE TABLE notes (
        id          INT IDENTITY(1,1) PRIMARY KEY,
        title       NVARCHAR(200) NOT NULL,
        content     NVARCHAR(MAX) NOT NULL,    -- markdown source
        is_private  BIT           NOT NULL DEFAULT 0,
        created_by  INT           NOT NULL FOREIGN KEY (created_by) REFERENCES users(id),
        created_at  DATETIME2     NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_by  INT           NOT NULL FOREIGN KEY (updated_by) REFERENCES users(id),
        updated_at  DATETIME2     NOT NULL DEFAULT SYSUTCDATETIME()
    );
    CREATE INDEX IX_notes_updated_at ON notes(updated_at DESC);
    CREATE INDEX IX_notes_created_by ON notes(created_by);
    CREATE INDEX IX_notes_is_private ON notes(is_private);
END;

