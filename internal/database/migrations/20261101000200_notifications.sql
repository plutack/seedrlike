-- +goose Up
CREATE TABLE Notification_Settings (
    User_ID CHAR(36) PRIMARY KEY,
    Email VARCHAR(254) NOT NULL DEFAULT '',
    Verified_At DATETIME(6) NULL,
    Notify_Completed BOOLEAN NOT NULL DEFAULT FALSE,
    Notify_Failed BOOLEAN NOT NULL DEFAULT FALSE,
    Verification_Hash BINARY(32) NULL,
    Verification_Expires_At DATETIME(6) NULL,
    Last_Verification_At DATETIME(6) NULL,
    Last_Test_At DATETIME(6) NULL,
    CONSTRAINT fk_notification_user FOREIGN KEY (User_ID) REFERENCES Users(ID) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE Notification_Settings;
