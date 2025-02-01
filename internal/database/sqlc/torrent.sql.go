// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: torrent.sql

package database

import (
	"context"
	"database/sql"
)

const createFile = `-- name: CreateFile :exec
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
)
`

type CreateFileParams struct {
	ID       string
	Name     string
	FolderID string
	Size     int64
	Mimetype string
	Md5      string
	Server   string
}

func (q *Queries) CreateFile(ctx context.Context, arg CreateFileParams) error {
	_, err := q.db.ExecContext(ctx, createFile,
		arg.ID,
		arg.Name,
		arg.FolderID,
		arg.Size,
		arg.Mimetype,
		arg.Md5,
		arg.Server,
	)
	return err
}

const createFolder = `-- name: CreateFolder :exec
INSERT INTO Folders (
    ID,
    Name,
    Hash,
    Size,
    Parent_Folder_ID
) VALUES (
    ?, ?, ?, ?, ?
)
`

type CreateFolderParams struct {
	ID             string
	Name           string
	Hash           sql.NullString
	Size           int64
	ParentFolderID sql.NullString
}

func (q *Queries) CreateFolder(ctx context.Context, arg CreateFolderParams) error {
	_, err := q.db.ExecContext(ctx, createFolder,
		arg.ID,
		arg.Name,
		arg.Hash,
		arg.Size,
		arg.ParentFolderID,
	)
	return err
}

const folderExists = `-- name: FolderExists :one
SELECT COUNT(*) > 0 FROM Folders WHERE ID = ?
`

func (q *Queries) FolderExists(ctx context.Context, id string) (bool, error) {
	row := q.db.QueryRowContext(ctx, folderExists, id)
	var column_1 bool
	err := row.Scan(&column_1)
	return column_1, err
}

const getFilesByFolderID = `-- name: GetFilesByFolderID :many
SELECT id, name, folder_id, size, mimetype, md5, server, date_added FROM Files
WHERE Folder_ID = ?
`

func (q *Queries) GetFilesByFolderID(ctx context.Context, folderID string) ([]File, error) {
	rows, err := q.db.QueryContext(ctx, getFilesByFolderID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []File
	for rows.Next() {
		var i File
		if err := rows.Scan(
			&i.ID,
			&i.Name,
			&i.FolderID,
			&i.Size,
			&i.Mimetype,
			&i.Md5,
			&i.Server,
			&i.DateAdded,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getFolderByID = `-- name: GetFolderByID :one
SELECT id, name, hash, size, parent_folder_id, date_added FROM Folders
WHERE ID = ?
`

func (q *Queries) GetFolderByID(ctx context.Context, id string) (Folder, error) {
	row := q.db.QueryRowContext(ctx, getFolderByID, id)
	var i Folder
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.Hash,
		&i.Size,
		&i.ParentFolderID,
		&i.DateAdded,
	)
	return i, err
}

const getFolderContents = `-- name: GetFolderContents :many
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
SELECT type, id, name, size, date_added, server FROM folder_contents
ORDER BY Date_Added
`

type GetFolderContentsParams struct {
	Column1        interface{}
	ParentFolderID sql.NullString
	Column3        interface{}
	FolderID       string
}

type GetFolderContentsRow struct {
	Type      string
	ID        string
	Name      string
	Size      int64
	DateAdded interface{}
	Server    string
}

func (q *Queries) GetFolderContents(ctx context.Context, arg GetFolderContentsParams) ([]GetFolderContentsRow, error) {
	rows, err := q.db.QueryContext(ctx, getFolderContents,
		arg.Column1,
		arg.ParentFolderID,
		arg.Column3,
		arg.FolderID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetFolderContentsRow
	for rows.Next() {
		var i GetFolderContentsRow
		if err := rows.Scan(
			&i.Type,
			&i.ID,
			&i.Name,
			&i.Size,
			&i.DateAdded,
			&i.Server,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getTorrents = `-- name: getTorrents :many
select id, name, hash, size, parent_folder_id, date_added from Folders WHERE Parent_Folder_ID IS NULL order by Date_Added Desc
`

func (q *Queries) getTorrents(ctx context.Context) ([]Folder, error) {
	rows, err := q.db.QueryContext(ctx, getTorrents)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Folder
	for rows.Next() {
		var i Folder
		if err := rows.Scan(
			&i.ID,
			&i.Name,
			&i.Hash,
			&i.Size,
			&i.ParentFolderID,
			&i.DateAdded,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
