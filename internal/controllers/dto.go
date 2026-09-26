package controllers

import (
	"time"

	"github.com/cambodia-fast-news/backend/internal/models"
)

// ArticleCard is the list/grid projection. It deliberately excludes the body:
// a 40-item feed should not ship 40 article bodies.
type ArticleCard struct {
	ID        uint   `json:"id"`
	Slug      string `json:"slug"`
	TitleKh   string `json:"titleKh"`
	TitleEn   string `json:"titleEn,omitempty"`
	SummaryKh string `json:"summaryKh,omitempty"`

	ImageURL    string `json:"imageUrl,omitempty"`
	ImageAlt    string `json:"imageAlt,omitempty"`
	ImageWidth  int    `json:"imageWidth,omitempty"`
	ImageHeight int    `json:"imageHeight,omitempty"`
	ImageIsAIGenerated bool `json:"imageIsAiGenerated,omitempty"`

	Category *CategoryRef `json:"category,omitempty"`
	Author   *AuthorRef   `json:"author,omitempty"`

	IsBreaking bool       `json:"isBreaking"`
	IsFeatured bool       `json:"isFeatured"`
	PublishedAt *time.Time `json:"publishedAt"`
	ReadingMinutes int     `json:"readingMinutes"`
	ViewCount   int64      `json:"viewCount"`

	// ContentType and SponsorName drive the paid-content label. They are on the
	// card as well as the detail view so a sponsored item is labelled in feeds
	// too, not only once a reader opens it (§42).
	ContentType models.ContentType `json:"contentType"`
	SponsorName string             `json:"sponsorName,omitempty"`
}

type CategoryRef struct {
	ID     uint   `json:"id"`
	Slug   string `json:"slug"`
	NameKh string `json:"nameKh"`
	NameEn string `json:"nameEn"`
	Color  string `json:"color,omitempty"`
	Icon   string `json:"icon,omitempty"`
}

type AuthorRef struct {
	ID       uint   `json:"id"`
	Slug     string `json:"slug"`
	NameKh   string `json:"nameKh"`
	NameEn   string `json:"nameEn,omitempty"`
	Title    string `json:"title,omitempty"`
	PhotoURL string `json:"photoUrl,omitempty"`
}

// ArticleDetail is the full article-page payload, including everything the
// frontend needs to emit correct metadata and structured data (§45–§49).
type ArticleDetail struct {
	ArticleCard

	// Status is included so an editor action can confirm the state it landed
	// in. Public reads only ever return published articles, so this is always
	// "published" there.
	Status models.ArticleStatus `json:"status"`

	SummaryEn string `json:"summaryEn,omitempty"`
	ContentKh string `json:"contentKh"`
	ContentEn string `json:"contentEn,omitempty"`
	ImageCaption string `json:"imageCaption,omitempty"`

	Tags []TagRef `json:"tags,omitempty"`

	UpdatedAt        time.Time  `json:"updatedAt"`
	UpdatedContentAt *time.Time `json:"updatedContentAt,omitempty"`
	WordCount        int        `json:"wordCount"`

	SponsorURL string `json:"sponsorUrl,omitempty"`

	// HasEnglish tells the renderer whether an hreflang alternate may be
	// emitted. Advertising a translation that does not exist is worse than
	// having none (§54).
	HasEnglish bool `json:"hasEnglish"`

	// AISummary is always delivered under its own key so the frontend renders
	// it in a labelled block, never inline with the reporting (§24).
	AISummary   []string   `json:"aiSummary,omitempty"`
	AISummaryAt *time.Time `json:"aiSummaryAt,omitempty"`
	AIAssisted  bool       `json:"aiAssisted"`

	Corrections []CorrectionRef `json:"corrections,omitempty"`
	SEO         *SEORef         `json:"seo,omitempty"`
}

type TagRef struct {
	Slug   string `json:"slug"`
	NameKh string `json:"nameKh"`
	NameEn string `json:"nameEn,omitempty"`
}

// CorrectionRef is the reader-facing correction notice (§20).
type CorrectionRef struct {
	NoteKh      string    `json:"noteKh"`
	NoteEn      string    `json:"noteEn,omitempty"`
	CorrectedAt time.Time `json:"correctedAt"`
	EditorName  string    `json:"editorName,omitempty"`
}

