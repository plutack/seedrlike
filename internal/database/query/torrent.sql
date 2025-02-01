-- name: getTorrents :many
select * from Folders WHERE Parent_Folder_ID IS NULL order by Date_Added Desc;

-- name: GetFolderContents :many
WITH folder_contents AS (
    SELECT 'folder' AS type, 
           ID, 
           Name, 
           Size, 
           CAST(Date_Added AS CHAR) as Date_Added,
           '' as Server
    FROM Folders 
    WHERE CASE 
        WHEN ? IS NOT NULL THEN Parent_Folder_ID = ?
        ELSE Parent_Folder_ID IS NULL
    END
    UNION ALL
    SELECT 'file' AS type,
           ID,
           Name,
           Size,
           CAST(Date_Added AS CHAR) as Date_Added,
           Server
    FROM Files 
    WHERE CASE 
        WHEN ? IS NOT NULL THEN Folder_ID = ?
        ELSE FALSE
    END
)
SELECT * FROM folder_contents
ORDER BY Date_Added;

-- name: CreateFolder :exec
INSERT INTO Folders (
    ID,
    Name,
    Hash,
    Size,
    Parent_Folder_ID
) VALUES (
    ?, ?, ?, ?, ?
);

-- name: CreateFile :exec
INSERT INTO Files (
    ID,
    Name,
    Folder_ID,
    Size,
    Mimetype,
    MD5,
    Server
) VALUES (
    ?, ?, ?, ?, ?, ?, ?
);

-- name: GetFolderByID :one
SELECT * FROM Folders
WHERE ID = ?;

-- name: GetFilesByFolderID :many
SELECT * FROM Files
WHERE Folder_ID = ?;

-- name: FolderExists :one
SELECT COUNT(*) > 0 FROM Folders WHERE ID = ?;
