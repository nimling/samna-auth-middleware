package azure

import (
	"errors"
	"github.com/google/uuid"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
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
