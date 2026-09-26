package controllers

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/ai"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// AIController exposes the newsroom assistants (§21–§25).
//
// Every endpoint here returns a draft for a human to review. None of them
// writes to the articles table, changes a status, or triggers distribution —
// that separation is the whole point of §21.
type AIController struct {
	ai    *ai.Service
	db    *gorm.DB
	audit *services.AuditService
}

func NewAIController(svc *ai.Service, db *gorm.DB, audit *services.AuditService) *AIController {
	return &AIController{ai: svc, db: db, audit: audit}
}

// aiDisclaimer accompanies every response so the wording in the UI cannot
// drift from the policy.
const aiDisclaimer = "AI-generated draft. Verify every fact against your sources before publishing. This draft has not been published and cannot publish itself."

// GenerateNews handles POST /api/ai/generate-news (§22).
func (ctl *AIController) GenerateNews(c *gin.Context) {
	if !ctl.ai.Configured() {
		httpx.Fail(c, 503, httpx.CodeAINotConfigured,
			"The AI assistant is not configured. Set ANTHROPIC_API_KEY to enable it.")
		return
	}

	var in ai.BreakingDraftInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation,
			"A topic, the verified facts, and a source are all required")
		return
	}

	draft, err := ctl.ai.GenerateBreakingDraft(c.Request.Context(), in)
	if err != nil {
		ctl.failAI(c, err)
		return
	}

	userID := middleware.CurrentUserID(c)
	ctl.audit.Record(c.Request.Context(), userID, "ai.generate_news", "article", nil, in.Topic, nil)

	httpx.OK(c, gin.H{
		"draft":      draft,
		"status":     "draft",
		"disclaimer": aiDisclaimer,
		"source":     in.Source,
	})
}

// GenerateSEO handles POST /api/ai/generate-seo (§23).
func (ctl *AIController) GenerateSEO(c *gin.Context) {
	if !ctl.ai.Configured() {
		httpx.Fail(c, 503, httpx.CodeAINotConfigured, "The AI assistant is not configured")
		return
	}

	var req struct {
		ArticleID uint   `json:"articleId"`
		TitleKh   string `json:"titleKh"`
		TitleEn   string `json:"titleEn"`
		Summary   string `json:"summary"`
		Body      string `json:"body"`
		Category  string `json:"category"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid request")
		return
	}

	// When an article id is given, read the text from the database rather than
	// trusting whatever the client posted.
	if req.ArticleID > 0 {
		var article models.Article
		err := ctl.db.WithContext(c.Request.Context()).Preload("Category").
			First(&article, req.ArticleID).Error
		if err != nil {
			httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
			return
		}
		req.TitleKh, req.TitleEn = article.TitleKh, article.TitleEn
		req.Summary, req.Body = article.SummaryKh, article.ContentKh
		if article.Category != nil {
			req.Category = article.Category.NameEn
		}
	}
	if req.TitleKh == "" && req.TitleEn == "" {
		httpx.BadRequest(c, httpx.CodeValidation, "An article or a headline is required")
		return
	}

	draft, err := ctl.ai.GenerateSEO(c.Request.Context(), ai.SEOInput{
		TitleKh: req.TitleKh, TitleEn: req.TitleEn,
		Summary: req.Summary, Body: req.Body, Category: req.Category,
	})
	if err != nil {
		ctl.failAI(c, err)
		return
	}

	httpx.OK(c, gin.H{
		"draft":      draft,
		"disclaimer": "AI-suggested metadata. Review and edit before saving — it is stored as unreviewed until you do.",
		"reviewed":   false,
	})
}

// GenerateSummary handles POST /api/ai/generate-summary (§24).
func (ctl *AIController) GenerateSummary(c *gin.Context) {
	if !ctl.ai.Configured() {
		httpx.Fail(c, 503, httpx.CodeAINotConfigured, "The AI assistant is not configured")
		return
	}

	var req struct {
		ArticleID uint   `json:"articleId"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Save      bool   `json:"save"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid request")
		return
	}

	var article *models.Article
	if req.ArticleID > 0 {
		var found models.Article
		if err := ctl.db.WithContext(c.Request.Context()).First(&found, req.ArticleID).Error; err != nil {
			httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
			return
		}
		article = &found
		req.Title, req.Body = found.TitleKh, found.ContentKh
	}
	if req.Body == "" {
		httpx.BadRequest(c, httpx.CodeValidation, "There is no article text to summarise")
		return
	}

	draft, err := ctl.ai.GenerateSummary(c.Request.Context(), req.Title, req.Body)
	if err != nil {
		ctl.failAI(c, err)
		return
	}

	// Saving attaches the summary to the article, where it renders inside its
	// own labelled block. It never merges into the article body.
	if req.Save && article != nil && len(draft.Points) > 0 {
		now := time.Now().UTC()
		err := ctl.db.WithContext(c.Request.Context()).Model(article).Updates(map[string]any{
			"ai_summary":    models.StringSlice(draft.Points),
			"ai_summary_at": now,
		}).Error
		if err != nil {
			httpx.Internal(c, "Could not save the summary")
			return
		}
	}

	httpx.OK(c, gin.H{
		"draft":      draft,
		"saved":      req.Save && article != nil,
		"label":      "⚡ សង្ខេបព័ត៌មាន",
		"disclaimer": "AI summary of this article. Read the full report for the complete picture.",
	})
}

// ImagePrompts handles GET /api/ai/image-prompts (§25).
func (ctl *AIController) ImagePrompts(c *gin.Context) {
	httpx.OK(c, gin.H{
		"prompts":    ai.ImagePromptLibrary(),
		"disclosure": ai.AIImageDisclosure,
		"policy":     "Images generated from these prompts are illustrations. Never present one as a photograph of a real event, and always upload it with the AI-generated flag set.",
	})
}

// Status handles GET /api/ai/status, so the admin UI can disable the assistant
// panels rather than surfacing failures when no key is configured.
func (ctl *AIController) Status(c *gin.Context) {
	httpx.OK(c, gin.H{
		"configured": ctl.ai.Configured(),
		"canPublish": false, // stated explicitly; the AI never publishes (§21)
		"workflow":   []string{"verified information", "AI draft", "human editor", "publish"},
	})
}

func (ctl *AIController) failAI(c *gin.Context, err error) {
	if errors.Is(err, ai.ErrNotConfigured) {
		httpx.Fail(c, 503, httpx.CodeAINotConfigured, "The AI assistant is not configured")
		return
	}
	// The model's own explanation (a refusal, a truncation) is useful to the
	// journalist, so it is surfaced rather than replaced with a generic error.
	httpx.Fail(c, 502, "AI_REQUEST_FAILED", err.Error())
}
