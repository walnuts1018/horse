package ingestion

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestObjectStorePutRequestsSSEAndProtectsOriginalObjects(t *testing.T) {
	original := []byte("receipt bytes")
	hash := DocumentHash(original)
	var putCalls, headCalls, getCalls int
	storedData := append([]byte(nil), original...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/horse/household/originals/sse-v1/"+hash {
			t.Errorf("unexpected object key: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			putCalls++
			if r.Header.Get("X-Amz-Server-Side-Encryption") != "AES256" || r.Header.Get("If-None-Match") != "*" || r.Header.Get("X-Amz-Meta-Sha256") != hash {
				t.Errorf("PutObject omitted encryption or immutability controls: headers=%v", r.Header)
			}
			if putCalls == 1 {
				body, err := io.ReadAll(r.Body)
				if err != nil || !bytes.Equal(body, original) {
					t.Errorf("PutObject changed original bytes: %q, %v", body, err)
				}
				w.Header().Set("ETag", `"stored"`)
				return
			}
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code><Message>object already exists</Message></Error>`)
		case http.MethodHead:
			headCalls++
			w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("X-Amz-Server-Side-Encryption", "AES256")
			w.Header().Set("X-Amz-Meta-Sha256", hash)
		case http.MethodGet:
			getCalls++
			w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("X-Amz-Server-Side-Encryption", "AES256")
			_, _ = w.Write(storedData)
		default:
			t.Errorf("unexpected S3 method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client := s3.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test-access", "test-secret", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(server.URL)
		options.UsePathStyle = true
	})
	store := &ObjectStore{Client: client, Bucket: "horse"}

	key, returnedHash, err := store.Put(context.Background(), "household", "image/jpeg", original)
	if err != nil || returnedHash != hash || !strings.HasSuffix(key, "/originals/sse-v1/"+hash) {
		t.Fatalf("first SSE upload failed: key=%q hash=%q err=%v", key, returnedHash, err)
	}
	if repeatedKey, repeatedHash, err := store.Put(context.Background(), "household", "image/jpeg", original); err != nil || repeatedKey != key || repeatedHash != hash {
		t.Fatalf("idempotent SSE upload failed: key=%q hash=%q err=%v", repeatedKey, repeatedHash, err)
	}
	if putCalls != 2 || headCalls != 1 || getCalls != 1 {
		t.Fatalf("unexpected idempotency requests: put=%d head=%d get=%d", putCalls, headCalls, getCalls)
	}
	storedData = []byte("different object")
	if _, _, err := store.Put(context.Background(), "household", "image/jpeg", original); err == nil {
		t.Fatal("existing object with different bytes was accepted")
	}
}

func TestObjectStoreGetKeepsLegacyReadsAndVerifiesSSEObjects(t *testing.T) {
	data := []byte("legacy receipt")
	serveEncryptedHeader := false
	storedData := append([]byte(nil), data...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
		w.Header().Set("Content-Type", "image/jpeg")
		if serveEncryptedHeader {
			w.Header().Set("X-Amz-Server-Side-Encryption", "AES256")
		}
		_, _ = w.Write(storedData)
	}))
	defer server.Close()
	client := s3.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test-access", "test-secret", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(server.URL)
		options.UsePathStyle = true
	})
	store := &ObjectStore{Client: client, Bucket: "horse"}
	if _, err := store.Get(context.Background(), "household/originals/"+DocumentHash(data), "image/jpeg"); err != nil {
		t.Fatalf("legacy plaintext original is no longer readable: %v", err)
	}
	if _, err := store.Get(context.Background(), "household/originals/"+StorageEncryptionVersion+"/"+DocumentHash(data), "image/jpeg"); err == nil {
		t.Fatal("versioned object without SSE metadata was accepted")
	}
	serveEncryptedHeader = true
	if _, err := store.Get(context.Background(), "household/originals/"+StorageEncryptionVersion+"/"+DocumentHash(data), "image/jpeg"); err != nil {
		t.Fatalf("valid SSE original was rejected: %v", err)
	}
	storedData = []byte("tampered receipt")
	if _, err := store.Get(context.Background(), "household/originals/"+StorageEncryptionVersion+"/"+DocumentHash(data), "image/jpeg"); err == nil {
		t.Fatal("SSE object whose bytes do not match its content-addressed key was accepted")
	}
}
