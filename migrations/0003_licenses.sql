IF OBJECT_ID('licenses') IS NULL
BEGIN
    CREATE TABLE licenses (
        id             INT IDENTITY(1,1) PRIMARY KEY,
        maker          NVARCHAR(150) NOT NULL,
        software_name  NVARCHAR(200) NOT NULL,
        version        NVARCHAR(50)  NULL,
        license_key    NVARCHAR(300) NULL,
        activation_key NVARCHAR(300) NULL,
        assigned_to    NVARCHAR(150) NULL,
        device_hostname NVARCHAR(100) NULL,
        device_sn      NVARCHAR(100) NULL,
        section        NVARCHAR(100) NULL,
        po_no          NVARCHAR(100) NULL,
        status         NVARCHAR(20)  NOT NULL CONSTRAINT DF_licenses_status DEFAULT 'Available'
                       CONSTRAINT CK_licenses_status CHECK (status IN ('In use','Available','Expired','Retired')),
        activated_on   DATE NULL,
        expiry_date    DATE NULL,
        created_at     DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at     DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME()
    );

    CREATE INDEX IX_licenses_status ON licenses(status);
    CREATE INDEX IX_licenses_expiry_date ON licenses(expiry_date);
    CREATE INDEX IX_licenses_section ON licenses(section);
    CREATE INDEX IX_licenses_software_name ON licenses(software_name);
END;
