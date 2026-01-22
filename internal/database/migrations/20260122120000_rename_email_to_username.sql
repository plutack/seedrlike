-- +goose Up
-- +goose StatementBegin
ALTER TABLE Users CHANGE COLUMN Email Username VARCHAR(255) NOT NULL UNIQUE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE Users CHANGE COLUMN Username Email VARCHAR(255) NOT NULL UNIQUE;
-- +goose StatementEnd
