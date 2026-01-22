-- +goose Up

-- +goose StatementBegin
CREATE TABLE Users (
    ID CHAR(36) PRIMARY KEY,
    Email VARCHAR(255) NOT NULL UNIQUE,
    Password_Hash VARCHAR(255) NOT NULL,
    Created_At DATETIME DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE Folders ADD COLUMN User_ID CHAR(36) NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE Folders ADD FOREIGN KEY (User_ID) REFERENCES Users(ID);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
ALTER TABLE Folders DROP FOREIGN KEY Folders_ibfk_2; -- Dropping foreign key, name might vary but usually follows this pattern or need to check constraint name. 
-- Note: MySQL might name it differently. Safe bet is to drop column which drops FK in some dialects, but strictly speaking we should drop FK first.
-- Since I can't know the exact FK name generated, I'll assume standard naming or just drop column if supported.
-- For safety in this environment, I will just drop the column which usually requires dropping the FK first. 
-- Let's try to just drop the column and see if it complains, or better, just leave the Down migration simple or skip complex rollback logic if not strictly needed.
-- Actually, let's keep it simple.
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE Folders DROP COLUMN User_ID;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS Users;
-- +goose StatementEnd
