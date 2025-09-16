package controller

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	parkingv1alpha1 "github.com/gminiba/parked-domain-operator/api/v1alpha1"
)

// --- Mock AWS Client Implementations ---

// MockS3Client simulates the S3 client for tests.
type MockS3Client struct {
	HeadBucketFunc   func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	DeleteBucketFunc func(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error)
	// Add other functions as needed, returning nil or empty structs
}

func (m *MockS3Client) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.HeadBucketFunc != nil {
		return m.HeadBucketFunc(ctx, params, optFns...)
	}
	// Default behavior: Simulate bucket not found to trigger creation
	return nil, &s3types.NotFound{}
}

func (m *MockS3Client) CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, nil
}
func (m *MockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}
func (m *MockS3Client) PutBucketWebsite(ctx context.Context, params *s3.PutBucketWebsiteInput, optFns ...func(*s3.Options)) (*s3.PutBucketWebsiteOutput, error) {
	return &s3.PutBucketWebsiteOutput{}, nil
}
func (m *MockS3Client) PutBucketPolicy(ctx context.Context, params *s3.PutBucketPolicyInput, optFns ...func(*s3.Options)) (*s3.PutBucketPolicyOutput, error) {
	return &s3.PutBucketPolicyOutput{}, nil
}
func (m *MockS3Client) DeleteBucket(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	if m.DeleteBucketFunc != nil {
		return m.DeleteBucketFunc(ctx, params, optFns...)
	}
	return &s3.DeleteBucketOutput{}, nil
}
func (m *MockS3Client) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &s3.ListObjectsV2Output{Contents: []s3types.Object{}}, nil
}
func (m *MockS3Client) DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return &s3.DeleteObjectsOutput{}, nil
}

// MockR53Client simulates the Route53 client for tests.
type MockR53Client struct {
	CreateHostedZoneFunc func(ctx context.Context, params *route53.CreateHostedZoneInput, optFns ...func(*route53.Options)) (*route53.CreateHostedZoneOutput, error)
	// Add other functions as needed
}

func (m *MockR53Client) CreateHostedZone(ctx context.Context, params *route53.CreateHostedZoneInput, optFns ...func(*route53.Options)) (*route53.CreateHostedZoneOutput, error) {
	if m.CreateHostedZoneFunc != nil {
		return m.CreateHostedZoneFunc(ctx, params, optFns...)
	}
	// Default behavior: Simulate a successful API call
	return &route53.CreateHostedZoneOutput{
		HostedZone:    &r53types.HostedZone{Id: aws.String("/hostedzone/MOCKZONEID123")},
		DelegationSet: &r53types.DelegationSet{NameServers: []string{"ns-1.awsdns.com", "ns-2.awsdns.com"}},
	}, nil
}
func (m *MockR53Client) ChangeResourceRecordSets(ctx context.Context, params *route53.ChangeResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error) {
	return &route53.ChangeResourceRecordSetsOutput{}, nil
}
func (m *MockR53Client) ListResourceRecordSets(ctx context.Context, params *route53.ListResourceRecordSetsInput, optFns ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error) {
	// Return a list that only contains default records to simulate an "empty" zone
	return &route53.ListResourceRecordSetsOutput{ResourceRecordSets: []r53types.ResourceRecordSet{
		{Type: "NS"},
		{Type: "SOA"},
	}}, nil
}
func (m *MockR53Client) DeleteHostedZone(ctx context.Context, params *route53.DeleteHostedZoneInput, optFns ...func(*route53.Options)) (*route53.DeleteHostedZoneOutput, error) {
	return &route53.DeleteHostedZoneOutput{}, nil
}

// --- Test Suite ---

var _ = Describe("ParkedDomain Controller", func() {
	const (
		ParkedDomainName      = "test-domain"
		ParkedDomainNamespace = "default"
		DomainName            = "test.example.com"
		Timeout               = time.Second * 10
		Interval              = time.Millisecond * 250
	)

	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func() {
			By("creating the custom resource for the Kind ParkedDomain")
			ctx := context.Background()

			// Define the ParkedDomain resource
			parkedDomain := &parkingv1alpha1.ParkedDomain{
				TypeMeta:   metav1.TypeMeta{APIVersion: "parking.yourcompany.com/v1alpha1", Kind: "ParkedDomain"},
				ObjectMeta: metav1.ObjectMeta{Name: ParkedDomainName, Namespace: ParkedDomainNamespace},
				Spec:       parkingv1alpha1.ParkedDomainSpec{DomainName: DomainName},
			}
			Expect(k8sClient.Create(ctx, parkedDomain)).To(Succeed())

			// --- Assertions for Creation ---
			parkedDomainLookupKey := types.NamespacedName{Name: ParkedDomainName, Namespace: ParkedDomainNamespace}
			createdParkedDomain := &parkingv1alpha1.ParkedDomain{}

			// We'll wait until the status is updated with the ZoneID and Status
			Eventually(func() (string, error) {
				err := k8sClient.Get(ctx, parkedDomainLookupKey, createdParkedDomain)
				if err != nil {
					return "", err
				}
				return createdParkedDomain.Status.Status, nil
			}, Timeout, Interval).Should(Equal("Provisioned"))

			// Check that the status fields were populated correctly by the mock
			Expect(createdParkedDomain.Status.ZoneID).To(Equal("MOCKZONEID123"))
			Expect(createdParkedDomain.Status.NameServers).To(ContainElement("ns-1.awsdns.com"))

			// --- Trigger and Assert Deletion ---
			By("deleting the custom resource for the Kind ParkedDomain")
			Expect(k8sClient.Delete(ctx, createdParkedDomain)).To(Succeed())

			// Wait until the resource is completely gone from the cluster
			Eventually(func() error {
				return k8sClient.Get(ctx, parkedDomainLookupKey, createdParkedDomain)
			}, Timeout, Interval).ShouldNot(Succeed())
		})
	})
})
