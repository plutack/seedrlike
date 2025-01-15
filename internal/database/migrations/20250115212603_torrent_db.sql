-- +goose Up
-- +goose StatementBegin
CREATE TABLE torrent (
    id INTEGER PRIMARY KEY AUTOINCREMENT
    name t
)
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd
