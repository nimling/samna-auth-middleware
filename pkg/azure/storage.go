package azure

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/labstack/echo/v4"
)

func GetBlobClient(ctx echo.Context, appID uuid.UUID, assetID uuid.UUID) (*blockblob.Client, error) {
	blobName := appID.String() + "/" + assetID.String()
	connectionString, ok := os.LookupEnv("AZURE_STORAGE_CONNECTION_STRING")
	if !ok {
		err := errors.New("Missing AZURE_STORAGE_CONNECTION_STRING in environment")
		ctx.Logger().Error(err)
		return nil, err
	}

	containerName, ok := os.LookupEnv("AZURE_STORAGE_CONTAINER_NAME")
	if !ok {
		err := errors.New("Missing AZURE_STORAGE_CONTAINER_NAME in environment")
		ctx.Logger().Error(err)
		return nil, err
	}

	serviceClient, err := azblob.NewClientFromConnectionString(connectionString, nil)
	if err != nil {
		ctx.Logger().Error(err)
		return nil, err
	}

	containerClient := serviceClient.ServiceClient().NewContainerClient(containerName)

	return containerClient.NewBlockBlobClient(blobName), nil
}

type ColdStorage struct {
	container *container.Client
}

func GetColdStorage(connectionString, containerName string) (*ColdStorage, error) {
	if containerName == "" {
		return nil, errors.New("missing cold storage container name")
	}
	serviceClient, err := azblob.NewClientFromConnectionString(connectionString, nil)
	if err != nil {
		return nil, err
	}
	return &ColdStorage{container: serviceClient.ServiceClient().NewContainerClient(containerName)}, nil
}

func (s *ColdStorage) PutObject(ctx context.Context, path string, body []byte) error {
	contentType := "application/gzip"
	_, err := s.container.NewBlockBlobClient(path).UploadBuffer(ctx, body, &blockblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: &contentType},
	})
	return err
}
