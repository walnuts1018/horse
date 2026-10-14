package ingestion

import (
	"bytes"
	"cmp"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	Client                *s3.Client
	Bucket                string
	emailEncryptionKeys   map[string][]byte
	activeEmailKeyID      string
	emailEncryptionKeyErr error
}

const (
	encryptedEmailPrefix       = "app-aesgcm-v2/"
	legacyEncryptedEmailPrefix = "app-aesgcm-v1/"
)

func NewObjectStore(ctx context.Context, endpoint, bucket, accessKey, secretKey, emailEncryptionKeys string) (*ObjectStore, error) {
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
	keys, activeKeyID, keyErr := parseEmailEncryptionKeys(emailEncryptionKeys)
	return &ObjectStore{
		Client: client, Bucket: bucket, emailEncryptionKeys: keys,
		activeEmailKeyID: activeKeyID, emailEncryptionKeyErr: keyErr,
	}, nil
}

func parseEmailEncryptionKeys(configuration string) (map[string][]byte, string, error) {
	if configuration == "" {
		return nil, "", errors.New("email body encryption keys are not configured")
	}
	keys := make(map[string][]byte)
	activeKeyID := ""
	for entry := range strings.SplitSeq(configuration, ",") {
		keyID, encodedKey, ok := strings.Cut(entry, ":")
		if !ok || !validEmailKeyID(keyID) || encodedKey == "" {
			return nil, "", errors.New("invalid email body encryption keyring")
		}
		if _, exists := keys[keyID]; exists {
			return nil, "", errors.New("invalid email body encryption keyring")
		}
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		if err != nil || len(key) != 32 {
			return nil, "", errors.New("invalid email body encryption keyring")
		}
		keys[keyID] = key
		if activeKeyID == "" {
			activeKeyID = keyID
		}
	}
	return keys, activeKeyID, nil
}

func validEmailKeyID(keyID string) bool {
	if keyID == "" || len(keyID) > 32 {
		return false
	}
	for _, r := range keyID {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
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
	storedData := data
	metadata := map[string]string{"sha256": hash}
	metadataKey := "sha256"
	identity := hash
	if mimeType == mimeTypeEmail {
		if s.emailEncryptionKeyErr != nil || s.activeEmailKeyID == "" {
			return "", "", errors.New("store original document failed")
		}
		activeKey := s.emailEncryptionKeys[s.activeEmailKeyID]
		identity = emailFingerprint(activeKey, data)
		key = householdID + "/originals/" + encryptedEmailPrefix + s.activeEmailKeyID + "/" + identity
		var err error
		storedData, err = encryptEmailBody(activeKey, key, data)
		if err != nil {
			return "", "", errors.New("store original document failed")
		}
		metadata = map[string]string{"hmac-sha256": identity}
		metadataKey = "hmac-sha256"
	}
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: new(s.Bucket), Key: new(key), Body: bytes.NewReader(storedData), ContentLength: new(int64(len(storedData))),
		ContentType: new(mimeType), IfNoneMatch: new("*"),
		Metadata: metadata,
	})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && apiError.ErrorCode() == "PreconditionFailed" {
			if verifyErr := s.verifyExistingObject(ctx, key, mimeType, metadataKey, identity, len(storedData), data); verifyErr == nil {
				return key, hash, nil
			}
		}
		return "", "", errors.New("store original document failed")
	}
	return key, hash, nil
}

func (s *ObjectStore) verifyExistingObject(ctx context.Context, key, mimeType, metadataKey, identity string, storedLength int, data []byte) error {
	head, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: new(s.Bucket), Key: new(key)})
	if err != nil || head.Metadata[metadataKey] != identity || head.ContentLength == nil || *head.ContentLength != int64(storedLength) {
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
	encryptedEmail := strings.Contains(key, "/originals/"+encryptedEmailPrefix) || strings.Contains(key, "/originals/"+legacyEncryptedEmailPrefix)
	maxStoredBytes := int64(MaxDocumentBytes)
	if encryptedEmail {
		maxStoredBytes += int64(2*aes.BlockSize + 12)
	}
	if object.ContentLength == nil || *object.ContentLength <= 0 || *object.ContentLength > maxStoredBytes {
		closeErr := object.Body.Close()
		return Document{}, errors.Join(errors.New("invalid original document"), closeErr)
	}
	data, readErr := io.ReadAll(io.LimitReader(object.Body, maxStoredBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil {
		return Document{}, errors.Join(errors.New("read original document failed"), readErr, closeErr)
	}
	if int64(len(data)) > maxStoredBytes {
		return Document{}, errors.New("original document exceeds limit")
	}
	if _, originalKey, ok := strings.Cut(key, "/originals/"); ok {
		if suffix, found := strings.CutPrefix(originalKey, encryptedEmailPrefix); found {
			if s.emailEncryptionKeyErr != nil {
				return Document{}, errors.New("decrypt original document failed")
			}
			keyID, fingerprint, ok := strings.Cut(suffix, "/")
			if !ok || !validEmailKeyID(keyID) {
				return Document{}, errors.New("decrypt original document failed")
			}
			encryptionKey, exists := s.emailEncryptionKeys[keyID]
			if !exists {
				return Document{}, errors.New("decrypt original document failed")
			}
			data, err = decryptEmailBody(encryptionKey, key, data)
			if err != nil {
				return Document{}, errors.New("decrypt original document failed")
			}
			if len(data) == 0 || len(data) > MaxDocumentBytes {
				return Document{}, errors.New("invalid original document")
			}
			if !hmac.Equal([]byte(emailFingerprint(encryptionKey, data)), []byte(fingerprint)) {
				return Document{}, errors.New("original document fingerprint does not match object key")
			}
		} else if suffix, found := strings.CutPrefix(originalKey, legacyEncryptedEmailPrefix); found {
			if s.emailEncryptionKeyErr != nil {
				return Document{}, errors.New("decrypt original document failed")
			}
			var decrypted []byte
			for _, encryptionKey := range s.emailEncryptionKeys {
				decrypted, err = decryptEmailBody(encryptionKey, key, data)
				if err == nil {
					break
				}
			}
			if err != nil {
				return Document{}, errors.New("decrypt original document failed")
			}
			data = decrypted
			originalKey = suffix
		} else if suffix, found := strings.CutPrefix(originalKey, "sse-v1/"); found {
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

func (s *ObjectStore) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("invalid original document key")
	}
	_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: new(s.Bucket), Key: new(key)})
	if err != nil {
		return errors.New("delete original document failed")
	}

	return nil
}

func encryptEmailBody(key []byte, objectKey string, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, []byte(objectKey)), nil
}

func emailFingerprint(key, plaintext []byte) string {
	keyDerivation := hmac.New(sha256.New, key)
	_, _ = keyDerivation.Write([]byte("horse/email-object-fingerprint-key/v1"))
	fingerprint := hmac.New(sha256.New, keyDerivation.Sum(nil))
	_, _ = fingerprint.Write(plaintext)
	return hex.EncodeToString(fingerprint.Sum(nil))
}

func decryptEmailBody(key []byte, objectKey string, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("encrypted email body is truncated")
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	return aead.Open(nil, nonce, sealed, []byte(objectKey))
}
