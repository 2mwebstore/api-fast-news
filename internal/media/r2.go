// Package media stores uploads in Cloudflare R2 over its S3-compatible API (§4).
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

var (
	ErrNotConfigured = errors.New("media storage is not configured")
	ErrTooLarge      = errors.New("file is too large")
	ErrTypeRejected  = errors.New("file type is not allowed")
)

// Size ceilings per §74.
const (
	MaxImageBytes = 10 << 20  // 10 MB
	MaxVideoBytes = 512 << 20 // 512 MB
)

// allowedTypes maps a sniffed content type to the extension we store it under.
// The map is the allowlist: anything not present is rejected.
var allowedTypes = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"image/avif":      ".avif",
	"image/gif":       ".gif",
	"video/mp4":       ".mp4",
	"video/webm":      ".webm",
	"video/quicktime": ".mov",
	"application/pdf": ".pdf",
}

// Folder namespaces objects inside the bucket.
type Folder string

const (
	FolderArticle Folder = "article"
	FolderAuthor  Folder = "author"
	FolderAd      Folder = "ad"
	FolderVideo   Folder = "video"
	FolderTip     Folder = "tip"
)

// Upload is the result of storing one object.
type Upload struct {
	Key       string `json:"key"`
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mimeType"`
	SizeBytes int64  `json:"sizeBytes"`
}

type Service struct {
	cfg    config.R2
	client *s3.Client
}

func NewService(cfg *config.Config) *Service {
	s := &Service{cfg: cfg.R2}
	if !s.cfg.Configured() {
		return s
	}

	s.client = s3.New(s3.Options{
		// R2 ignores the region but the SDK requires one.
		Region:       "auto",
		BaseEndpoint: aws.String(s.cfg.Endpoint()),
		Credentials: credentials.NewStaticCredentialsProvider(
			s.cfg.AccessKeyID, s.cfg.SecretAccessKey, "",
		),
		// R2 does not support the streaming-trailer checksums the v2 SDK sends
		// by default; without this, every PUT is rejected.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return s
}

func (s *Service) Configured() bool { return s.client != nil }

// UploadMultipart validates and stores a browser upload.
//
// The client-supplied filename and Content-Type are treated as untrusted (§74):
// the type is determined by sniffing the first 512 bytes, the extension comes
// from that sniffed type, and the stored object key is generated, never taken
// from the upload.
func (s *Service) UploadMultipart(ctx context.Context, header *multipart.FileHeader, folder Folder) (*Upload, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}

	file, err := header.Open()
	if err != nil {
		return nil, fmt.Errorf("open upload: %w", err)
	}
	defer file.Close()

	// Read the whole object into memory so the type can be sniffed before
	// anything is written, and so the size limit is enforced against actual
	// bytes rather than a client-declared Content-Length.
	limit := int64(MaxImageBytes)
	if folder == FolderVideo {
		limit = MaxVideoBytes
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: limit is %d MB", ErrTooLarge, limit>>20)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: file is empty", ErrTypeRejected)
	}

	sniffed := http.DetectContentType(data)
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = strings.TrimSpace(sniffed[:i])
	}
	ext, ok := allowedTypes[sniffed]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTypeRejected, sniffed)
	}
	// A video-sized upload must actually be a video, and vice versa — this
	// stops an "image" upload slipping through the larger video ceiling.
	isVideo := strings.HasPrefix(sniffed, "video/")
	if isVideo != (folder == FolderVideo) {
		return nil, fmt.Errorf("%w: %s does not belong in the %s folder", ErrTypeRejected, sniffed, folder)
	}

	key := objectKey(folder, header.Filename, ext)

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(sniffed),
		// Media is immutable once written — the key carries a random suffix —
		// so it can be cached hard at the edge.
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return nil, fmt.Errorf("upload to r2: %w", err)
	}

	return &Upload{
		Key:       key,
		URL:       s.PublicURL(key),
		Filename:  path.Base(header.Filename),
		MimeType:  sniffed,
		SizeBytes: int64(len(data)),
	}, nil
}

// objectKey builds a collision-proof, date-partitioned key. The original
// filename contributes only a sanitised slug, so a path traversal or a
// look-alike extension in the upload name cannot reach the bucket.
func objectKey(folder Folder, originalName, ext string) string {
	base := strings.TrimSuffix(path.Base(originalName), path.Ext(originalName))
	slug := utils.Slugify(base)
	if slug == "" {
		slug = "file"
	}
	if len([]rune(slug)) > 40 {
		slug = string([]rune(slug)[:40])
	}
	now := time.Now().UTC()
	return fmt.Sprintf("%s/%d/%02d/%s-%s%s", folder, now.Year(), now.Month(), slug, utils.RandomHex(6), ext)
}

// PublicURL maps an object key to its CDN URL.
func (s *Service) PublicURL(key string) string {
	if s.cfg.PublicURL == "" {
		return key
	}
	return s.cfg.PublicURL + "/" + strings.TrimLeft(key, "/")
}

// Delete removes an object.
func (s *Service) Delete(ctx context.Context, key string) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete from r2: %w", err)
	}
	return nil
}
