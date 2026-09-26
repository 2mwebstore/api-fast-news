// Package services holds business rules: workflow transitions, cache
// invalidation and the side effects of publishing.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/repositories"
	"github.com/cambodia-fast-news/backend/internal/telegram"
	"github.com/cambodia-fast-news/backend/internal/utils"
	"github.com/cambodia-fast-news/backend/internal/websocket"
)

var (
	ErrSlugTaken        = errors.New("an article with this slug already exists")
	ErrInvalidStatus    = errors.New("invalid status transition")
	ErrMissingForPublish = errors.New("article is missing fields required for publishing")
)

type ArticleService struct {
	db       *gorm.DB
	repo     *repositories.ArticleRepository
	cache    *cache.Cache
	hub      *websocket.Hub
	telegram *telegram.Service
	audit    *AuditService
	cfg      *config.Config
}

func NewArticleService(
	db *gorm.DB,
	repo *repositories.ArticleRepository,
	c *cache.Cache,
	hub *websocket.Hub,
	tg *telegram.Service,
	audit *AuditService,
	cfg *config.Config,
) *ArticleService {
	return &ArticleService{db: db, repo: repo, cache: c, hub: hub, telegram: tg, audit: audit, cfg: cfg}
}

// allowedTransitions encodes the workflow in §16. Anything not listed here is
// rejected, so an article cannot jump from draft straight to published without
// passing review.
var allowedTransitions = map[models.ArticleStatus][]models.ArticleStatus{
	models.StatusDraft:     {models.StatusReview, models.StatusArchived},
	models.StatusReview:    {models.StatusApproved, models.StatusRejected, models.StatusDraft},
	models.StatusRejected:  {models.StatusDraft, models.StatusReview},
	models.StatusApproved:  {models.StatusScheduled, models.StatusPublished, models.StatusReview},
	models.StatusScheduled: {models.StatusPublished, models.StatusApproved, models.StatusDraft},
	models.StatusPublished: {models.StatusArchived, models.StatusDraft},
	models.StatusArchived:  {models.StatusDraft, models.StatusPublished},
}

