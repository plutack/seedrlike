-- name: getTorrents :many
select * from torrents order by date_added;
