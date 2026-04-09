package command

import (
	"testing"
)

func TestIsBrowseFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// --- Keyword-based detection ---
		{
			name:  "english could not be browsed",
			input: "The provided URL could not be browsed. Therefore, I cannot summarize the content.",
			want:  true,
		},
		{
			name:  "english cannot be browsed",
			input: "This URL cannot be browsed at this time.",
			want:  true,
		},
		{
			name:  "english unable to access",
			input: "I was unable to access the provided URL.",
			want:  true,
		},
		{
			name:  "english unable to browse",
			input: "I'm unable to browse this webpage.",
			want:  true,
		},
		{
			name:  "english failed to retrieve",
			input: "Failed to retrieve the content from the URL.",
			want:  true,
		},
		{
			name:  "english i cannot access",
			input: "I cannot access this URL due to restrictions.",
			want:  true,
		},
		{
			name:  "korean url open failure",
			input: "해당 URL을 열 수 없습니다.",
			want:  true,
		},
		{
			name:  "korean access failure",
			input: "이 페이지에 접근할 수 없습니다.",
			want:  true,
		},
		{
			name:  "korean page load failure",
			input: "페이지를 불러올 수 없습니다.",
			want:  true,
		},
		{
			name:  "korean fetch failure",
			input: "웹 페이지를 불러오는 데 실패했습니다.",
			want:  true,
		},
		{
			name:  "korean cannot verify content",
			input: "해당 URL의 내용을 확인할 수 없습니다.",
			want:  true,
		},
		{
			name:  "korean cannot summarize",
			input: "내용을 요약해 드릴 수 없습니다.",
			want:  true,
		},
		{
			name:  "korean blocked or inaccessible",
			input: "페이지가 차단되었거나 접근할 수 없는 상태입니다.",
			want:  true,
		},
		{
			name:  "korean access impossible",
			input: "해당 페이지에 접속이 불가능합니다.",
			want:  true,
		},
		{
			name:  "real naver failure from gemini",
			input: "죄송합니다. 제공해주신 웹 페이지(https://m.blog.naver.com/fontoylab/224098503131)를 불러오는 데 실패했습니다. 페이지가 차단되었거나 접근할 수 없는 상태인 것 같습니다. 따라서 내용을 요약해 드릴 수 없습니다.",
			want:  true,
		},
		{
			name:  "real naver failure variant 2",
			input: "죄송합니다. 제공해주신 웹 페이지(https://m.blog.naver.com/ranto28/224211434862)에 접속하여 내용을 가져올 수 없습니다. 페이지가 차단되었거나 존재하지 않을 수 있습니다. 따라서 해당 페이지를 요약해 드릴 수 없습니다. 다른 URL을 제공해 주시거나, 페이지 접속이 가능한지 확인해 주시면 감사하겠습니다.",
			want:  true,
		},
		{
			name:  "case insensitive",
			input: "THE PROVIDED URL COULD NOT BE BROWSED.",
			want:  true,
		},
		// --- Heuristic detection (short + negative keyword) ---
		{
			name:  "short korean apology heuristic",
			input: "죄송합니다. 이 페이지의 콘텐츠를 가져올 수 없었습니다.",
			want:  true,
		},
		{
			name:  "short english sorry heuristic",
			input: "Sorry, I was not able to load this page.",
			want:  true,
		},
		{
			name:  "short failure heuristic",
			input: "이 URL에 대한 요약 생성이 실패하였습니다.",
			want:  true,
		},
		// --- False positive protection ---
		{
			name:  "normal summary not detected",
			input: "📰 서울시 새 교통 정책 발표\n\n💡 핵심 내용\n• 2026년부터 심야 버스 노선 확대",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "korean article summary",
			input: "이 기사는 최근 AI 기술 동향에 대해 다루고 있습니다.",
			want:  false,
		},
		{
			name: "long summary with sorry not false positive",
			input: "이 기사에서는 죄송합니다라는 표현이 포함된 사과 문화에 대해 다루고 있습니다. " +
				"한국 사회에서 사과의 의미와 그 변화를 분석하며, 공적 사과가 갖는 사회적 기능에 대해 " +
				"심도 있게 논의합니다. 기사의 핵심 주장은 사과가 단순한 예의가 아닌 사회적 계약의 " +
				"일부라는 것입니다. 여러 사례를 통해 이를 뒷받침하고 있으며, 전문가 인터뷰도 포함되어 있습니다. " +
				"특히 정치인과 기업인의 사과 사례를 비교 분석한 부분이 인상적입니다.",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isBrowseFailure(tt.input)
			if got != tt.want {
				t.Errorf("isBrowseFailure(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
