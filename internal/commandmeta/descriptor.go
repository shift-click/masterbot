package commandmeta

import "strings"

type FallbackScope string

const (
	FallbackScopeAuto          FallbackScope = "auto"
	FallbackScopeDeterministic FallbackScope = "deterministic"
)

type Descriptor struct {
	ID              string
	Name            string
	Description     string
	SlashAliases    []string
	ExplicitAliases []string
	NormalizeKeys   []string
	FallbackScope   FallbackScope
	AllowAutoQuery  bool
	ACLExempt       bool
	Example         string
	Category        string
	HelpVisible     bool
}

type descriptorOptions struct {
	slashAliases    []string
	explicitAliases []string
	normalizeKeys   []string
	fallbackScope   FallbackScope
	allowAutoQuery  bool
	aclExempt       bool
	example         string
	category        string
	helpVisible     bool
}

var descriptors = []Descriptor{
	newDescriptor("help", "도움", "명령어 목록 조회", descriptorOptions{
		slashAliases:  []string{"help", "h"},
		normalizeKeys: []string{"help.show"},
		example:       "도움",
	}),
	newMarketDescriptor("coin", "코인", "코인 시세 조회", []string{"coin"}, []string{"coin.quote"}, "비트, BTC, PEPE"),
	newMarketDescriptor("stock", "주식", "주식 시세 조회", []string{"stock"}, []string{"stock.quote"}, "삼전, 삼성전자, 005930"),
	newMarketDescriptor("index", "지수", "시장 지수 조회", []string{"index"}, []string{"index.quote"}, "코스피, 코스닥, 나스닥, 다우"),
	newDescriptor("coupang", "쿠팡", "쿠팡 상품 가격 추이 조회", descriptorOptions{
		slashAliases:    []string{"coupang"},
		explicitAliases: []string{"쿠팡"},
		normalizeKeys:   []string{"coupang.track"},
		fallbackScope:   FallbackScopeDeterministic,
		allowAutoQuery:  true,
		example:         "상품 링크 붙여넣기",
		category:        "시세",
		helpVisible:     true,
	}),
	newInfoDescriptor("finance", "환율", "주요 통화 환율 조회", []string{"finance"}, []string{"환율"}, []string{"finance.quote"}, infoDescriptorOptions{
		category:       "시세",
		example:        "환율",
		allowAutoQuery: true,
	}),
	newInfoDescriptor("weather", "날씨", "날씨/미세먼지 조회", []string{"weather"}, []string{"날씨"}, []string{"weather.show"}, infoDescriptorOptions{
		category:       "정보",
		example:        "날씨, 서울 날씨, 강남구 미세먼지",
		allowAutoQuery: true,
	}),
	newInfoDescriptor("news", "뉴스", "실시간 인기뉴스 Top5", []string{"news"}, []string{"뉴스"}, []string{"news.show"}, infoDescriptorOptions{
		category:       "정보",
		example:        "뉴스",
		allowAutoQuery: false,
	}),
	newDescriptor("ai", "AI", "AI 대화", descriptorOptions{
		slashAliases:  []string{"ai", "gpt"},
		normalizeKeys: []string{"ai.chat"},
		example:       "AI 안녕",
	}),
	newDescriptor("admin", "관리", "ACL/조회 정책 운영 관리", descriptorOptions{
		slashAliases:    []string{"admin"},
		explicitAliases: []string{"관리"},
		normalizeKeys:   []string{"admin.status", "admin.acl"},
		example:         "관리 현황",
		category:        "관리",
		helpVisible:     true,
	}),
	newSportsDescriptor("football", "축구", "축구 경기 일정/스코어", []string{"축구", "soccer", "football"}, []string{"축구"}, []string{"football.schedule"}, "EPL, 챔스, K리그"),
	newSportsDescriptor("esports", "롤", "LoL e스포츠 일정/스코어", []string{"롤", "lol", "esports"}, []string{"롤"}, []string{"esports.schedule"}, "LCK, LPL, LEC"),
	newSportsDescriptor("baseball", "야구", "야구 경기 일정/스코어", []string{"야구", "baseball"}, []string{"야구"}, []string{"baseball.schedule"}, "MLB, KBO, NPB"),
	newDescriptor("forex-convert", "환율변환", "채팅 내 통화 자동 원화 변환", descriptorOptions{
		normalizeKeys: []string{"forex-convert.detect"},
		fallbackScope: FallbackScopeDeterministic,
		aclExempt:     true,
	}),
	newDescriptor("gold", "금시세", "금/은 귀금속 시세 조회", descriptorOptions{
		explicitAliases: []string{"금시세"},
		normalizeKeys:   []string{"gold.quote"},
		fallbackScope:   FallbackScopeAuto,
		allowAutoQuery:  true,
		category:        "시세",
		helpVisible:     true,
	}),
	newDescriptor("lotto", "로또", "최신 당첨번호와 내 번호 조회", descriptorOptions{
		slashAliases:  []string{"lotto"},
		normalizeKeys: []string{"lotto.show", "!로또"},
		example:       "로또, 로또 추천, !로또 1 2 3 4 5 6",
		category:      "로또",
		helpVisible:   true,
	}),
	newDescriptor("fortune", "운세", "오늘의 운세 조회", descriptorOptions{
		explicitAliases: []string{"오늘운세", "오늘의운세"},
		normalizeKeys:   []string{"fortune.show"},
		category:        "정보",
		helpVisible:     true,
	}),
	newDescriptor("calc", "계산기", "수식 계산", descriptorOptions{
		normalizeKeys: []string{"calc.eval"},
		fallbackScope: FallbackScopeDeterministic,
		aclExempt:     true,
	}),
	newDescriptor("youtube", "유튜브", "유튜브 영상 요약", descriptorOptions{
		slashAliases:    []string{"yt", "youtube"},
		explicitAliases: []string{"유튜브"},
		normalizeKeys:   []string{"youtube.summary"},
		fallbackScope:   FallbackScopeDeterministic,
		example:         "YouTube URL 붙여넣기",
		category:        "정보",
		helpVisible:     true,
	}),
	newDescriptor("url-summary", "요약", "웹 링크 AI 요약", descriptorOptions{
		slashAliases:    []string{"summary"},
		explicitAliases: []string{"요약"},
		normalizeKeys:   []string{"url-summary.summarize"},
		fallbackScope:   FallbackScopeDeterministic,
		example:         "요약 https://...",
		category:        "정보",
		helpVisible:     true,
	}),
	newDescriptor("chart", "차트", "코인/주식 가격 차트 조회", descriptorOptions{
		slashAliases:    []string{"chart"},
		explicitAliases: []string{"차트"},
		normalizeKeys:   []string{"chart.show"},
		fallbackScope:   FallbackScopeDeterministic,
		example:         "비트코인 차트, 삼전 차트 1달",
		category:        "시세",
		helpVisible:     true,
	}),
	newDescriptor("trending", "실검", "실시간 검색 트렌드 Top10", descriptorOptions{
		slashAliases:    []string{"trending"},
		explicitAliases: []string{"실검"},
		normalizeKeys:   []string{"trending.show"},
		example:         "실검",
		category:        "정보",
		helpVisible:     true,
	}),
}

