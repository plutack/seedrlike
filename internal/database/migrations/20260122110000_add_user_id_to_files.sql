-- +goose Up
-- +goose StatementBegin
ALTER TABLE Files ADD COLUMN User_ID CHAR(36) NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE Files ADD FOREIGN KEY (User_ID) REFERENCES Users(ID);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE Files DROP COLUMN User_ID;
-- +goose StatementEnd
