package model

import (
	"time"

	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/platform/idgen"
)

// 资讯数据源类型。目前由外部爬虫推送，类型只用于展示与统计。
const (
	NewsSourceCrawler = "crawler"
	NewsSourceAPI     = "api"
	NewsSourceRSS     = "rss"
)

// NewsSource 采集来源。爬虫按 Code 归属，便于统计产量与定位异常源。
//
// 来源是「白名单」而非自由字符串：推送时必须命中已启用的来源，
// 否则整批拒绝，避免脏编码把统计和客户端筛选项撕成碎片。
type NewsSource struct {
	Base
	Code          string     `gorm:"size:32;uniqueIndex;not null" json:"code"`
	Name          string     `gorm:"size:64;not null" json:"name"`
	Kind          string     `gorm:"size:16;not null;default:crawler" json:"kind"`
	Icon          string     `gorm:"size:512" json:"icon,omitempty"`
	Color         string     `gorm:"size:16" json:"color,omitempty"`
	Sort          int        `gorm:"default:0" json:"sort"`
	Homepage      string     `gorm:"size:255" json:"homepage,omitempty"`
	Status        int8       `gorm:"index;default:1" json:"status"`
	LastIngestAt  *time.Time `json:"lastIngestAt"`
	TotalIngested int64      `gorm:"default:0" json:"totalIngested"`
	Remark        string     `gorm:"size:255" json:"remark,omitempty"`
}

// TableName 指定表名。
func (NewsSource) TableName() string { return "news_source" }

// NewsCategory 资讯分类。与来源同为白名单配置，
// 由后台维护名称、图标与颜色，开放端原样下发给客户端。
type NewsCategory struct {
	Base
	Code   string `gorm:"size:32;uniqueIndex;not null" json:"code"`
	Name   string `gorm:"size:64;not null" json:"name"`
	Icon   string `gorm:"size:512" json:"icon,omitempty"`
	Color  string `gorm:"size:16" json:"color,omitempty"`
	Sort   int    `gorm:"default:0" json:"sort"`
	Status int8   `gorm:"index;default:1" json:"status"`
	Remark string `gorm:"size:255" json:"remark,omitempty"`
	// ArticleCount 仅用于列表展示，不落库。
	ArticleCount int64 `gorm:"-" json:"articleCount"`
}

// TableName 指定表名。
func (NewsCategory) TableName() string { return "news_category" }

// NewsArticle 一条资讯。
//
// 刻意不使用 Base：资讯是 7 天滚动的时序数据，软删除只会让表无限膨胀，
// 因此升级为硬删除，主键与时间字段单独声明。
type NewsArticle struct {
	ID          string    `gorm:"primaryKey;size:32" json:"id"`
	SourceID    string    `gorm:"size:32;not null;uniqueIndex:uk_news_dedup,priority:1" json:"sourceId"`
	SourceCode  string    `gorm:"size:32;index" json:"sourceCode"`
	SourceName  string    `gorm:"size:64" json:"sourceName"`
	ExternalID  string    `gorm:"size:160;not null;uniqueIndex:uk_news_dedup,priority:2" json:"externalId"`
	Title       string    `gorm:"size:300;not null" json:"title"`
	Summary     string    `gorm:"size:600" json:"summary,omitempty"`
	Content     string    `gorm:"type:text" json:"content,omitempty"`
	URL         string    `gorm:"size:512" json:"url,omitempty"`
	Author      string    `gorm:"size:64" json:"author,omitempty"`
	Category    string    `gorm:"size:32;not null;index:idx_news_category,priority:1" json:"category"`
	Tags        []string  `gorm:"serializer:json;type:text" json:"tags"`
	CoverURL    string    `gorm:"size:512" json:"coverUrl,omitempty"`
	ContentHash string    `gorm:"size:64;index:idx_news_hash" json:"-"`
	WordCount   int       `gorm:"default:0" json:"wordCount"`
	ViewCount   int64     `gorm:"default:0" json:"viewCount"`
	PublishedAt time.Time `gorm:"index:idx_news_published,priority:1;index:idx_news_category,priority:2" json:"publishedAt"`
	IngestedAt  time.Time `gorm:"index:idx_news_ingested" json:"ingestedAt"`
	Status      int8      `gorm:"index;default:1" json:"status"`
}

// TableName 指定表名。
func (NewsArticle) TableName() string { return "news_article" }

// BeforeCreate 生成雪花主键，与其它表保持一致。
func (a *NewsArticle) BeforeCreate(*gorm.DB) error {
	if a.ID == "" {
		a.ID = idgen.Next()
	}
	if a.IngestedAt.IsZero() {
		a.IngestedAt = time.Now().UTC()
	}
	return nil
}

// Brief 判断是否只加载了列表所需字段（没有正文）。
func (a *NewsArticle) Brief() bool { return a.Content == "" }
