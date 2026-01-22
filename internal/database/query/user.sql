-- name: CreateUser :exec
INSERT INTO Users (ID, Username, Password_Hash) VALUES (?, ?, ?);

-- name: GetUserByUsername :one
SELECT * FROM Users WHERE Username = ?;

-- name: GetUserByID :one
SELECT * FROM Users WHERE ID = ?;
