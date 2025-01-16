-- +goose Up
-- +goose StatementBegin
CREATE TABLE torrents (
    id int auto_increment primary key,
    name varchar(1000) not null,
    hash bigint not null unique,
    size bigint not null,
    date_added datetime default current_timestamp,
    index idx_hash (hash)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE torrents
-- +goose StatementEnd
