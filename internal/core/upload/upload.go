package upload

import (
	"io/fs"
	"log"
	"path/filepath"

	"github.com/plutack/go-gofile/api"
)

type folderID = string

func createFolder(folderName string, parentFolderID string, uploadClient *api.Api) (string, error) {
	info, err := uploadClient.CreateFolder(parentFolderID, folderName)
	if err != nil {
		return "", err
	}
	return info.Data.ID, nil
}

func uploadFile(fullFilePath string, parentFolderID string, uploadClient *api.Api, server string) error {
	_, err := uploadClient.UploadFile(server, fullFilePath, parentFolderID)
	if err != nil {
		return err
	}
	return nil
}

func SendFolderToServer(folderPath string, uploadClient *api.Api, rootFolderID string, server string) error {
	err := filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			relativePath, _ := filepath.Rel(folderPath, path)
			log.Printf("Creating folder: %s\n", relativePath)

			folderID, createErr := createFolder(d.Name(), rootFolderID, uploadClient)
			if createErr != nil {
				return err
			}
			rootFolderID = folderID
		} else {
			log.Printf("Uploading file: %s\n", path)
			err = uploadFile(path, rootFolderID, uploadClient, server)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}
