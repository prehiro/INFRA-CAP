IF OBJECT_ID('users') IS NULL
BEGIN
    CREATE TABLE users (
        id            INT IDENTITY(1,1) PRIMARY KEY,
        username      NVARCHAR(100) NOT NULL UNIQUE,
        password_hash NVARCHAR(200) NOT NULL,
        full_name     NVARCHAR(150) NOT NULL,
        role          NVARCHAR(20)  NOT NULL CONSTRAINT CK_users_role CHECK (role IN ('admin','user')),
        is_active     BIT           NOT NULL DEFAULT 1,
        created_at    DATETIME2     NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at    DATETIME2     NOT NULL DEFAULT SYSUTCDATETIME()
    );
END;

IF OBJECT_ID('sessions') IS NULL
BEGIN
    CREATE TABLE sessions (
        token      NVARCHAR(128) PRIMARY KEY,
        user_id    INT           NOT NULL FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
        expires_at DATETIME2     NOT NULL,
        created_at DATETIME2     NOT NULL DEFAULT SYSUTCDATETIME()
    );
    CREATE INDEX IX_sessions_user_id ON sessions(user_id);
    CREATE INDEX IX_sessions_expires_at ON sessions(expires_at);
END;
