package services

import (
	"testing"

	"github.com/cambodia-fast-news/backend/internal/models"
)

// The workflow in §16 is the rule that keeps unreviewed copy off the site, so
// it is tested directly rather than only through HTTP.
func TestCanTransitionFollowsEditorialWorkflow(t *testing.T) {
	allowed := []struct{ from, to models.ArticleStatus }{
		{models.StatusDraft, models.StatusReview},
		{models.StatusReview, models.StatusApproved},
		{models.StatusReview, models.StatusRejected},
		{models.StatusRejected, models.StatusDraft},
		{models.StatusApproved, models.StatusPublished},
		{models.StatusApproved, models.StatusScheduled},
		{models.StatusScheduled, models.StatusPublished},
		{models.StatusPublished, models.StatusArchived},
		{models.StatusArchived, models.StatusPublished},
	}
	for _, tc := range allowed {
		if !CanTransition(tc.from, tc.to) {
			t.Errorf("expected %s -> %s to be allowed", tc.from, tc.to)
		}
	}

	forbidden := []struct{ from, to models.ArticleStatus }{
		// The one that matters most: nothing reaches readers without review.
		{models.StatusDraft, models.StatusPublished},
		{models.StatusDraft, models.StatusApproved},
		{models.StatusDraft, models.StatusScheduled},
		{models.StatusReview, models.StatusPublished},
		{models.StatusPublished, models.StatusReview},
		{models.StatusRejected, models.StatusPublished},
	}
	for _, tc := range forbidden {
		if CanTransition(tc.from, tc.to) {
			t.Errorf("expected %s -> %s to be rejected", tc.from, tc.to)
		}
	}
}

func TestCanTransitionAllowsNoOp(t *testing.T) {
	if !CanTransition(models.StatusDraft, models.StatusDraft) {
		t.Error("saving an article in its current status should not be rejected")
	}
}

func TestValidateForPublishRequiresTheEssentials(t *testing.T) {
	svc := &ArticleService{}

	complete := &models.Article{
		TitleKh: "ចំណងជើង", ContentKh: "<p>អត្ថបទ</p>", SummaryKh: "សង្ខេប",
		CategoryID: 1, ContentType: models.ContentEditorial,
	}
	if err := svc.validateForPublish(complete); err != nil {
		t.Fatalf("a complete article was rejected: %v", err)
	}

	cases := map[string]func(*models.Article){
		"no headline": func(a *models.Article) { a.TitleKh = "" },
		"no body":     func(a *models.Article) { a.ContentKh = "" },
		"no summary":  func(a *models.Article) { a.SummaryKh = "" },
		"no category": func(a *models.Article) { a.CategoryID = 0 },
		"image without ALT text": func(a *models.Article) {
			a.ImageURL = "https://example.com/a.jpg"
		},
		"sponsored without a sponsor name": func(a *models.Article) {
			a.ContentType = models.ContentSponsored
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			article := *complete
			mutate(&article)
			if err := svc.validateForPublish(&article); err == nil {
				t.Errorf("publishing should have been refused: %s", name)
			}
		})
	}
}

func TestValidateForPublishAcceptsLabelledSponsoredContent(t *testing.T) {
	svc := &ArticleService{}
	article := &models.Article{
		TitleKh: "ចំណងជើង", ContentKh: "<p>x</p>", SummaryKh: "សង្ខេប", CategoryID: 1,
		ContentType: models.ContentSponsored, SponsorName: "Acme Bank",
	}
	if err := svc.validateForPublish(article); err != nil {
		t.Errorf("properly disclosed sponsored content was rejected: %v", err)
	}
}

func TestContentTypeRequiresDisclosure(t *testing.T) {
	if models.ContentEditorial.RequiresDisclosure() {
		t.Error("editorial content must not be labelled as paid")
	}
	for _, ct := range []models.ContentType{
		models.ContentSponsored, models.ContentAdvertise, models.ContentPaid,
	} {
		if !ct.RequiresDisclosure() {
			t.Errorf("%s must require a disclosure label", ct)
		}
	}
}
