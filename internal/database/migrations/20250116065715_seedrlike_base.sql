-- +goose Up

-- +goose StatementBegin
CREATE TABLE Folders(
    ID CHAR(36) PRIMARY KEY,
    Name VARCHAR(1000) NOT NULL,
    Hash CHAR(40),
    Size BIGINT NOT NULL, 
    Parent_Folder_ID CHAR(36) NOT NULL,
    Date_Added datetime DEFAULT CURRENT_TIMESTAMP,
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
    Server CHAR(20) NOT NULL,
    Date_Added DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (Folder_ID) REFERENCES Folders(ID)
); 
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO Folders (ID, Name, Hash, Size, Parent_Folder_ID)
VALUES ('00000000-0000-0000-0000-000000000000', 'Root', NULL, 0, '00000000-0000-0000-0000-000000000000');
-- +goose StatementEnd



-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS Files;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS Folders;
-- +goose StatementEnd
