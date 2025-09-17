package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	parkingv1alpha1 "github.com/gminiba/parked-domain-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getS3WebsiteHostedZoneID returns the canonical hosted zone ID for S3 website endpoints for a given region.
func getS3WebsiteHostedZoneID(region string) string {
	// Source: https://docs.aws.amazon.com/general/latest/gr/s3.html
	s3HostedZoneIDs := map[string]string{
		"us-east-1": "Z3AQBSTGFYJSTF",
		"us-west-1": "Z2F56UZL2M1ACD",
		"us-west-2": "Z3BJ6K6RIION7M",
		"eu-west-1": "Z1BKCTXD74EZPE",

		"eu-central-1": "Z21DNDUVLTQW6Q",
		// ... add other regions as needed
	}
	return s3HostedZoneIDs[region]
}

// reconcileRoute53Zone ensures the Hosted Zone exists and returns its ID and nameservers.
func (r *ParkedDomainReconciler) reconcileRoute53ARecord(ctx context.Context, pd *parkingv1alpha1.ParkedDomain, zoneID, s3Endpoint string) error {
	logger := log.FromContext(ctx)

	region := pd.Spec.Region
	if region == "" {
		region = "eu-central-1"
	}

	s3HostedZoneID := getS3WebsiteHostedZoneID(region)
	if s3HostedZoneID == "" {
		return fmt.Errorf("unsupported S3 website region for alias record: %s", region)
	}

	changeBatch := &r53types.ChangeBatch{
		Comment: aws.String("Managed by ParkedDomain Operator"),
		Changes: []r53types.Change{
			{
				Action: r53types.ChangeActionUpsert,
				ResourceRecordSet: &r53types.ResourceRecordSet{
					Name: aws.String(pd.Spec.DomainName),
					Type: "A",
					AliasTarget: &r53types.AliasTarget{
						HostedZoneId:         aws.String(s3HostedZoneID),
						DNSName:              aws.String(s3Endpoint),
						EvaluateTargetHealth: false,
					},
				},
			},
		},
	}

	_, err := r.R53Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch:  changeBatch,
	})
	if err != nil {
		return fmt.Errorf("failed to create/update A record: %w", err)
	}

	logger.Info("Successfully reconciled Route 53 A record", "DomainName", pd.Spec.DomainName)
	return nil
}

// cleanupRoute53Zone cleans up records and deletes the Hosted Zone.
func (r *ParkedDomainReconciler) cleanupRoute53Zone(ctx context.Context, pd *parkingv1alpha1.ParkedDomain) error {
	logger := log.FromContext(ctx)
	zoneID := pd.Status.ZoneID
	if zoneID == "" {
		logger.Info("ZoneID is empty, skipping Route 53 cleanup")
		return nil
	}

	logger.Info("Starting Route 53 Hosted Zone cleanup", "ZoneID", zoneID)
	paginator := route53.NewListResourceRecordSetsPaginator(r.R53Client, &route53.ListResourceRecordSetsInput{HostedZoneId: aws.String(zoneID)})
	var changes []r53types.Change
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list records in Hosted Zone: %w", err)
		}
		for _, record := range page.ResourceRecordSets {
			if record.Type != "NS" && record.Type != "SOA" {
				changes = append(changes, r53types.Change{
					Action:            r53types.ChangeActionDelete,
					ResourceRecordSet: &record,
				})
			}
		}
	}

	if len(changes) > 0 {
		_, err := r.R53Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(zoneID),
			ChangeBatch:  &r53types.ChangeBatch{Changes: changes},
		})
		if err != nil {
			return fmt.Errorf("failed to delete records from Hosted Zone: %w", err)
		}
	}

	_, err := r.R53Client.DeleteHostedZone(ctx, &route53.DeleteHostedZoneInput{Id: aws.String(zoneID)})
	if err != nil {
		var nshze *r53types.NoSuchHostedZone
		if !errors.As(err, &nshze) {
			return fmt.Errorf("failed to delete Hosted Zone: %w", err)
		}
	}

	logger.Info("Route 53 Hosted Zone cleanup complete", "ZoneID", zoneID)
	return nil
}

func (r *ParkedDomainReconciler) reconcileRoute53Zone(ctx context.Context, pd *parkingv1alpha1.ParkedDomain) (string, []string, error) {
	logger := log.FromContext(ctx)
	domainName := pd.Spec.DomainName

	// Check if the Hosted Zone already exists.
	listInput := &route53.ListHostedZonesByNameInput{
		DNSName: aws.String(domainName),
	}
	listOutput, err := r.R53Client.ListHostedZonesByName(ctx, listInput)
	if err != nil {
		return "", nil, fmt.Errorf("failed to list hosted zones: %w", err)
	}

	// If a zone with the exact name is found, adopt it.
	if len(listOutput.HostedZones) > 0 && *listOutput.HostedZones[0].Name == domainName+"." {
		existingZone := listOutput.HostedZones[0]
		zoneID := strings.Replace(*existingZone.Id, "/hostedzone/", "", 1)
		logger.Info("Found existing Route 53 Hosted Zone, adopting it.", "ZoneID", zoneID)

		// To get the nameservers for an existing zone, we need another API call.
		getZoneOutput, err := r.R53Client.GetHostedZone(ctx, &route53.GetHostedZoneInput{Id: existingZone.Id})
		if err != nil {
			return "", nil, fmt.Errorf("failed to get details for existing hosted zone: %w", err)
		}

		var nameservers []string
		nameservers = append(nameservers, getZoneOutput.DelegationSet.NameServers...)

		return zoneID, nameservers, nil
	}

	// If no zone was found, proceed to create it.
	logger.Info("No existing Hosted Zone found, creating a new one.")
	callerReference := fmt.Sprintf("parkeddomain-operator-%s-%d", pd.Name, time.Now().Unix())
	createZoneInput := &route53.CreateHostedZoneInput{
		Name:            aws.String(domainName),
		CallerReference: aws.String(callerReference),
	}

	createOutput, err := r.R53Client.CreateHostedZone(ctx, createZoneInput)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create Route 53 Hosted Zone: %w", err)
	}

	zoneID := strings.Replace(*createOutput.HostedZone.Id, "/hostedzone/", "", 1)
	var nameservers []string
	nameservers = append(nameservers, createOutput.DelegationSet.NameServers...)

	logger.Info("Successfully created Route 53 Hosted Zone", "ZoneID", zoneID)
	return zoneID, nameservers, nil
}
