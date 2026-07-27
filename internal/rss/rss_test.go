package rss

import (
    "testing"

    "github.com/mmcdole/gofeed"
    "rss2tg/internal/storage"
)

func TestMatchKeywordsHonorsMatchScope(t *testing.T) {
    manager := &Manager{db: storage.NewStorage(t.TempDir() + "/sent_items.txt")}
    item := &gofeed.Item{
        Title:       "cheap phone deal",
        Description: "golang api tutorial",
        Content:     "docker release notes",
        Link:        "https://example.com/item",
    }

    tests := []struct {
        name       string
        keyword    string
        scope      string
        wantMatch  bool
    }{
        {name: "title scope matches title", keyword: "phone", scope: "title", wantMatch: true},
        {name: "title scope ignores content", keyword: "golang", scope: "title", wantMatch: false},
        {name: "content scope matches content", keyword: "golang", scope: "content", wantMatch: true},
        {name: "content scope matches full content", keyword: "docker", scope: "content", wantMatch: true},
        {name: "content scope ignores title", keyword: "phone", scope: "content", wantMatch: false},
        {name: "all scope matches title", keyword: "phone", scope: "all", wantMatch: true},
        {name: "all scope matches content", keyword: "golang", scope: "all", wantMatch: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            feed := &Feed{
                URLs:           []string{"https://example.com/feed"},
                Keywords:       []string{tt.keyword},
                AllowPartMatch: true,
                MatchScope:     tt.scope,
            }

            matched := manager.matchKeywords(item, feed)
            gotMatch := len(matched) > 0
            if gotMatch != tt.wantMatch {
                t.Fatalf("matchKeywords() match = %v, want %v, matched = %v", gotMatch, tt.wantMatch, matched)
            }
        })
    }
}

func TestMatchKeywordsSkipsExcludedKeywords(t *testing.T) {
    manager := &Manager{db: storage.NewStorage(t.TempDir() + "/sent_items.txt")}

    tests := []struct {
        name            string
        item            *gofeed.Item
        excludeKeywords []string
    }{
        {
            name: "excludes title keyword",
            item: &gofeed.Item{
                Title: "sponsored phone deal",
                Link:  "https://example.com/title",
            },
            excludeKeywords: []string{"sponsored"},
        },
        {
            name: "excludes description keyword",
            item: &gofeed.Item{
                Title:       "phone deal",
                Description: "advertisement block",
                Link:        "https://example.com/description",
            },
            excludeKeywords: []string{"advertisement"},
        },
        {
            name: "excludes content keyword",
            item: &gofeed.Item{
                Title:   "phone deal",
                Content: "promoted article",
                Link:    "https://example.com/content",
            },
            excludeKeywords: []string{"promoted"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            feed := &Feed{
                URLs:            []string{"https://example.com/feed"},
                Keywords:        []string{"phone"},
                ExcludeKeywords: tt.excludeKeywords,
                AllowPartMatch:  true,
                MatchScope:      "all",
            }

            if matched := manager.matchKeywords(tt.item, feed); len(matched) != 0 {
                t.Fatalf("matchKeywords() matched excluded item: %v", matched)
            }
        })
    }
}
