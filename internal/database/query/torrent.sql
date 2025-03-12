-- name: GetFolderContents :many
WITH RECURSIVE folder_contents AS (
    SELECT 'folder' AS type, 
           ID, 
           Name, 
           Size, 
           DATE_FORMAT(Date_Added, '%Y-%m-%d %H:%i:%s') as Date_Added,
           '' as Server
    FROM Folders 
    WHERE Parent_Folder_ID = ?
    AND ID != '00000000-0000-0000-0000-000000000000'
    UNION ALL
    SELECT 'file' AS type,
           ID,
           Name,
           Size,
           DATE_FORMAT(Date_Added, '%Y-%m-%d %H:%i:%s') as Date_Added,
           Server
    FROM Files 
    WHERE Folder_ID = ?
)
SELECT type, ID, Name, Size, Date_Added, Server 
FROM folder_contents
ORDER BY Date_Added DESC;

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

DELETE FROM Folders
WHERE Date_Added < NOW() - INTERVAL 7 DAY;

-- name: GetFoldersToDelete :many
WITH RECURSIVE to_delete AS (
    SELECT f.ID FROM Folders f WHERE f.ID = ? 
    UNION ALL
    SELECT f.ID 
    FROM Folders f
    INNER JOIN to_delete td ON f.Parent_Folder_ID = td.ID
)
SELECT ID FROM to_delete;

-- name: DeleteFilesByFolderIDs :exec
DELETE FROM Files WHERE Folder_ID = ?;

-- name: DeleteFolderByID :exec
DELETE FROM Folders WHERE ID = ?;

-- name: DeleteFileByID :exec
DELETE FROM Files WHERE ID = ?;

-- name: GetOldFiles :many
SELECT ID FROM Files WHERE Date_Added < NOW() - INTERVAL 7 DAY;

-- name: GetOldFolders :many
SELECT ID FROM Folders 
WHERE Date_Added < NOW() - INTERVAL 7 DAY
AND ID != '00000000-0000-0000-0000-000000000000';  -- Ensure root folder is never included

-- name: DeleteOldContent :exec
DELETE FROM Files 
WHERE Folder_ID IN (SELECT ID FROM Folders WHERE Date_Added < NOW() - INTERVAL 7 DAY);

DELETE FROM Files 
WHERE Date_Added < NOW() - INTERVAL 7 DAY;

DELETE FROM Folders
WHERE Date_Added < NOW() - INTERVAL 7 DAY
AND ID != '00000000-0000-0000-0000-000000000000';  -- Protect root folder