var descriptorIndex = buildDescriptorIndex(descriptors)

func Descriptors() []Descriptor {
	out := make([]Descriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		out = append(out, cloneDescriptor(descriptor))
	}
	return out
}

func Lookup(id string) (Descriptor, bool) {
	descriptor, ok := descriptorIndex[normalize(id)]
	if !ok {
		return Descriptor{}, false
	}
	return cloneDescriptor(descriptor), true
}

func Must(id string) Descriptor {
	descriptor, ok := Lookup(id)
	if !ok {
		panic("unknown command descriptor: " + strings.TrimSpace(id))
	}
	return descriptor
}

func NormalizeIntentID(value string) (string, bool) {
	descriptor, ok := Lookup(value)
	if !ok {
		return "", false
	}
	return descriptor.ID, true
}

// DisplayName returns the Korean display name for the given intent ID.
// If the intent is not found, it returns the input as-is.
func DisplayName(intentID string) string {
	descriptor, ok := Lookup(intentID)
	if !ok {
		return intentID
	}
	return descriptor.Name
}

// ToggleableIntentIDs returns intent IDs eligible for bulk toggle ("전체 켜기/끄기").
// Includes intents that have SlashAliases or ExplicitAliases (user-facing),
// excluding admin, forex-convert, and calc.
func ToggleableIntentIDs() []string {
	excluded := map[string]struct{}{
		"admin":         {},
		"forex-convert": {},
		"calc":          {},
	}
	var ids []string
	for _, d := range descriptors {
		if _, skip := excluded[d.ID]; skip {
			continue
		}
		if len(d.SlashAliases) > 0 || len(d.ExplicitAliases) > 0 {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	descriptor.SlashAliases = cloneStrings(descriptor.SlashAliases)
	descriptor.ExplicitAliases = cloneStrings(descriptor.ExplicitAliases)
	descriptor.NormalizeKeys = cloneStrings(descriptor.NormalizeKeys)
	return descriptor
}

func newMarketDescriptor(id, name, description string, slashAliases, normalizeKeys []string, example string) Descriptor {
	return newDescriptor(id, name, description, descriptorOptions{
		slashAliases:   slashAliases,
		normalizeKeys:  normalizeKeys,
		fallbackScope:  FallbackScopeAuto,
		allowAutoQuery: true,
		example:        example,
		category:       "시세",
		helpVisible:    true,
	})
}

type infoDescriptorOptions struct {
	category       string
	example        string
	allowAutoQuery bool
}

func newInfoDescriptor(id, name, description string, slashAliases, explicitAliases, normalizeKeys []string, opts infoDescriptorOptions) Descriptor {
	return newDescriptor(id, name, description, descriptorOptions{
		slashAliases:    slashAliases,
		explicitAliases: explicitAliases,
		normalizeKeys:   normalizeKeys,
		allowAutoQuery:  opts.allowAutoQuery,
		example:         opts.example,
		category:        opts.category,
		helpVisible:     true,
	})
}

func newSportsDescriptor(id, name, description string, slashAliases, explicitAliases, normalizeKeys []string, example string) Descriptor {
	return newDescriptor(id, name, description, descriptorOptions{
		slashAliases:    slashAliases,
		explicitAliases: explicitAliases,
		normalizeKeys:   normalizeKeys,
		allowAutoQuery:  true,
		example:         example,
		category:        "스포츠",
		helpVisible:     true,
	})
}

func newDescriptor(id, name, description string, opts descriptorOptions) Descriptor {
	return Descriptor{
		ID:              id,
		Name:            name,
		Description:     description,
		SlashAliases:    cloneStrings(opts.slashAliases),
		ExplicitAliases: cloneStrings(opts.explicitAliases),
		NormalizeKeys:   cloneStrings(opts.normalizeKeys),
		FallbackScope:   opts.fallbackScope,
		AllowAutoQuery:  opts.allowAutoQuery,
		ACLExempt:       opts.aclExempt,
		Example:         opts.example,
		Category:        opts.category,
		HelpVisible:     opts.helpVisible,
	}
}

func buildDescriptorIndex(values []Descriptor) map[string]Descriptor {
	index := make(map[string]Descriptor, len(values)*6)
	for _, descriptor := range values {
		for _, raw := range descriptorKeys(descriptor) {
			registerDescriptorKey(index, raw, descriptor)
		}
	}
	return index
}

func descriptorKeys(descriptor Descriptor) []string {
	keys := make([]string, 0, 2+len(descriptor.SlashAliases)+len(descriptor.ExplicitAliases)+len(descriptor.NormalizeKeys))
	keys = append(keys, descriptor.ID, descriptor.Name)
	keys = append(keys, descriptor.SlashAliases...)
	keys = append(keys, descriptor.ExplicitAliases...)
	keys = append(keys, descriptor.NormalizeKeys...)
	return keys
}

func registerDescriptorKey(index map[string]Descriptor, raw string, descriptor Descriptor) {
	key := normalize(raw)
	if key == "" {
		return
	}
	index[key] = descriptor
}

func normalize(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
