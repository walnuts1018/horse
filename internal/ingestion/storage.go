package ingestion

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type ObjectStore struct {
	Client *s3.Client
	Bucket string
}

func NewObjectStore(ctx context.Context, endpoint, bucket, accessKey, secretKey string) (*ObjectStore, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" {
		return nil, errors.New("invalid S3 endpoint")
	}
	if bucket == "" || (accessKey == "") != (secretKey == "") {
		return nil, errors.New("S3 bucket and a complete credential pair are required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	region := cmp.Or(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region), awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired), awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired)}
	if accessKey != "" {
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")))
	}
	config, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, errors.New("initialize S3 configuration failed")
	}
	client := s3.NewFromConfig(config, func(options *s3.Options) {
		options.BaseEndpoint = new(endpoint)
		options.UsePathStyle = true
	})
	return &ObjectStore{Client: client, Bucket: bucket}, nil
}

func DocumentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (s *ObjectStore) Put(ctx context.Context, householdID, mimeType string, data []byte) (string, string, error) {
	if len(data) == 0 || len(data) > MaxDocumentBytes {
		return "", "", errors.New("invalid document size")
	}
	if strings.ContainsAny(householdID, "/\\.") || householdID == "" {
		return "", "", errors.New("invalid household identifier")
	}
	hash := DocumentHash(data)
	key := householdID + "/originals/" + hash
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: new(s.Bucket), Key: new(key), Body: bytes.NewReader(data), ContentLength: new(int64(len(data))),
		ContentType: new(mimeType), IfNoneMatch: new("*"),
		Metadata: map[string]string{"sha256": hash},
	})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && apiError.ErrorCode() == "PreconditionFailed" {
			if verifyErr := s.verifyExistingObject(ctx, key, mimeType, hash, data); verifyErr == nil {
				return key, hash, nil
			}
		}
		return "", "", errors.New("store original document failed")
	}
	return key, hash, nil
}

func (s *ObjectStore) verifyExistingObject(ctx context.Context, key, mimeType, hash string, data []byte) error {
	head, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: new(s.Bucket), Key: new(key)})
	if err != nil || head.Metadata["sha256"] != hash || head.ContentLength == nil || *head.ContentLength != int64(len(data)) {
		return errors.New("existing object metadata does not match original content")
	}
	stored, err := s.Get(ctx, key, mimeType)
	if err != nil || !bytes.Equal(stored.Data, data) {
		return errors.New("existing object does not match original content")
	}
	return nil
}

func (s *ObjectStore) Get(ctx context.Context, key, mimeType string) (Document, error) {
	object, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: new(s.Bucket), Key: new(key)})
	if err != nil {
		return Document{}, errors.New("load original document failed")
	}
	if object.ContentLength == nil || *object.ContentLength <= 0 || *object.ContentLength > MaxDocumentBytes {
		closeErr := object.Body.Close()
		return Document{}, errors.Join(errors.New("invalid original document"), closeErr)
	}
	data, readErr := io.ReadAll(io.LimitReader(object.Body, MaxDocumentBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil {
		return Document{}, errors.Join(errors.New("read original document failed"), readErr, closeErr)
	}
	if len(data) > MaxDocumentBytes {
		return Document{}, errors.New("original document exceeds limit")
	}
	if _, originalKey, ok := strings.Cut(key, "/originals/"); ok {
		if suffix, found := strings.CutPrefix(originalKey, "sse-v1/"); found {
			originalKey = suffix
		}
		storedHash, _, _ := strings.Cut(originalKey, "/")
		if storedHash == "" {
			storedHash = originalKey
		}
		if len(storedHash) == sha256.Size*2 && DocumentHash(data) != storedHash {
			return Document{}, errors.New("original document hash does not match object key")
		}
	}
	return Document{MIMEType: mimeType, Data: data}, nil
}
