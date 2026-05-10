// Package rss polls a list of feeds and stores headlines in SQLite + state.
package rss

import (
	"context"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/store"
)

type Poller struct {
	Feeds    []string
	Interval time.Duration
	Limit    int
	Store    *state.Store
	DB       *store.DB
	Parser   *gofeed.Parser
}

func New(feeds []string, interval time.Duration, st *state.Store, db *store.DB) *Poller {
	return &Poller{Feeds: feeds, Interval: interval, Limit: 20, Store: st, DB: db, Parser: gofeed.NewParser()}
}

func (p *Poller) Once(ctx context.Context) error {
	now := time.Now()
	for _, url := range p.Feeds {
		feed, err := p.Parser.ParseURLWithContext(url, ctx)
		if err != nil {
			continue
		}
		for _, item := range feed.Items {
			pub := now
			if item.PublishedParsed != nil {
				pub = *item.PublishedParsed
			}
			guid := item.GUID
			if guid == "" {
				guid = url + "|" + item.Link
			}
			_ = p.DB.UpsertRSS(ctx, store.RSSItem{
				GUID: guid, Feed: feed.Title, Title: item.Title, Link: item.Link,
				Published: pub, FetchedAt: now,
			})
		}
	}
	items, err := p.DB.RecentRSS(ctx, p.Limit)
	if err != nil {
		return err
	}
	news := make([]state.NewsItem, 0, len(items))
	for _, it := range items {
		news = append(news, state.NewsItem{GUID: it.GUID, Feed: it.Feed, Title: it.Title, Link: it.Link, Published: it.Published})
	}
	p.Store.SetNews(news)
	return nil
}

func (p *Poller) Run(ctx context.Context) {
	if p.Interval == 0 {
		p.Interval = 15 * time.Minute
	}
	_ = p.Once(ctx)
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = p.Once(ctx)
		}
	}
}
