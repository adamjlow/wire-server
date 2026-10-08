package cargohold

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type Store interface {
	Upload(context.Context, string, map[string]string, io.Reader, int64) (int64, error)
	Metadata(context.Context, string) (map[string]string, error)
	SignedURL(context.Context, string, string) (string, error)
	Delete(context.Context, string) error
	UpdateMetadata(context.Context, string, map[string]string) error
	Open(context.Context, string) (io.ReadCloser, error)
}

type S3Store struct {
	client   *s3.Client
	config   aws.Config
	settings Config
}

func NewS3Store(ctx context.Context, settings Config) (*S3Store, error) {
	c, err := config.LoadDefaultConfig(ctx, config.WithDefaultRegion("us-east-1"))
	if err != nil {
		return nil, err
	}
	c.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	c.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	c.HTTPClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConns: 2048, MaxIdleConnsPerHost: 1024, ResponseHeaderTimeout: 10 * time.Second}}
	s := &S3Store{config: c, settings: settings}
	s.client = s.clientFor(settings.AWS.Endpoint)
	return s, nil
}

func (s *S3Store) clientFor(endpoint string) *s3.Client {
	return s3.NewFromConfig(s.config, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = s.settings.AWS.AddressingStyle == "" || s.settings.AWS.AddressingStyle == "path"
	})
}

func (s *S3Store) Upload(ctx context.Context, key string, meta map[string]string, r io.Reader, n int64) (int64, error) {
	created, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key), Metadata: meta, ContentType: aws.String("application/octet-stream")})
	if err != nil {
		return 0, err
	}
	complete := false
	defer func() {
		if !complete {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_, _ = s.client.AbortMultipartUpload(cleanup, &s3.AbortMultipartUploadInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key), UploadId: created.UploadId})
		}
	}()
	parts := []types.CompletedPart{}
	limited := io.LimitReader(r, n)
	buffer := make([]byte, 5*1024*1024)
	var seen int64
	for {
		count, readErr := io.ReadFull(limited, buffer)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return seen, readErr
		}
		if count > 0 {
			part := int32(len(parts) + 1)
			out, err := s.client.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key), UploadId: created.UploadId, PartNumber: aws.Int32(part), Body: bytes.NewReader(buffer[:count]), ContentLength: aws.Int64(int64(count))})
			if err != nil {
				return seen, err
			}
			parts = append(parts, types.CompletedPart{ETag: out.ETag, PartNumber: aws.Int32(part)})
			seen += int64(count)
		}
		if readErr != nil {
			break
		}
	}
	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key), UploadId: created.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: parts}})
	complete = err == nil
	return seen, err
}

func (s *S3Store) Metadata(ctx context.Context, key string) (map[string]string, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key)})
	var api smithy.APIError
	if errors.As(err, &api) && (api.ErrorCode() == "NotFound" || api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "AccessDenied") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out.Metadata, nil
}

func (s *S3Store) SignedURL(ctx context.Context, key, host string) (string, error) {
	ep := s.settings.AWS.DownloadEndpoint
	if ep == "" {
		ep = s.settings.AWS.Endpoint
	}
	if len(s.settings.AWS.MultiIngress) > 0 {
		var ok bool
		ep, ok = s.settings.AWS.MultiIngress[host]
		if !ok {
			return "", errNotFound
		}
	}
	out, err := s3.NewPresignClient(s.clientFor(ep)).PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key)}, func(o *s3.PresignOptions) {
		o.Expires = time.Duration(s.settings.Settings.DownloadLinkTTL) * time.Second
	})
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key)})
	return err
}

func objectKey(key string) (string, error) {
	p := strings.SplitN(key, "-", 3)
	if len(p) != 3 || p[0] != "3" || !validUUID(p[2]) {
		return "", errNotFound
	}
	ret, ok := retentionNames[p[1]]
	if !ok {
		return "", errNotFound
	}
	return "v3/" + ret + "/" + strings.ToLower(p[2]), nil
}

// Kept here for the next token-copy slice; copy sources require URL path escaping.
func copySource(bucket, key string) string { return url.PathEscape(bucket + "/" + key) }

func (s *S3Store) UpdateMetadata(ctx context.Context, key string, meta map[string]string) error {
	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key), CopySource: aws.String(copySource(s.settings.AWS.Bucket, key)), Metadata: meta, MetadataDirective: types.MetadataDirectiveReplace, ContentType: aws.String("application/octet-stream")})
	return err
}
func (s *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.settings.AWS.Bucket), Key: aws.String(key), ResponseContentType: aws.String("application/octet-stream")})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}
