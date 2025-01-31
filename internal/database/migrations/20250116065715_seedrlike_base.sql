-- +goose Up

-- +goose StatementBegin
CREATE TABLE Folders(
    ID char(36) primary key,
    Name varchar(1000) NOT NULL,
    Hash char(40),
    Size BIGINT NOT NULL, 
    Parent_Folder_ID char(36),
    Date_Added datetime default current_timestamp,
    FOREIGN KEY (Parent_Folder_ID) REFERENCES Folders(ID)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE Files(
    ID CHAR(36) PRIMARY KEY, 
    Name VARCHAR(1000) NOT NULL,
    Folder_ID CHAR(36) NOT NULL, 
    Size BIGINT NOT NULL, 
    Mimetype VARCHAR(255) NOT NULL,
    MD5 CHAR(32) NOT NULL, 
    Date_Added DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (Folder_ID) REFERENCES Folders(ID)
); 
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS Files;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS Folders;
-- +goose StatementEnd