type SEORef struct {
	SEOTitle       string   `json:"seoTitle,omitempty"`
	SEODescription string   `json:"seoDescription,omitempty"`
	SEOKeywords    []string `json:"seoKeywords,omitempty"`
	CanonicalURL   string   `json:"canonicalUrl,omitempty"`
	OGTitle        string   `json:"ogTitle,omitempty"`
	OGDescription  string   `json:"ogDescription,omitempty"`
	OGImage        string   `json:"ogImage,omitempty"`
	TwitterTitle   string   `json:"twitterTitle,omitempty"`
	TwitterDescription string `json:"twitterDescription,omitempty"`
	TwitterImage   string   `json:"twitterImage,omitempty"`
	Robots         string   `json:"robots,omitempty"`
}

func toCard(a *models.Article) ArticleCard {
	card := ArticleCard{
		ID:          a.ID,
		Slug:        a.Slug,
		TitleKh:     a.TitleKh,
		TitleEn:     a.TitleEn,
		SummaryKh:   a.SummaryKh,
		ImageURL:    a.ImageURL,
		ImageAlt:    a.ImageAltKh,
		ImageWidth:  a.ImageWidth,
		ImageHeight: a.ImageHeight,
		ImageIsAIGenerated: a.ImageIsAIGenerated,
		IsBreaking:  a.IsLiveBreaking(time.Now().UTC()),
		IsFeatured:  a.IsFeatured,
		PublishedAt: a.PublishedAt,
		ReadingMinutes: a.ReadingMinutes,
		ViewCount:   a.ViewCount,
		ContentType: a.ContentType,
		SponsorName: a.SponsorName,
	}
	if card.ImageAlt == "" {
		card.ImageAlt = a.ImageAltEn
	}
	if a.Category != nil {
		card.Category = &CategoryRef{
			ID: a.Category.ID, Slug: a.Category.Slug,
			NameKh: a.Category.NameKh, NameEn: a.Category.NameEn,
			Color: a.Category.Color, Icon: a.Category.Icon,
		}
	}
	if a.Author != nil {
		card.Author = &AuthorRef{
			ID: a.Author.ID, Slug: a.Author.Slug,
			NameKh: a.Author.NameKh, NameEn: a.Author.NameEn,
			Title: a.Author.Title, PhotoURL: a.Author.PhotoURL,
		}
	}
	return card
}

func toCards(articles []models.Article) []ArticleCard {
	out := make([]ArticleCard, 0, len(articles))
	for i := range articles {
		out = append(out, toCard(&articles[i]))
	}
	return out
}

func toDetail(a *models.Article) ArticleDetail {
	detail := ArticleDetail{
		ArticleCard:      toCard(a),
		Status:           a.Status,
		SummaryEn:        a.SummaryEn,
		ContentKh:        a.ContentKh,
		ContentEn:        a.ContentEn,
		ImageCaption:     a.ImageCaption,
		UpdatedAt:        a.UpdatedAt,
		UpdatedContentAt: a.UpdatedContentAt,
		WordCount:        a.WordCount,
		SponsorURL:       a.SponsorURL,
		HasEnglish:       a.HasEnglish(),
		AISummary:        a.AISummary,
		AISummaryAt:      a.AISummaryAt,
		AIAssisted:       a.AIAssisted,
	}

	for _, t := range a.Tags {
		detail.Tags = append(detail.Tags, TagRef{Slug: t.Slug, NameKh: t.NameKh, NameEn: t.NameEn})
	}
	for _, corr := range a.Corrections {
		detail.Corrections = append(detail.Corrections, CorrectionRef{
			NoteKh: corr.NoteKh, NoteEn: corr.NoteEn,
			CorrectedAt: corr.CorrectedAt, EditorName: corr.EditorName,
		})
	}
	if a.SEO != nil {
		detail.SEO = &SEORef{
			SEOTitle: a.SEO.SEOTitle, SEODescription: a.SEO.SEODescription,
			SEOKeywords: a.SEO.SEOKeywords, CanonicalURL: a.SEO.CanonicalURL,
			OGTitle: a.SEO.OGTitle, OGDescription: a.SEO.OGDescription, OGImage: a.SEO.OGImage,
			TwitterTitle: a.SEO.TwitterTitle, TwitterDescription: a.SEO.TwitterDescription,
			TwitterImage: a.SEO.TwitterImage, Robots: a.SEO.Robots,
		}
	}
	return detail
}
