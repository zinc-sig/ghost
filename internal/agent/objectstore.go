package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectStore is the agent's view of object storage. Unlike
// internal/upload (upload-only, bucket fixed at configure time), agent
// activities address explicit buckets per call and also download.
// Activities depend on this interface so tests can substitute a fake.
type ObjectStore interface {
	// UploadFile uploads the file at path to bucket/key.
	UploadFile(ctx context.Context, bucket, key, path string) error
	// UploadBytes uploads data to bucket/key.
	UploadBytes(ctx context.Context, bucket, key string, data []byte) error
	// DownloadPrefix mirrors every object under bucket/prefix into
	// targetDir, returning the number of files and total bytes written.
	// Object keys that would escape targetDir are rejected.
	DownloadPrefix(ctx context.Context, bucket, prefix, targetDir string) (files int, bytes int64, err error)
}

// minioStore is the production ObjectStore backed by minio-go.
type minioStore struct {
	client *minio.Client
}

// uploadOptions bounds the memory one upload can hold. NumThreads 1 keeps
// a multipart upload to a single in-flight part, and PartSize pins the
// part to minio's minimum, because an upload of unknown size otherwise
// allocates a part buffer sized for a 5 TiB object. The agent's memory
// headroom in the container assumes one 16 MiB buffer per concurrent
// exec; the memory budgets section of README.md shows the arithmetic.
var uploadOptions = minio.PutObjectOptions{NumThreads: 1, PartSize: 16 << 20}

// newObjectStore builds the minio client from the agent config. An
// endpoint with an explicit http(s) scheme overrides the secure flag.
func newObjectStore(cfg *Config) (ObjectStore, error) {
	endpoint := cfg.StorageEndpoint
	secure := cfg.StorageSecure
	if u, err := url.Parse(endpoint); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		secure = u.Scheme == "https"
		endpoint = u.Host
		if endpoint == "" {
			return nil, fmt.Errorf("agent: invalid storage endpoint URL %q", cfg.StorageEndpoint)
		}
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.StorageAccessKey, cfg.StorageSecretKey, cfg.StorageSessionToken),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: failed to create object storage client: %w", err)
	}
	return &minioStore{client: client}, nil
}

func (s *minioStore) UploadFile(ctx context.Context, bucket, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("agent: failed to open %s for upload: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	// Size is known for regular files; for anything else (e.g. /dev/null
	// as stdin) fall back to streaming with unknown size.
	size := int64(-1)
	if st, err := f.Stat(); err == nil && st.Mode().IsRegular() {
		size = st.Size()
	}

	if _, err := s.client.PutObject(ctx, bucket, key, f, size, uploadOptions); err != nil {
		return fmt.Errorf("agent: failed to upload %s to %s/%s: %w", path, bucket, key, err)
	}
	return nil
}

func (s *minioStore) UploadBytes(ctx context.Context, bucket, key string, data []byte) error {
	_, err := s.client.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), uploadOptions)
	if err != nil {
		return fmt.Errorf("agent: failed to upload %d bytes to %s/%s: %w", len(data), bucket, key, err)
	}
	return nil
}

func (s *minioStore) DownloadPrefix(ctx context.Context, bucket, prefix, targetDir string) (int, int64, error) {
	files := 0
	var total int64

	// Drain the full object listing BEFORE downloading any object. minio's
	// ListObjects streams results over a single HTTP response fed into the
	// channel by a goroutine; doing per-object GetObject work *inside* the
	// range loop stalls that stream and can truncate the listing (a few
	// objects come back, the rest are silently dropped). Collecting the keys
	// first, then downloading, keeps the list stream short-lived and
	// complete. The child ctx is cancelled as soon as the drain finishes so
	// the list goroutine never leaks.
	var keys []string
	listCtx, cancel := context.WithCancel(ctx)
	for obj := range s.client.ListObjects(listCtx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			cancel()
			return files, total, fmt.Errorf("agent: failed to list %s/%s: %w", bucket, prefix, obj.Err)
		}
		// Skip directory marker objects.
		if strings.HasSuffix(obj.Key, "/") {
			continue
		}
		keys = append(keys, obj.Key)
	}
	cancel()

	for _, key := range keys {
		r, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
		if err != nil {
			return files, total, fmt.Errorf("agent: failed to get %s/%s: %w", bucket, key, err)
		}
		n, err := materializeObject(targetDir, prefix, key, r)
		_ = r.Close()
		if err != nil {
			return files, total, err
		}
		files++
		total += n
	}
	return files, total, nil
}

// objectDestination maps an object key under prefix to a path inside
// targetDir, with mandatory path-traversal defence: the relative path
// must be non-empty, non-absolute, and free of ".." escapes after
// cleaning, and the joined result must stay inside targetDir.
func objectDestination(targetDir, prefix, key string) (string, error) {
	rel := strings.TrimPrefix(key, prefix)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "", fmt.Errorf("agent: object key %q yields an empty path relative to prefix %q", key, prefix)
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("agent: object key %q yields an absolute path", key)
	}
	cleaned := filepath.Clean(rel)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("agent: object key %q escapes the target directory", key)
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return "", fmt.Errorf("agent: failed to resolve target dir %s: %w", targetDir, err)
	}
	dest := filepath.Join(absTarget, cleaned)
	relCheck, err := filepath.Rel(absTarget, dest)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("agent: object key %q escapes the target directory", key)
	}
	return dest, nil
}

// materializeObject writes one object's content to its traversal-safe
// destination under targetDir, creating parent directories (0755). It is
// shared by the real store and test fakes so the defence is uniform.
func materializeObject(targetDir, prefix, key string, r io.Reader) (int64, error) {
	dest, err := objectDestination(targetDir, prefix, key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, fmt.Errorf("agent: failed to create directory for %s: %w", dest, err)
	}
	f, err := os.Create(dest)
	if err != nil {
		return 0, fmt.Errorf("agent: failed to create %s: %w", dest, err)
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, fmt.Errorf("agent: failed to write %s: %w", dest, err)
	}
	return n, nil
}
