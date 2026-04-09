package transport

import "testing"

const (
	productionMediaAttachment   = `{"k":"dc7afa14a102e0ac1e38782fcfb6a8c8ca279633f00afecf94ed90a66c68e641.png","w":1600,"h":1200,"s":91632,"cs":"9fa6fd83cce4416977eefdb20095905b719da6178ff13a6f4c5b16f34d0109b7","mt":"image/png","thumbnailUrl":"https://talk.kakaocdn.net/dn/example.png","thumbnailHeight":90,"thumbnailWidth":120,"url":"https://talk.kakaocdn.net/dn/example-full.png","expire":1776246214962,"f":true}`
	productionModernLinkPreview = `{"urls":["https://v.daum.net/v/20260401185027234"],"universalScrapData":"{\"requested_url\":\"https://v.daum.net/v/20260401185027234\",\"canonical_url\":\"https://v.daum.net/v/20260401185027234\",\"title\":\"샘플 기사 제목\",\"description\":\"샘플 기사 설명\",\"image_url\":\"https://img1.daumcdn.net/example.jpg\"}"}`
)

func TestParseAttachmentInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		attachment string
		want       AttachmentInfo
	}{
		{
			name:       "empty",
			attachment: "",
			want:       AttachmentInfo{Kind: AttachmentKindUnknown},
		},
		{
			name:       "invalid json",
			attachment: "not json at all",
			want:       AttachmentInfo{Kind: AttachmentKindUnknown},
		},
		{
			name:       "production media attachment is ignored",
			attachment: productionMediaAttachment,
			want:       AttachmentInfo{Kind: AttachmentKindMedia},
		},
		{
			name:       "modern universal scrap attachment",
			attachment: productionModernLinkPreview,
			want: AttachmentInfo{
				Kind:        AttachmentKindLinkPreview,
				URL:         "https://v.daum.net/v/20260401185027234",
				Title:       "샘플 기사 제목",
				Description: "샘플 기사 설명",
				URLSource:   "universal_canonical",
			},
		},
		{
			name:       "legacy nested url object",
			attachment: `{"url":{"title":"프로폴리스 스프레이","url":"https://www.coupang.com/vp/products/1"}}`,
			want: AttachmentInfo{
				Kind:      AttachmentKindLinkPreview,
				URL:       "https://www.coupang.com/vp/products/1",
				Title:     "프로폴리스 스프레이",
				URLSource: "url",
			},
		},
		{
			name:       "legacy urls object array",
			attachment: `{"urls":[{"title":"삼성 갤럭시","url":"https://www.coupang.com/vp/products/3"}]}`,
			want: AttachmentInfo{
				Kind:      AttachmentKindLinkPreview,
				URL:       "https://www.coupang.com/vp/products/3",
				Title:     "삼성 갤럭시",
				URLSource: "urls",
			},
		},
		{
			name:       "flat description fallback",
			attachment: `{"description":"아이패드 프로 12.9","url":"https://www.coupang.com/..."}`,
			want: AttachmentInfo{
				Kind:        AttachmentKindLinkPreview,
				URL:         "https://www.coupang.com/...",
				Title:       "아이패드 프로 12.9",
				Description: "아이패드 프로 12.9",
				URLSource:   "flat",
			},
		},
		{
			name:       "photo attachment with legacy type is media",
			attachment: `{"type":"photo","url":"https://example.com/img.jpg"}`,
			want:       AttachmentInfo{Kind: AttachmentKindMedia},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAttachmentInfo(tt.attachment)
			if got != tt.want {
				t.Fatalf("ParseAttachmentInfo() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseLinkPreviewTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		attachment string
		want       string
	}{
		{
			name:       "modern universal scrap title",
			attachment: productionModernLinkPreview,
			want:       "샘플 기사 제목",
		},
		{
			name:       "legacy description fallback",
			attachment: `{"description":"폴백용 설명","url":"https://www.coupang.com/..."}`,
			want:       "폴백용 설명",
		},
		{
			name:       "media attachment returns empty",
			attachment: productionMediaAttachment,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseLinkPreviewTitle(tt.attachment); got != tt.want {
				t.Fatalf("ParseLinkPreviewTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseLinkPreviewURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		attachment string
		want       string
	}{
		{
			name:       "modern universal scrap url",
			attachment: productionModernLinkPreview,
			want:       "https://v.daum.net/v/20260401185027234",
		},
		{
			name:       "legacy shout url",
			attachment: `{"shout":{"type":"link","title":"다이슨 에어랩","url":"https://www.coupang.com/vp/products/2"}}`,
			want:       "https://www.coupang.com/vp/products/2",
		},
		{
			name:       "media attachment returns empty",
			attachment: productionMediaAttachment,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseLinkPreviewURL(tt.attachment); got != tt.want {
				t.Fatalf("ParseLinkPreviewURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
