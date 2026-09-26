package services

import (
	"testing"
	"time"
)

func TestTrendingScoreDecaysWithAge(t *testing.T) {
	svc := &TrendingService{weights: DefaultTrendingWeights()}
	now := time.Now().UTC()

	engagement := scoreRow{Views: 1000, UniqueViews: 500, Shares: 50, ReadSeconds: 20000}

	fresh := engagement
	fresh.PublishedAt = now.Add(-1 * time.Hour)

	old := engagement
	old.PublishedAt = now.Add(-24 * time.Hour)

	freshScore := svc.score(fresh, now)
	oldScore := svc.score(old, now)

	if freshScore <= oldScore {
		t.Errorf("a fresh story should outrank an equally-read old one: fresh=%f old=%f", freshScore, oldScore)
	}
}

func TestTrendingScoreIsZeroWithoutEngagement(t *testing.T) {
	svc := &TrendingService{weights: DefaultTrendingWeights()}
	now := time.Now().UTC()

	row := scoreRow{PublishedAt: now.Add(-time.Hour)}
	if got := svc.score(row, now); got != 0 {
		t.Errorf("an unread article scored %f, want 0", got)
	}
}

func TestTrendingScoreWeightsSharesAboveViews(t *testing.T) {
	// A share is a stronger signal than a view, so the weights must reflect it.
	svc := &TrendingService{weights: DefaultTrendingWeights()}
	now := time.Now().UTC()
	published := now.Add(-time.Hour)

	oneShare := svc.score(scoreRow{Shares: 1, PublishedAt: published}, now)
	oneView := svc.score(scoreRow{Views: 1, PublishedAt: published}, now)

	if oneShare <= oneView {
		t.Errorf("a share (%f) should weigh more than a view (%f)", oneShare, oneView)
	}
}

func TestFutureDatedArticleDoesNotGetABoost(t *testing.T) {
	// Clock skew must not produce a decay multiplier above 1.
	svc := &TrendingService{weights: DefaultTrendingWeights()}
	now := time.Now().UTC()

	future := svc.score(scoreRow{Views: 100, PublishedAt: now.Add(2 * time.Hour)}, now)
	present := svc.score(scoreRow{Views: 100, PublishedAt: now}, now)

	if future > present {
		t.Errorf("a future-dated article outscored a current one: %f > %f", future, present)
	}
}