// CanTransition reports whether from -> to is a legal editorial move.
func CanTransition(from, to models.ArticleStatus) bool {
	if from == to {
		return true
	}
	for _, allowed := range allowedTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// ArticleInput is the payload accepted by create and update.
type ArticleInput struct {
	TitleKh   string `json:"titleKh" binding:"required,min=3"`
	TitleEn   string `json:"titleEn"`
	Slug      string `json:"slug"`
	SummaryKh string `json:"summaryKh"`
	SummaryEn string `json:"summaryEn"`
	ContentKh string `json:"contentKh"`
	ContentEn string `json:"contentEn"`
	ContentJSON models.JSONMap `json:"contentJson"`

	CategoryID uint   `json:"categoryId" binding:"required"`
	AuthorID   *uint  `json:"authorId"`
	TagIDs     []uint `json:"tagIds"`

	ImageURL     string `json:"imageUrl"`
	ImageAltKh   string `json:"imageAltKh"`
	ImageAltEn   string `json:"imageAltEn"`
	ImageWidth   int    `json:"imageWidth"`
	ImageHeight  int    `json:"imageHeight"`
	ImageCaption string `json:"imageCaption"`
	ImageIsAIGenerated bool `json:"imageIsAiGenerated"`

	ContentType models.ContentType `json:"contentType"`
	SponsorName string             `json:"sponsorName"`
	SponsorURL  string             `json:"sponsorUrl"`

	IsBreaking bool       `json:"isBreaking"`
	IsFeatured bool       `json:"isFeatured"`
	IsPinned   bool       `json:"isPinned"`
	ScheduledAt *time.Time `json:"scheduledAt"`

	AIAssisted bool `json:"aiAssisted"`
}

// Create stores a new draft. New articles always start at draft regardless of
// what the client sends — status is only ever changed through Transition.
func (s *ArticleService) Create(ctx context.Context, in ArticleInput, userID *uint) (*models.Article, error) {
	slug, err := s.resolveSlug(ctx, in.Slug, in.TitleEn, in.TitleKh, 0)
	if err != nil {
		return nil, err
	}

	article := &models.Article{
		Slug:        slug,
		Status:      models.StatusDraft,
		CreatedByID: userID,
	}
	s.applyInput(article, in)

	if err := s.repo.Create(ctx, article); err != nil {
		return nil, fmt.Errorf("create article: %w", err)
	}
	if len(in.TagIDs) > 0 {
		if err := s.syncTags(ctx, article, in.TagIDs); err != nil {
			slog.Warn("could not attach tags", "article", article.ID, "error", err)
		}
	}

	s.audit.Record(ctx, userID, "article.create", "article", &article.ID, article.TitleKh, nil)
	return s.repo.GetByID(ctx, article.ID)
}

// Update edits an article. When the article is already published, a revision
// snapshot is taken first so the change is always reconstructable (§20).
func (s *ArticleService) Update(ctx context.Context, id uint, in ArticleInput, userID *uint, editorName string) (*models.Article, error) {
	article, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if article.Status == models.StatusPublished {
		if err := s.snapshot(ctx, article, userID, editorName, "edit after publication"); err != nil {
			// Refuse the edit rather than lose the audit trail — an
			// unrecorded change to a published story is exactly what §20 exists
			// to prevent.
			return nil, fmt.Errorf("could not record revision, edit aborted: %w", err)
		}
	}

	if in.Slug != "" && in.Slug != article.Slug {
		slug, err := s.resolveSlug(ctx, in.Slug, in.TitleEn, in.TitleKh, article.ID)
		if err != nil {
			return nil, err
		}
		// A published slug change breaks every existing link, so leave a
		// redirect behind (§61).
		if article.Status == models.StatusPublished {
			s.createSlugRedirect(ctx, article.Slug, slug, userID)
		}
		article.Slug = slug
	}

	s.applyInput(article, in)
	now := time.Now().UTC()
	article.UpdatedContentAt = &now

	if err := s.repo.Save(ctx, article); err != nil {
		return nil, fmt.Errorf("update article: %w", err)
	}
	if in.TagIDs != nil {
		if err := s.syncTags(ctx, article, in.TagIDs); err != nil {
			slog.Warn("could not sync tags", "article", article.ID, "error", err)
		}
	}

	s.cache.InvalidateArticle(ctx, article.Slug)
	s.audit.Record(ctx, userID, "article.update", "article", &article.ID, article.TitleKh, nil)
	return s.repo.GetByID(ctx, article.ID)
}

// applyInput copies editable fields onto the model. Status, counters and
// publication timestamps are deliberately not settable from client input.
func (s *ArticleService) applyInput(a *models.Article, in ArticleInput) {
	a.TitleKh = strings.TrimSpace(in.TitleKh)
	a.TitleEn = strings.TrimSpace(in.TitleEn)
	a.SummaryKh = strings.TrimSpace(in.SummaryKh)
	a.SummaryEn = strings.TrimSpace(in.SummaryEn)
	a.ContentKh = utils.SanitizeHTML(in.ContentKh)
	a.ContentEn = utils.SanitizeHTML(in.ContentEn)
	a.ContentJSON = in.ContentJSON
	a.CategoryID = in.CategoryID
	a.AuthorID = in.AuthorID

	a.ImageURL = in.ImageURL
	a.ImageAltKh = in.ImageAltKh
	a.ImageAltEn = in.ImageAltEn
	a.ImageWidth = in.ImageWidth
	a.ImageHeight = in.ImageHeight
	a.ImageCaption = in.ImageCaption
	a.ImageIsAIGenerated = in.ImageIsAIGenerated

	a.ContentType = in.ContentType
	if a.ContentType == "" {
		a.ContentType = models.ContentEditorial
	}
	a.SponsorName = in.SponsorName
	a.SponsorURL = in.SponsorURL

	a.IsFeatured = in.IsFeatured
	a.IsPinned = in.IsPinned
	a.ScheduledAt = in.ScheduledAt
	if in.AIAssisted {
		a.AIAssisted = true
	}

	// Breaking is a state with a start time, not just a boolean.
	now := time.Now().UTC()
	if in.IsBreaking && !a.IsBreaking {
		a.IsBreaking = true
		a.BreakingStartedAt = &now
		a.BreakingEndedAt = nil
	} else if !in.IsBreaking && a.IsBreaking {
		a.IsBreaking = false
		a.BreakingEndedAt = &now
	}

	// A summary is what the meta description and cards fall back to, so derive
	// one when the editor left it blank.
	if a.SummaryKh == "" && a.ContentKh != "" {
		a.SummaryKh = utils.Truncate(utils.StripHTML(a.ContentKh), 200)
	}
	a.WordCount = utils.CountWords(utils.StripHTML(a.ContentKh) + " " + utils.StripHTML(a.ContentEn))
	a.ReadingMinutes = utils.ReadingMinutes(a.WordCount)
}

// Transition moves an article through the editorial workflow and fires the
// side effects that belong to the target state.
func (s *ArticleService) Transition(ctx context.Context, id uint, to models.ArticleStatus, userID *uint, editorName string) (*models.Article, error) {
	article, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !CanTransition(article.Status, to) {
		return nil, fmt.Errorf("%w: %s to %s", ErrInvalidStatus, article.Status, to)
	}

	now := time.Now().UTC()
	from := article.Status

	switch to {
	case models.StatusPublished:
		if err := s.validateForPublish(article); err != nil {
			return nil, err
		}
		if article.PublishedAt == nil {
			article.PublishedAt = &now
		}
		article.ApprovedByID = userID
		article.ScheduledAt = nil

	case models.StatusScheduled:
		if article.ScheduledAt == nil || article.ScheduledAt.Before(now) {
			return nil, fmt.Errorf("%w: a future scheduled time is required", ErrInvalidStatus)
		}
		if err := s.validateForPublish(article); err != nil {
			return nil, err
		}

	case models.StatusApproved:
		article.ApprovedByID = userID

	case models.StatusArchived:
		// Archiving must also clear any live breaking flag, or the red bar
		// keeps pointing at a story no longer on the site.
		if article.IsBreaking {
			article.IsBreaking = false
			article.BreakingEndedAt = &now
		}
	}

	article.Status = to
	if err := s.repo.Save(ctx, article); err != nil {
		return nil, fmt.Errorf("save status: %w", err)
	}

	s.cache.InvalidateArticle(ctx, article.Slug)
	s.audit.Record(ctx, userID, "article."+string(to), "article", &article.ID,
		fmt.Sprintf("%s: %s -> %s", article.TitleKh, from, to), nil)

	if to == models.StatusPublished {
		s.afterPublish(ctx, article, userID)
	}
	return s.repo.GetByID(ctx, article.ID)
}

// validateForPublish enforces the minimum an article needs before readers and
// search engines see it. This is the machine-checkable subset of the §59
// editorial checklist — the rest of that checklist is a human judgement, which
// is why the UI shows it as a list rather than a score.
func (s *ArticleService) validateForPublish(a *models.Article) error {
	var missing []string
	if strings.TrimSpace(a.TitleKh) == "" {
		missing = append(missing, "Khmer headline")
	}
	if strings.TrimSpace(a.ContentKh) == "" {
		missing = append(missing, "article body")
	}
	if a.CategoryID == 0 {
		missing = append(missing, "category")
	}
	if strings.TrimSpace(a.SummaryKh) == "" {
		missing = append(missing, "summary")
	}
	if a.ImageURL != "" && strings.TrimSpace(a.ImageAltKh) == "" && strings.TrimSpace(a.ImageAltEn) == "" {
		missing = append(missing, "image ALT text")
	}
	if a.ContentType.RequiresDisclosure() && strings.TrimSpace(a.SponsorName) == "" {
		// Without a sponsor name the disclosure label would be empty, and paid
		// content would render as ordinary reporting (§42).
		missing = append(missing, "sponsor name (required for sponsored content)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingForPublish, strings.Join(missing, ", "))
	}
	return nil
}

// afterPublish runs the distribution side effects. Each is best-effort and
// logged: a Telegram outage must not roll back a published story.
func (s *ArticleService) afterPublish(ctx context.Context, a *models.Article, userID *uint) {
	if a.IsLiveBreaking(time.Now().UTC()) {
		s.hub.Publish(websocket.EventBreaking, breakingPayload(a, s.cfg.App.URL))
	} else {
		s.hub.Publish(websocket.EventArticle, breakingPayload(a, s.cfg.App.URL))
	}

	if s.telegram.Configured(ctx) && s.telegram.AutoPublishEnabled(ctx) {
		go s.PublishToTelegram(context.WithoutCancel(ctx), a, userID)
	}
}

// PublishToTelegram posts one article to the channel and records the attempt.
// Exported so the admin "Manual Publish" action (§70) can call it directly.
func (s *ArticleService) PublishToTelegram(ctx context.Context, a *models.Article, userID *uint) {
	kind := telegram.KindNormal
	if a.IsLiveBreaking(time.Now().UTC()) {
		kind = telegram.KindBreaking
	} else if a.Category != nil && a.Category.Slug == "sports" {
		kind = telegram.KindSports
	}

	msg := telegram.Message{
		Kind:     kind,
		Headline: a.TitleKh,
		Summary:  a.SummaryKh,
		URL:      fmt.Sprintf("%s/news/%s", s.cfg.App.URL, a.Slug),
		ImageURL: a.ImageURL,
	}

	post := models.TelegramPost{
		ArticleID:     &a.ID,
		Kind:          string(kind),
		Text:          msg.Compose(),
		ImageURL:      a.ImageURL,
		Status:        "pending",
		TriggeredByID: userID,
	}
	if err := s.db.WithContext(ctx).Create(&post).Error; err != nil {
		slog.Error("could not record telegram post", "article", a.ID, "error", err)
		return
	}

	messageID, err := s.telegram.Send(ctx, msg)
	post.Attempts++
	if err != nil {
		post.Status = "failed"
		post.ErrorMessage = utils.Truncate(err.Error(), 500)
		slog.Error("telegram publish failed", "article", a.ID, "error", err)
	} else {
		now := time.Now().UTC()
		post.Status = "sent"
		post.MessageID = messageID
		post.SentAt = &now
	}
	if err := s.db.WithContext(ctx).Save(&post).Error; err != nil {
		slog.Error("could not update telegram post", "post", post.ID, "error", err)
	}
}

// SetBreaking raises or retires the breaking flag and pushes the change to
// every connected reader without a refresh (§7).
func (s *ArticleService) SetBreaking(ctx context.Context, id uint, on bool, userID *uint) (*models.Article, error) {
	article, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if on {
		article.IsBreaking = true
		article.BreakingStartedAt = &now
		article.BreakingEndedAt = nil
	} else {
		article.IsBreaking = false
		article.BreakingEndedAt = &now
	}
	if err := s.repo.Save(ctx, article); err != nil {
		return nil, fmt.Errorf("set breaking: %w", err)
	}

	s.cache.Delete(ctx, cache.KeyBreaking)
	s.cache.InvalidateArticle(ctx, article.Slug)

	if on && article.Status == models.StatusPublished {
		s.hub.Publish(websocket.EventBreaking, breakingPayload(article, s.cfg.App.URL))
	} else {
		s.hub.Publish(websocket.EventBreakingEnded, map[string]any{"articleId": article.ID})
	}

	s.audit.Record(ctx, userID, "article.breaking", "article", &article.ID,
		fmt.Sprintf("breaking=%v: %s", on, article.TitleKh), nil)
	return article, nil
}

// PublishDue promotes scheduled articles whose time has come. Called on a
// ticker from main.
func (s *ArticleService) PublishDue(ctx context.Context) int {
	due, err := s.repo.DueForPublish(ctx, time.Now().UTC())
	if err != nil {
		slog.Error("could not load scheduled articles", "error", err)
		return 0
	}
	published := 0
	for i := range due {
		if _, err := s.Transition(ctx, due[i].ID, models.StatusPublished, nil, "scheduler"); err != nil {
			slog.Error("scheduled publish failed", "article", due[i].ID, "error", err)
			continue
		}
		published++
	}
	if published > 0 {
		slog.Info("published scheduled articles", "count", published)
	}
	return published
}

// AddCorrection publishes a reader-facing correction and snapshots the version
// that preceded it (§20).
func (s *ArticleService) AddCorrection(ctx context.Context, articleID uint, noteKh, noteEn, reason string, userID *uint, editorName string) (*models.ArticleCorrection, error) {
	article, err := s.repo.GetByID(ctx, articleID)
	if err != nil {
		return nil, err
	}
	if err := s.snapshot(ctx, article, userID, editorName, reason); err != nil {
		return nil, fmt.Errorf("could not record revision: %w", err)
	}

	correction := &models.ArticleCorrection{
		ArticleID:   articleID,
		NoteKh:      noteKh,
		NoteEn:      noteEn,
		Reason:      reason,
		CorrectedAt: time.Now().UTC(),
		EditorID:    userID,
		EditorName:  editorName,
	}
	if err := s.db.WithContext(ctx).Create(correction).Error; err != nil {
		return nil, fmt.Errorf("create correction: %w", err)
	}

	s.cache.InvalidateArticle(ctx, article.Slug)
	s.audit.Record(ctx, userID, "article.correction", "article", &articleID, reason, nil)
	return correction, nil
}

// snapshot writes an immutable copy of the article's current state.
func (s *ArticleService) snapshot(ctx context.Context, a *models.Article, userID *uint, editorName, reason string) error {
	var version int
	err := s.db.WithContext(ctx).Model(&models.ArticleRevision{}).
		Where("article_id = ?", a.ID).
		Select("COALESCE(MAX(version), 0)").Scan(&version).Error
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).Create(&models.ArticleRevision{
		ArticleID:  a.ID,
		Version:    version + 1,
		TitleKh:    a.TitleKh,
		SummaryKh:  a.SummaryKh,
		ContentKh:  a.ContentKh,
		Status:     a.Status,
		EditorID:   userID,
		EditorName: editorName,
		Reason:     reason,
	}).Error
}

// resolveSlug picks a slug and guarantees it is unique.
func (s *ArticleService) resolveSlug(ctx context.Context, requested, titleEn, titleKh string, exceptID uint) (string, error) {
	base := utils.Slugify(requested)
	if base == "" {
		base = utils.SlugifyWithFallback(titleEn, titleKh, time.Now().UTC().Format("20060102"))
	}

	candidate := base
	for attempt := 0; attempt < 12; attempt++ {
		taken, err := s.repo.SlugExists(ctx, candidate, exceptID)
		if err != nil {
			return "", fmt.Errorf("check slug: %w", err)
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%s", base, utils.RandomHex(3))
	}
	return "", ErrSlugTaken
}

// createSlugRedirect leaves a 301 behind when a published article moves.
func (s *ArticleService) createSlugRedirect(ctx context.Context, oldSlug, newSlug string, userID *uint) {
	redirect := models.Redirect{
		FromPath:    "/news/" + oldSlug,
		ToPath:      "/news/" + newSlug,
		StatusCode:  301,
		IsActive:    true,
		CreatedByID: userID,
		Note:        "automatic: article slug changed",
	}
	// A duplicate here means the old path already redirects somewhere, which
	// is fine to leave alone.
	if err := s.db.WithContext(ctx).Where("from_path = ?", redirect.FromPath).
		FirstOrCreate(&redirect).Error; err != nil {
		slog.Warn("could not create slug redirect", "from", oldSlug, "error", err)
	}
}

func (s *ArticleService) syncTags(ctx context.Context, a *models.Article, tagIDs []uint) error {
	var tags []models.Tag
	if len(tagIDs) > 0 {
		if err := s.db.WithContext(ctx).Find(&tags, tagIDs).Error; err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Model(a).Association("Tags").Replace(tags)
}

// breakingPayload is the compact shape pushed over the socket — just enough to
// render the red bar without another API call.
func breakingPayload(a *models.Article, siteURL string) map[string]any {
	category := ""
	if a.Category != nil {
		category = a.Category.NameKh
	}
	return map[string]any{
		"id":          a.ID,
		"slug":        a.Slug,
		"titleKh":     a.TitleKh,
		"titleEn":     a.TitleEn,
		"summaryKh":   a.SummaryKh,
		"imageUrl":    a.ImageURL,
		"category":    category,
		"url":         fmt.Sprintf("%s/news/%s", siteURL, a.Slug),
		"publishedAt": a.PublishedAt,
		"isBreaking":  a.IsBreaking,
	}
}
