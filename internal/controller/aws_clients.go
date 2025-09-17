package controller

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// AWSS3ClientFactory creates real AWS S3 clients.
type AWSS3ClientFactory struct{}

func (f *AWSS3ClientFactory) GetClient(ctx context.Context, region string) (S3ClientAPI, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for region %s: %w", region, err)
	}
	return s3.NewFromConfig(cfg), nil
}
