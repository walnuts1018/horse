package ingestion

import (
	"bytes"
	"encoding/base64"
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
	t.Parallel()
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
			w.Header().Set("Content-Type", mimeTypeJPEG)
			w.Header().Set("X-Amz-Meta-Sha256", hash)
		case http.MethodGet:
			getCalls++
			w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
			w.Header().Set("Content-Type", mimeTypeJPEG)
			_, _ = w.Write(storedData)
		default:
			t.Errorf("unexpected S3 method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	store := newTestObjectStore(t, server)

	key, returnedHash, err := store.Put(t.Context(), "household", mimeTypeJPEG, original)
	if err != nil || returnedHash != hash || !strings.HasSuffix(key, "/originals/"+hash) {
		t.Fatalf("first upload failed: key=%q hash=%q err=%v", key, returnedHash, err)
	}
	if repeatedKey, repeatedHash, err := store.Put(t.Context(), "household", mimeTypeJPEG, original); err != nil || repeatedKey != key || repeatedHash != hash {
		t.Fatalf("idempotent upload failed: key=%q hash=%q err=%v", repeatedKey, repeatedHash, err)
	}
	if putCalls != 2 || headCalls != 1 || getCalls != 1 {
		t.Fatalf("unexpected idempotency requests: put=%d head=%d get=%d", putCalls, headCalls, getCalls)
	}
	storedData = []byte("receipt bytez")
	if _, _, err := store.Put(t.Context(), "household", mimeTypeJPEG, original); err == nil {
		t.Fatal("existing object with different bytes was accepted")
	}
}

func TestObjectStorePutAnalysisUsesDocumentScopedNamespaceAndIsIdempotent(t *testing.T) {
	t.Parallel()
	data := []byte("analysis image bytes")
	hash := DocumentHash(data)
	objects := make(map[string][]byte)
	var putCalls, headCalls, getCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/horse/")
		switch r.Method {
		case http.MethodPut:
			putCalls++
			if r.Header.Get("If-None-Match") != "*" || r.Header.Get("X-Amz-Meta-Sha256") != hash || r.Header.Get("X-Amz-Server-Side-Encryption") != "" {
				t.Errorf("PutAnalysis sent unexpected controls: headers=%v", r.Header)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(body, data) {
				t.Errorf("PutAnalysis changed analysis bytes: %q, %v", body, err)
			}
			if _, exists := objects[key]; exists {
				w.WriteHeader(http.StatusPreconditionFailed)
				_, _ = fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code><Message>object already exists</Message></Error>`)
				return
			}
			objects[key] = bytes.Clone(body)
			w.Header().Set("ETag", `"stored"`)
		case http.MethodHead:
			headCalls++
			stored, exists := objects[key]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(stored)))
			w.Header().Set("X-Amz-Meta-Sha256", hash)
		case http.MethodGet:
			getCalls++
			stored, exists := objects[key]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(stored)))
			_, _ = w.Write(stored)
		default:
			t.Errorf("unexpected S3 method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	store := newTestObjectStore(t, server)

	firstKey, firstHash, err := store.PutAnalysis(t.Context(), "household", "document-1", mimeTypeJPEG, data)
	wantKey := "household/analysis/document-1/" + hash
	if err != nil || firstKey != wantKey || firstHash != hash {
		t.Fatalf("analysis upload failed: key=%q hash=%q err=%v", firstKey, firstHash, err)
	}
	repeatedKey, repeatedHash, err := store.PutAnalysis(t.Context(), "household", "document-1", mimeTypeJPEG, data)
	if err != nil || repeatedKey != firstKey || repeatedHash != firstHash {
		t.Fatalf("repeated analysis upload was not idempotent: key=%q hash=%q err=%v", repeatedKey, repeatedHash, err)
	}
	otherKey, otherHash, err := store.PutAnalysis(t.Context(), "household", "document-2", mimeTypeJPEG, data)
	if err != nil || otherKey == firstKey || otherHash != firstHash || otherKey != "household/analysis/document-2/"+hash {
		t.Fatalf("analysis objects were not scoped by document ID: key=%q hash=%q err=%v", otherKey, otherHash, err)
	}
	if len(objects) != 2 || putCalls != 3 || headCalls != 1 || getCalls != 1 {
		t.Fatalf("unexpected analysis object operations: objects=%d put=%d head=%d get=%d", len(objects), putCalls, headCalls, getCalls)
	}
}

func TestObjectStorePutAnalysisRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid input reached S3: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	store := newTestObjectStore(t, server)

	for _, input := range []struct {
		name        string
		householdID string
		documentID  string
		mimeType    string
		data        []byte
	}{
		{name: "empty data", householdID: "household", documentID: "document", mimeType: mimeTypeJPEG},
		{name: "household path separator", householdID: "household/other", documentID: "document", mimeType: mimeTypeJPEG, data: []byte("image")},
		{name: "document path separator", householdID: "household", documentID: "../document", mimeType: mimeTypeJPEG, data: []byte("image")},
		{name: "non-image MIME type", householdID: "household", documentID: "document", mimeType: "application/pdf", data: []byte("image")},
		{name: "malformed MIME type", householdID: "household", documentID: "document", mimeType: "image/\r\n", data: []byte("image")},
	} {
		t.Run(input.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := store.PutAnalysis(t.Context(), input.householdID, input.documentID, input.mimeType, input.data); err == nil {
				t.Fatal("invalid analysis input was accepted")
			}
		})
	}
}

func TestObjectStoreGetReadsExistingSSEVersionThroughS3(t *testing.T) {
	t.Parallel()
	data := []byte("legacy encrypted receipt")
	storedData := bytes.Clone(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
		w.Header().Set("Content-Type", mimeTypeJPEG)
		_, _ = w.Write(storedData)
	}))
	defer server.Close()
	store := newTestObjectStore(t, server)

	key := "household/originals/sse-v1/" + DocumentHash(data)
	document, err := store.Get(t.Context(), key, mimeTypeJPEG)
	if err != nil || !bytes.Equal(document.Data, data) {
		t.Fatalf("S3-decrypted existing original was rejected: document=%q err=%v", document.Data, err)
	}
	storedData = []byte("tampered encrypted receipt")
	if _, err := store.Get(t.Context(), key, mimeTypeJPEG); err == nil {
		t.Fatal("existing SSE original whose bytes do not match its key was accepted")
	}
}

func TestObjectStoreGetReadsPreviousApplicationEncryptionPrefix(t *testing.T) {
	t.Parallel()
	data := []byte("From: receipt@example.test\r\n\r\nlegacy application ciphertext")
	encryptionKey := bytes.Repeat([]byte{0x31}, 32)
	objectKey := "household/originals/" + legacyEncryptedEmailPrefix + DocumentHash(data)
	storedData, err := encryptEmailBody(encryptionKey, objectKey, data)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
		_, _ = w.Write(storedData)
	}))
	defer server.Close()
	store := newTestObjectStore(t, server)
	store.emailEncryptionKeys = map[string][]byte{"previous": encryptionKey}
	store.activeEmailKeyID = "previous"

	document, err := store.Get(t.Context(), objectKey, mimeTypeEmail)
	if err != nil || !bytes.Equal(document.Data, data) {
		t.Fatalf("previous encrypted original was rejected: document=%q err=%v", document.Data, err)
	}
}

func TestObjectStoreEncryptsEmailBodyAndRejectsTampering(t *testing.T) {
	t.Parallel()
	plaintext := []byte("From: receipt@example.test\r\n\r\nreceipt details")
	firstKey := bytes.Repeat([]byte{0x42}, 32)
	secondKey := bytes.Repeat([]byte{0x24}, 32)
	keys, activeKeyID, err := parseEmailEncryptionKeys("key-1:" + base64.StdEncoding.EncodeToString(firstKey))
	if err != nil {
		t.Fatal(err)
	}
	var objectKey string
	var storedData []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		objectKey = strings.TrimPrefix(r.URL.Path, "/horse/")
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("X-Amz-Server-Side-Encryption") != "" || r.Header.Get("X-Amz-Meta-Hmac-Sha256") != emailFingerprint(firstKey, plaintext) || r.Header.Get("X-Amz-Meta-Sha256") != "" {
				t.Errorf("PutObject sent unexpected encryption or identity metadata: headers=%v", r.Header)
			}
			var err error
			storedData, err = io.ReadAll(r.Body)
			if err != nil || bytes.Equal(storedData, plaintext) {
				t.Errorf("PutObject did not store ciphertext: body=%q err=%v", storedData, err)
			}
			w.Header().Set("ETag", `"stored"`)
		case http.MethodGet:
			w.Header().Set("Content-Length", fmt.Sprint(len(storedData)))
			_, _ = w.Write(storedData)
		default:
			t.Errorf("unexpected S3 method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	store := newTestObjectStore(t, server)
	store.emailEncryptionKeys = keys
	store.activeEmailKeyID = activeKeyID

	key, hash, err := store.Put(t.Context(), "household", mimeTypeEmail, plaintext)
	if err != nil || hash != DocumentHash(plaintext) || !strings.HasSuffix(key, "/originals/"+encryptedEmailPrefix+"key-1/"+emailFingerprint(firstKey, plaintext)) {
		t.Fatalf("encrypted upload failed: key=%q hash=%q err=%v", key, hash, err)
	}
	if !strings.HasPrefix(objectKey, "household/originals/"+encryptedEmailPrefix+"key-1/") || strings.Contains(objectKey, hash) {
		t.Fatalf("email object did not use the versioned key: %q", objectKey)
	}
	rotatedKeys, rotatedActiveKeyID, err := parseEmailEncryptionKeys("key-2:" + base64.StdEncoding.EncodeToString(secondKey) + ",key-1:" + base64.StdEncoding.EncodeToString(firstKey))
	if err != nil {
		t.Fatal(err)
	}
	if rotatedActiveKeyID != "key-2" {
		t.Fatalf("first keyring entry was not selected as active: %q", rotatedActiveKeyID)
	}
	store.emailEncryptionKeys = rotatedKeys
	store.activeEmailKeyID = rotatedActiveKeyID
	document, err := store.Get(t.Context(), key, mimeTypeEmail)
	if err != nil || !bytes.Equal(document.Data, plaintext) {
		t.Fatalf("encrypted original did not round-trip: document=%q err=%v", document.Data, err)
	}
	storedData[len(storedData)-1] ^= 1
	if _, err := store.Get(t.Context(), key, mimeTypeEmail); err == nil {
		t.Fatal("tampered encrypted original was accepted")
	}
}

func TestObjectStoreRejectsEmailPutWithoutEncryptionKey(t *testing.T) {
	t.Parallel()
	store := &ObjectStore{}
	if _, _, err := store.Put(t.Context(), "household", mimeTypeEmail, []byte("message")); err == nil {
		t.Fatal("email original was accepted without an encryption key")
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
