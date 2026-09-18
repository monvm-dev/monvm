// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Store manages the deployment's private S3 bucket.
type Store struct {
	bucket string
	region string
	client *s3.Client
}

// New creates an S3 store client for a deployment bucket.
func New(ctx context.Context, region, profile, bucket string) (*Store, error) {
	options := []func(*config.LoadOptions) error{
		config.WithRegion(region),
		config.WithUseDualStackEndpoint(aws.DualStackEndpointStateEnabled),
	}
	if profile != "" {
		options = append(options, config.WithSharedConfigProfile(profile))
	}
	configuration, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	return &Store{bucket: bucket, region: region, client: s3.NewFromConfig(configuration)}, nil
}

// Ensure creates or validates the bucket and enforces its security settings.
func (s *Store) Ensure(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		code := apiErrorCode(err)
		if code != "NotFound" && code != "NoSuchBucket" {
			return fmt.Errorf("access bucket: %w", err)
		}
		input := &s3.CreateBucketInput{Bucket: aws.String(s.bucket)}
		if s.region != "us-east-1" {
			input.CreateBucketConfiguration = &types.CreateBucketConfiguration{LocationConstraint: types.BucketLocationConstraint(s.region)}
		}
		if _, err = s.client.CreateBucket(ctx, input); err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
		if err = s3.NewBucketExistsWaiter(s.client).Wait(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)}, 5*time.Minute); err != nil {
			return err
		}
	}
	location, err := s.client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		return fmt.Errorf("get bucket region: %w", err)
	}
	actual := string(location.LocationConstraint)
	switch actual {
	case "":
		actual = "us-east-1"
	case "EU":
		actual = "eu-west-1"
	}
	if actual != s.region {
		return fmt.Errorf("bucket is in %s, not %s", actual, s.region)
	}
	if _, err = s.client.PutBucketOwnershipControls(ctx, &s3.PutBucketOwnershipControlsInput{
		Bucket: aws.String(s.bucket),
		OwnershipControls: &types.OwnershipControls{Rules: []types.OwnershipControlsRule{{
			ObjectOwnership: types.ObjectOwnershipBucketOwnerEnforced,
		}}},
	}); err != nil {
		return fmt.Errorf("enforce bucket ownership: %w", err)
	}
	versioning, err := s.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		return fmt.Errorf("get bucket versioning: %w", err)
	}
	if versioning.Status == types.BucketVersioningStatusEnabled {
		if _, err = s.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: aws.String(s.bucket),
			VersioningConfiguration: &types.VersioningConfiguration{
				Status: types.BucketVersioningStatusSuspended,
			},
		}); err != nil {
			return fmt.Errorf("suspend bucket versioning: %w", err)
		}
	}
	block, err := s.client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(s.bucket)})
	if err != nil && apiErrorCode(err) != "NoSuchPublicAccessBlockConfiguration" {
		return fmt.Errorf("get bucket public access block: %w", err)
	}
	if err != nil || !publicAccessBlocked(block.PublicAccessBlockConfiguration) {
		_, err = s.client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
			Bucket: aws.String(s.bucket),
			PublicAccessBlockConfiguration: &types.PublicAccessBlockConfiguration{
				BlockPublicAcls:       aws.Bool(true),
				BlockPublicPolicy:     aws.Bool(true),
				IgnorePublicAcls:      aws.Bool(true),
				RestrictPublicBuckets: aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("enable bucket public access block: %w", err)
		}
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return err
	}
	key := ".monvm-access-check-" + hex.EncodeToString(random)
	if err = s.Put(ctx, key, bytes.NewReader([]byte("monvm")), 5); err != nil {
		return fmt.Errorf("verify bucket write: %w", err)
	}
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("verify bucket read: %w", err)
	}
	_, readErr := io.Copy(io.Discard, object.Body)
	closeErr := object.Body.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err = s.Delete(ctx, key); err != nil {
		return fmt.Errorf("verify bucket delete: %w", err)
	}
	return nil
}

// Put writes an object to the deployment bucket.
func (s *Store) Put(ctx context.Context, key string, body io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
	})
	return err
}

// Delete removes an object from the deployment bucket.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

// EmptyAndDelete removes all current and historical objects before deleting the bucket.
func (s *Store) EmptyAndDelete(ctx context.Context) error {
	for {
		objects, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket)})
		if err != nil {
			return err
		}
		if len(objects.Contents) == 0 {
			break
		}
		identifiers := make([]types.ObjectIdentifier, 0, len(objects.Contents))
		for _, object := range objects.Contents {
			identifiers = append(identifiers, types.ObjectIdentifier{Key: object.Key})
		}
		if err = s.deleteObjects(ctx, identifiers); err != nil {
			return err
		}
	}
	for {
		versions, err := s.client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(s.bucket)})
		if err != nil {
			return err
		}
		identifiers := make([]types.ObjectIdentifier, 0, len(versions.Versions)+len(versions.DeleteMarkers))
		for _, object := range versions.Versions {
			identifiers = append(identifiers, types.ObjectIdentifier{Key: object.Key, VersionId: object.VersionId})
		}
		for _, marker := range versions.DeleteMarkers {
			identifiers = append(identifiers, types.ObjectIdentifier{Key: marker.Key, VersionId: marker.VersionId})
		}
		if len(identifiers) == 0 {
			break
		}
		if err = s.deleteObjects(ctx, identifiers); err != nil {
			return err
		}
	}
	_, err := s.client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

// deleteObjects deletes one batch of object identifiers and reports partial failures.
func (s *Store) deleteObjects(ctx context.Context, objects []types.ObjectIdentifier) error {
	result, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return err
	}
	if len(result.Errors) != 0 {
		return fmt.Errorf("delete object %s: %s", aws.ToString(result.Errors[0].Key), aws.ToString(result.Errors[0].Message))
	}
	return nil
}

// publicAccessBlocked reports whether every S3 public-access safeguard is enabled.
func publicAccessBlocked(configuration *types.PublicAccessBlockConfiguration) bool {
	return configuration != nil && aws.ToBool(configuration.BlockPublicAcls) && aws.ToBool(configuration.BlockPublicPolicy) &&
		aws.ToBool(configuration.IgnorePublicAcls) && aws.ToBool(configuration.RestrictPublicBuckets)
}

// apiErrorCode extracts the stable service code from an AWS API error.
func apiErrorCode(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode()
	}
	return ""
}
