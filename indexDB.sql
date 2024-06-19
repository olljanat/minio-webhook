CREATE TABLE dbo.Object (
    Bucket NVARCHAR(63) NOT NULL,
    Path NVARCHAR(255) NOT NULL,
    CreatedBy NVARCHAR(30) NULL,
	CreatedFrom NVARCHAR(30) NULL,
	Created DATETIME NULL,
    ModifiedBy NVARCHAR(30) NULL,
	ModifiedFrom NVARCHAR(30) NULL,
	Modified DATETIME NULL
)
GO

CREATE PROCEDURE dbo.SaveObject (
    @Bucket NVARCHAR(63),
    @Path NVARCHAR(255),
    @AccessKeyID NVARCHAR(30),
    @RemoteHost NVARCHAR(30)
) AS
BEGIN
    SET NOCOUNT ON;
    MERGE dbo.Object T
    USING (SELECT @Bucket AS Bucket, @Path AS Path, @AccessKeyID AS AccessKeyID, @RemoteHost AS RemoteHost) AS S
        ON T.Bucket = S.Bucket
        AND T.Path = S.Path
    WHEN MATCHED THEN
        UPDATE SET T.ModifiedBy = S.AccessKeyID,
                   ModifiedFrom = S.RemoteHost,
                   Modified = GETDATE()
    WHEN NOT MATCHED BY TARGET THEN
        INSERT (Bucket, Path, CreatedBy, CreatedFrom, Created, ModifiedBy, ModifiedFrom, Modified)
        VALUES (S.Bucket, S.Path, S.AccessKeyID, S.RemoteHost, GETDATE(), S.AccessKeyID, S.RemoteHost, GETDATE());
END
GO

CREATE PROCEDURE dbo.DeleteObject (@Bucket NVARCHAR(63), @Path NVARCHAR(255)) AS
	DELETE dbo.Object  WHERE Bucket = @Bucket AND Path = @Path
GO

CREATE UNIQUE INDEX IDX_Object_Bucket_Path
ON dbo.Object (Bucket, Path);
GO
