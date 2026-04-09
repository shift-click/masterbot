package providers

import (
	"net/url"
	"strings"
)

type SummaryURLKind string

const (
	SummaryURLKindNone      SummaryURLKind = ""
	SummaryURLKindNews      SummaryURLKind = "news"
	SummaryURLKindNaverBlog SummaryURLKind = "naver_blog"
	SummaryURLKindX         SummaryURLKind = "x"
)

var autoSummaryNewsHosts = map[string]struct{}{
	"naver.me":               {},
	"news.naver.com":         {},
	"n.news.naver.com":       {},
	"m.sports.naver.com":     {},
	"v.daum.net":             {},
	"news.kbs.co.kr":         {},
	"m.news.kbs.co.kr":       {},
	"news.sbs.co.kr":         {},
	"m.news.sbs.co.kr":       {},
	"biz.sbs.co.kr":          {},
	"imnews.imbc.com":        {},
	"m.imbc.com":             {},
	"news.jtbc.co.kr":        {},
	"m.jtbc.co.kr":           {},
	"www.ytn.co.kr":          {},
	"m.ytn.co.kr":            {},
	"www.chosun.com":         {},
	"biz.chosun.com":         {},
	"m.chosun.com":           {},
	"joongang.co.kr":         {},
	"www.joongang.co.kr":     {},
	"m.joongang.co.kr":       {},
	"www.donga.com":          {},
	"m.donga.com":            {},
	"www.hani.co.kr":         {},
	"m.hani.co.kr":           {},
	"www.khan.co.kr":         {},
	"m.khan.co.kr":           {},
	"www.munhwa.com":         {},
	"m.munhwa.com":           {},
	"www.seoul.co.kr":        {},
	"m.seoul.co.kr":          {},
	"www.mk.co.kr":           {},
	"m.mk.co.kr":             {},
	"biz.mk.co.kr":           {},
	"www.hankyung.com":       {},
	"m.hankyung.com":         {},
	"magazine.hankyung.com":  {},
	"www.asiae.co.kr":        {},
	"cm.asiae.co.kr":         {},
	"view.asiae.co.kr":       {},
	"m.asiae.co.kr":          {},
	"www.mt.co.kr":           {},
	"m.mt.co.kr":             {},
	"mt.co.kr":               {},
	"www.sedaily.com":        {},
	"m.sedaily.com":          {},
	"www.edaily.co.kr":       {},
	"m.edaily.co.kr":         {},
	"biz.heraldcorp.com":     {},
	"www.etnews.com":         {},
	"m.etnews.com":           {},
	"zdnet.co.kr":            {},
	"m.zdnet.co.kr":          {},
	"www.bloter.net":         {},
	"m.bloter.net":           {},
	"www.digitaltoday.co.kr": {},
	"www.blockmedia.co.kr":   {},
	"blockmedia.co.kr":       {},
	"www.yna.co.kr":          {},
	"m.yna.co.kr":            {},
	"newsis.com":             {},
	"www.newsis.com":         {},
	"news1.kr":               {},
	"www.news1.kr":           {},
	"www.ohmynews.com":       {},
	"m.ohmynews.com":         {},
	"www.pressian.com":       {},
	"www.newspim.com":        {},
	"www.sisain.co.kr":       {},
	"www.businesspost.co.kr": {},
	"m.businesspost.co.kr":   {},
	"www.thebell.co.kr":      {},
	"www.sisajournal-e.com":  {},
	"biz.newdaily.co.kr":     {},
	"news.tf.co.kr":          {},
	"www.starnewskorea.com":  {},
	"starnewskorea.com":      {},
	"www.sportsseoul.com":    {},
	"m.sportsseoul.com":      {},
	"m.g-enews.com":          {},
	"www.gukjenews.com":      {},
	"www.jnilbo.com":         {},
	"m.jnilbo.com":           {},
	"www.pinpointnews.co.kr": {},
}

var autoSummaryXHosts = map[string]struct{}{
	"x.com":           {},
	"www.x.com":       {},
	"twitter.com":     {},
	"www.twitter.com": {},
}

var autoSummaryNaverBlogHosts = map[string]struct{}{
	"blog.naver.com":   {},
	"m.blog.naver.com": {},
}

func ClassifyAutoSummaryURL(rawURL string) SummaryURLKind {
	host := normalizedURLHost(rawURL)
	if host == "" {
		return SummaryURLKindNone
	}
	if _, ok := autoSummaryNewsHosts[host]; ok {
		return SummaryURLKindNews
	}
	if _, ok := autoSummaryNaverBlogHosts[host]; ok {
		return SummaryURLKindNaverBlog
	}
	if _, ok := autoSummaryXHosts[host]; ok {
		return SummaryURLKindX
	}
	return SummaryURLKindNone
}

func IsAutoSummaryURL(rawURL string) bool {
	return ClassifyAutoSummaryURL(rawURL) != SummaryURLKindNone
}

func normalizedURLHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return ""
	}
	return host
}
