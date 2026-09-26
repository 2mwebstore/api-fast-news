package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// AuditService appends privileged-action records (§66, §79).
type AuditService struct{ db *gorm.DB }

func NewAuditService(db *gorm.DB) *AuditService { return &AuditService{db: db} }

// Record writes one audit row. Failures are logged but never propagate: losing
// an audit line is bad, but failing the editor's action because of it is worse,
// and the error log preserves the evidence either way.
//
// Callers must not pass credentials or token material in changes — this row is
// readable by anyone with users.manage.
func (s *AuditService) Record(ctx context.Context, userID *uint, action, entityType string, entityID *uint, summary string, changes models.JSONMap) {
	entry := models.AuditLog{
		UserID:     userID,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Summary:    utils.Truncate(summary, 500),
		Changes:    changes,
	}
	if err := s.db.WithContext(ctx).Create(&entry).Error; err != nil {
		slog.Error("could not write audit log", "action", action, "error", err)
	}
}

// RecordRequest is Record with the request context attached, used from HTTP
// handlers where the IP and user agent are available.
func (s *AuditService) RecordRequest(ctx context.Context, userID *uint, email, action, entityType string, entityID *uint, summary, ip, userAgent string) {
	entry := models.AuditLog{
		UserID:     userID,
		UserEmail:  email,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Summary:    utils.Truncate(summary, 500),
		IPAddress:  ip,
		UserAgent:  utils.Truncate(userAgent, 250),
	}
	if err := s.db.WithContext(ctx).Create(&entry).Error; err != nil {
		slog.Error("could not write audit log", "action", action, "error", err)
	}
}

// List returns audit entries for the admin log view, newest first.
func (s *AuditService) List(ctx context.Context, page, limit int) ([]models.AuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}

	var total int64
	if err := s.db.WithContext(ctx).Model(&models.AuditLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var entries []models.AuditLog
	err := s.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(limit).Offset((page - 1) * limit).
		Find(&entries).Error
	return entries, total, err
}

// RetentionOption is one of the retention windows the admin offers.
type RetentionOption struct {
	Months int    `json:"months"`
	Label  string `json:"label"`
}

// RetentionOptions are the windows an operator can choose from. Deliberately
// short and fixed: a free-form date lets somebody delete yesterday's entries,
// which is exactly what an audit log exists to prevent.
func RetentionOptions() []RetentionOption {
	return []RetentionOption{
		{Months: 3, Label: "Keep the last 3 months"},
		{Months: 2, Label: "Keep the last 2 months"},
		{Months: 1, Label: "Keep the last month"},
	}
}

// Stats reports how much of the log each retention window would remove, so the
// operator sees the cost before committing rather than after.
func (s *AuditService) Stats(ctx context.Context) (map[string]any, error) {
	var total int64
	if err := s.db.WithContext(ctx).Model(&models.AuditLog{}).Count(&total).Error; err != nil {
		return nil, err
	}

	var oldest time.Time
	// Scan into a nullable value: an empty table has no oldest row, and MySQL
	// returns NULL rather than a zero time.
	var oldestRow *time.Time
	s.db.WithContext(ctx).Model(&models.AuditLog{}).
		Select("MIN(created_at)").Scan(&oldestRow)
	if oldestRow != nil {
		oldest = *oldestRow
	}

	windows := make([]map[string]any, 0, len(RetentionOptions()))
	for _, opt := range RetentionOptions() {
		cutoff := time.Now().UTC().AddDate(0, -opt.Months, 0)
		var removable int64
		s.db.WithContext(ctx).Model(&models.AuditLog{}).
			Where("created_at < ?", cutoff).Count(&removable)
		windows = append(windows, map[string]any{
			"months": opt.Months, "label": opt.Label,
			"cutoff": cutoff, "removable": removable,
		})
	}

	out := map[string]any{"total": total, "windows": windows}
	if !oldest.IsZero() {
		out["oldest"] = oldest
	}
	return out, nil
}

// Purge deletes entries older than the given number of months and returns how
// many rows went.
//
// The purge itself is recorded first, so the log always contains the reason its
// own history is shorter than expected — a retention run that left no trace
// would be indistinguishable from someone covering their tracks.
func (s *AuditService) Purge(ctx context.Context, months int, userID *uint) (int64, error) {
	allowed := false
	for _, opt := range RetentionOptions() {
		if opt.Months == months {
			allowed = true
			break
		}
	}
	if !allowed {
		return 0, fmt.Errorf("unsupported retention window: %d months", months)
	}

	cutoff := time.Now().UTC().AddDate(0, -months, 0)

	var removable int64
	if err := s.db.WithContext(ctx).Model(&models.AuditLog{}).
		Where("created_at < ?", cutoff).Count(&removable).Error; err != nil {
		return 0, err
	}

	s.Record(ctx, userID, "audit.purge", "audit_log", nil,
		fmt.Sprintf("kept %d months, removed %d entries", months, removable),
		models.JSONMap{"months": months, "cutoff": cutoff, "removed": removable})

	// Unscoped is not needed — AuditLog has no soft-delete column — but the
	// cutoff is applied again here rather than reusing a count, so a row written
	// between the count and the delete is handled consistently.
	result := s.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&models.AuditLog{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
