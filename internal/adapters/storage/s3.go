// Package storage implementa app.Storage com presigned URLs do S3 (spec §7):
// upload restrito a PutObject em $linkId/raw/, download via presigned GET 1h.
package storage

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Storage struct {
	presign *s3.PresignClient
	bucket  string
}

func NewS3Storage(client *s3.Client, bucket string) *S3Storage {
	return &S3Storage{presign: s3.NewPresignClient(client), bucket: bucket}
}

func (s *S3Storage) PresignPut(ctx context.Context, key string, expires time.Duration) (string, error) {
	out, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *S3Storage) PresignGet(ctx context.Context, key string, expires time.Duration) (string, error) {
	out, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}
