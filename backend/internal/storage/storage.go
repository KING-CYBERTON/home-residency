// Package storage holds the document vault's file store. Files never pass through the API: clients
// upload and download with short-lived presigned URLs, and the API only checks what arrived.
package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var ErrNotFound = errors.New("object not found")

type Store interface {
	// PresignPut returns a URL the client PUTs the file to, and headers it must send with it.
	PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, map[string]string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	// Head reports the stored object's size and content type, or ErrNotFound.
	Head(ctx context.Context, key string) (int64, string, error)
}

// ---- S3 (SSE-KMS)

type S3 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
	kmsKey  string // empty uses the bucket default key
}

func NewS3(ctx context.Context, bucket, kmsKey string) (*S3, error) {
	if bucket == "" {
		return nil, errors.New("storage: STORAGE_BUCKET is required for the s3 driver")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	c := s3.NewFromConfig(cfg)
	return &S3{client: c, presign: s3.NewPresignClient(c), bucket: bucket, kmsKey: kmsKey}, nil
}

func (s *S3) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, map[string]string, error) {
	in := &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, ContentType: &contentType, ContentLength: aws.Int64(size),
		ServerSideEncryption: types.ServerSideEncryptionAwsKms,
	}
	headers := map[string]string{"Content-Type": contentType, "x-amz-server-side-encryption": "aws:kms"}
	if s.kmsKey != "" {
		in.SSEKMSKeyId = &s.kmsKey
		headers["x-amz-server-side-encryption-aws-kms-key-id"] = s.kmsKey
	}
	req, err := s.presign.PresignPutObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", nil, err
	}
	return req.URL, headers, nil
}

func (s *S3) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *S3) Head(ctx context.Context, key string) (int64, string, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		var nf *types.NotFound
		if errors.As(err, &nf) {
			return 0, "", ErrNotFound
		}
		return 0, "", err
	}
	return aws.ToInt64(out.ContentLength), aws.ToString(out.ContentType), nil
}

// ---- Local disk, for development and tests only

// Local keeps files in a directory and serves them from /storage on the API itself, with HMAC-signed
// URLs shaped like S3's. It is refused in production by config.
type Local struct {
	Dir     string
	BaseURL string // externally visible origin of the API
	Secret  []byte
	Now     func() time.Time
}

func (l *Local) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if strings.Contains(key, "..") || clean == "/" {
		return "", errors.New("bad key")
	}
	return filepath.Join(l.Dir, clean), nil
}

func (l *Local) sign(method, key, contentType string, size int64, exp int64) string {
	m := hmac.New(sha256.New, l.Secret)
	fmt.Fprintf(m, "%s\n%s\n%s\n%d\n%d", method, key, contentType, size, exp)
	return hex.EncodeToString(m.Sum(nil))
}

func (l *Local) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Local) url(method, key, contentType string, size int64, ttl time.Duration) string {
	exp := l.now().Add(ttl).Unix()
	q := url.Values{"exp": {strconv.FormatInt(exp, 10)}, "sig": {l.sign(method, key, contentType, size, exp)}}
	if method == http.MethodPut {
		q.Set("type", contentType)
		q.Set("size", strconv.FormatInt(size, 10))
	}
	return strings.TrimRight(l.BaseURL, "/") + "/storage/" + key + "?" + q.Encode()
}

func (l *Local) PresignPut(_ context.Context, key, contentType string, size int64, ttl time.Duration) (string, map[string]string, error) {
	return l.url(http.MethodPut, key, contentType, size, ttl), map[string]string{"Content-Type": contentType}, nil
}

func (l *Local) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return l.url(http.MethodGet, key, "", 0, ttl), nil
}

func (l *Local) Head(_ context.Context, key string) (int64, string, error) {
	p, err := l.path(key)
	if err != nil {
		return 0, "", err
	}
	st, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", err
	}
	ct, _ := os.ReadFile(p + ".type")
	return st.Size(), string(ct), nil
}

// Handler serves GET and PUT on /storage/{key}. Mount it at "/storage/".
func (l *Local) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/storage/")
		q := r.URL.Query()
		exp, _ := strconv.ParseInt(q.Get("exp"), 10, 64)
		size, _ := strconv.ParseInt(q.Get("size"), 10, 64)
		ct := q.Get("type")
		if exp < l.now().Unix() || !hmac.Equal([]byte(q.Get("sig")), []byte(l.sign(r.Method, key, ct, size, exp))) {
			http.Error(w, "link expired or invalid", http.StatusForbidden)
			return
		}
		p, err := l.path(key)
		if err != nil {
			http.Error(w, "bad key", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != ct {
				http.Error(w, "content type does not match the upload ticket", http.StatusBadRequest)
				return
			}
			_ = os.MkdirAll(filepath.Dir(p), 0o700)
			f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				http.Error(w, "cannot store", http.StatusInternalServerError)
				return
			}
			defer f.Close()
			n, err := io.Copy(f, io.LimitReader(r.Body, size+1))
			if err != nil || n > size {
				_ = os.Remove(p)
				http.Error(w, "file larger than declared", http.StatusRequestEntityTooLarge)
				return
			}
			_ = os.WriteFile(p+".type", []byte(ct), 0o600)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			ct, _ := os.ReadFile(p + ".type")
			if len(ct) > 0 {
				w.Header().Set("Content-Type", string(ct))
			}
			w.Header().Set("Content-Disposition", "attachment")
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, p)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
