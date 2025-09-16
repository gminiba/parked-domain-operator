package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	parkingv1alpha1 "github.com/gminiba/parked-domain-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileS3Bucket ensures the S3 bucket is correctly configured and returns its website endpoint.
func (r *ParkedDomainReconciler) reconcileS3Bucket(ctx context.Context, pd *parkingv1alpha1.ParkedDomain) (string, error) {
	logger := log.FromContext(ctx)
	bucketName := pd.Spec.DomainName

	// **MODIFICATION**: Default region is now eu-central-1
	region := pd.Spec.Region
	if region == "" {
		region = "eu-central-1"
	}

	logger = logger.WithValues("region", region)

	// 1. Check if bucket exists
	_, err := r.S3Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})
	if err != nil {
		var nfe *s3types.NotFound
		if errors.As(err, &nfe) {
			logger.Info("S3 bucket not found, creating it")

			createBucketInput := &s3.CreateBucketInput{
				Bucket: aws.String(bucketName),
				CreateBucketConfiguration: &s3types.CreateBucketConfiguration{
					LocationConstraint: s3types.BucketLocationConstraint(region),
				},
			}
			// Note: The us-east-1 region is a special case and does not use LocationConstraint.
			if region == "us-east-1" {
				createBucketInput.CreateBucketConfiguration = nil
			}

			if _, createErr := r.S3Client.CreateBucket(ctx, createBucketInput); createErr != nil {
				return "", fmt.Errorf("failed to create S3 bucket: %w", createErr)
			}
		} else {
			return "", fmt.Errorf("failed to check S3 bucket existence: %w", err)
		}
	}

	// 2. Upload the parking page content
	cmName := os.Getenv("TEMPLATE_CONFIGMAP_NAME")
	if cmName == "" {
		return "", errors.New("TEMPLATE_CONFIGMAP_NAME environment variable must be set")
	}

	// Get the namespace for the ConfigMap
	cmNamespace := os.Getenv("TEMPLATE_CONFIGMAP_NAMESPACE")
	if cmNamespace == "" {
		// If the env var is not set, default to the ParkedDomain's own namespace.
		cmNamespace = pd.Namespace
		logger.Info("TEMPLATE_CONFIGMAP_NAMESPACE not set, defaulting to ParkedDomain's namespace", "Namespace", cmNamespace)
	}

	templateName := pd.Spec.TemplateName
	if templateName == "" {
		templateName = "default.html" // Default template key in the ConfigMap
	}

	logger.Info("Using template from ConfigMap", "ConfigMap", cmName, "Namespace", cmNamespace, "TemplateKey", templateName)

	// 1. Get the template from the ConfigMap
	templateCM := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cmNamespace}, templateCM)
	if err != nil {
		return "", fmt.Errorf("failed to get template ConfigMap '%s' in namespace '%s': %w", cmName, cmNamespace, err)
	}

	templateContent, ok := templateCM.Data[templateName]
	if !ok {
		return "", fmt.Errorf("template key '%s' not found in ConfigMap '%s'", templateName, cmName)
	}

	// 2. Replace placeholder with the actual domain name
	finalContent := strings.ReplaceAll(templateContent, "{{DOMAIN_NAME}}", pd.Spec.DomainName)

	// 3. Upload the modified content to the destination bucket
	_, err = r.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String("index.html"),
		Body:        bytes.NewReader([]byte(finalContent)),
		ContentType: aws.String("text/html"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload final index.html: %w", err)
	}

	// 3. Enable static website hosting
	_, err = r.S3Client.PutBucketWebsite(ctx, &s3.PutBucketWebsiteInput{
		Bucket: aws.String(bucketName),
		WebsiteConfiguration: &s3types.WebsiteConfiguration{
			IndexDocument: &s3types.IndexDocument{Suffix: aws.String("index.html")},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to enable S3 static website hosting: %w", err)
	}

	// 4. Apply a public-read bucket policy
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Sid":"PublicReadGetObject","Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/*"}]}`, bucketName)
	_, err = r.S3Client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucketName),
		Policy: aws.String(policy),
	})
	if err != nil {
		return "", fmt.Errorf("failed to apply S3 bucket policy: %w", err)
	}

	logger.Info("Successfully reconciled S3 bucket", "BucketName", bucketName)
	// Construct the S3 website endpoint URL
	s3Endpoint := fmt.Sprintf("%s.s3-website.%s.amazonaws.com", bucketName, region)

	logger.Info("Successfully reconciled S3 bucket", "BucketName", bucketName, "Endpoint", s3Endpoint)
	return s3Endpoint, nil
}

// cleanupS3Bucket empties and deletes the S3 bucket.
func (r *ParkedDomainReconciler) cleanupS3Bucket(ctx context.Context, pd *parkingv1alpha1.ParkedDomain) error {
	logger := log.FromContext(ctx)
	bucketName := pd.Spec.DomainName

	logger.Info("Starting S3 bucket cleanup", "BucketName", bucketName)

	paginator := s3.NewListObjectsV2Paginator(r.S3Client, &s3.ListObjectsV2Input{Bucket: aws.String(bucketName)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list objects in S3 bucket for deletion: %w", err)
		}
		if len(page.Contents) > 0 {
			var objectsToDelete []s3types.ObjectIdentifier
			for _, obj := range page.Contents {
				objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{Key: obj.Key})
			}
			_, err := r.S3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucketName),
				Delete: &s3types.Delete{Objects: objectsToDelete},
			})
			if err != nil {
				return fmt.Errorf("failed to delete objects from S3 bucket: %w", err)
			}
		}
	}

	_, err := r.S3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucketName)})
	if err != nil {
		var nfe *s3types.NotFound
		if !errors.As(err, &nfe) {
			return fmt.Errorf("failed to delete S3 bucket: %w", err)
		}
	}

	logger.Info("S3 Bucket cleanup complete", "BucketName", bucketName)
	return nil
}
