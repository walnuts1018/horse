package ingestion

import (
	"bytes"
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

func TestObjectStorePutPreservesOriginalAndRejectsConflictingContent(t *testing.T) {
	original := []byte("receipt bytes")
	hash := DocumentHash(original)
	var putCalls, headCalls, getCalls int
	storedData := bytes.Clone(original)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/horse/household/originals/"+hash {
			t.Errorf("unexpected object key: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			putCalls++
			if r.Header.Get("X-Amz-Server-Side-Encryption") != "" || r.Header.Get("If-None-Match") != "*" || r.Header.Get("X-Amz-Meta-Sha256") != hash {
				t.Errorf("PutObject sent unexpected encryption or omitted immutability controls: headers=%v", r.Header)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(body, original) {
				t.Errorf("PutObject changed original bytes: %q, %v", body, err)
			}
			if putCalls == 1 {
				w.Header().Set("ETag", `"stored"`)
				return
			}
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code><Message>object already exists</Message></Error>`)
		case http.MethodHead:
			headCalls++
			w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("X-Amz-Meta-Sha256", hash)
		case http.MethodGet:
			getCalls++
			w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(storedData)
		default:
			t.Errorf("unexpected S3 method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	store := newTestObjectStore(t, server)

	key, returnedHash, err := store.Put(t.Context(), "household", "image/jpeg", original)
	if err != nil || returnedHash != hash || !strings.HasSuffix(key, "/originals/"+hash) {
		t.Fatalf("first upload failed: key=%q hash=%q err=%v", key, returnedHash, err)
	}
	if repeatedKey, repeatedHash, err := store.Put(t.Context(), "household", "image/jpeg", original); err != nil || repeatedKey != key || repeatedHash != hash {
		t.Fatalf("idempotent upload failed: key=%q hash=%q err=%v", repeatedKey, repeatedHash, err)
	}
	if putCalls != 2 || headCalls != 1 || getCalls != 1 {
		t.Fatalf("unexpected idempotency requests: put=%d head=%d get=%d", putCalls, headCalls, getCalls)
	}
	storedData = []byte("receipt bytez")
	if _, _, err := store.Put(t.Context(), "household", "image/jpeg", original); err == nil {
		t.Fatal("existing object with different bytes was accepted")
	}
}

func TestObjectStoreGetReadsExistingSSEVersionThroughS3(t *testing.T) {
	data := []byte("legacy encrypted receipt")
	storedData := bytes.Clone(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(storedData)
	}))
	defer server.Close()
	store := newTestObjectStore(t, server)

	key := "household/originals/sse-v1/" + DocumentHash(data)
	document, err := store.Get(t.Context(), key, "image/jpeg")
	if err != nil || !bytes.Equal(document.Data, data) {
		t.Fatalf("S3-decrypted existing original was rejected: document=%q err=%v", document.Data, err)
	}
	storedData = []byte("tampered encrypted receipt")
	if _, err := store.Get(t.Context(), key, "image/jpeg"); err == nil {
		t.Fatal("existing SSE original whose bytes do not match its key was accepted")
	}
}

func newTestObjectStore(t *testing.T, server *httptest.Server) *ObjectStore {
	t.Helper()
	client := s3.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test-access", "test-secret", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(server.URL)
		options.UsePathStyle = true
	})
	return &ObjectStore{Client: client, Bucket: "horse"}
}
