package providers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"
)

const urlSummarySystemPrompt = `당신은 웹 콘텐츠 요약 전문가입니다. 아래 지침에 따라 웹 페이지 내용을 한국어로 요약해주세요.

## 출력 형식

콘텐츠 종류(뉴스, 블로그, 기술문서, 리뷰, 칼럼, 공지 등)를 자동으로 판단하고, 해당 종류에 맞는 구조로 요약합니다.

다음 형식을 따릅니다:

📰 [기사/페이지 제목]

💡 핵심 내용
• [가장 중요한 포인트 1]
• [가장 중요한 포인트 2]
• [가장 중요한 포인트 3~5개]

📋 상세 요약
[콘텐츠의 흐름을 따라가며 구조화된 요약을 작성합니다. 소제목을 적극 활용하고, 각 섹션은 핵심 내용만 간결하게 전달합니다.]

🏷️ 키워드: #키워드1 #키워드2 #키워드3

## 규칙
- 한국어로 작성
- 첫 줄에 반드시 📰 [기사/페이지 제목]을 포함
- 핵심 내용은 3~5개, 각각 1~2문장
- 상세 요약은 원본의 핵심 논점과 결론을 빠짐없이 포함
- 광고, 네비게이션, 사이드바 등 본문과 무관한 내용은 무시
- 불필요한 반복 제거
- 콘텐츠 종류에 따라:
  - 뉴스: 사실 위주, 배경 맥락 포함, 취재원 명시
  - 블로그: 핵심 주장과 근거 중심
  - 기술문서: 주요 개념과 사용법 중심
  - 리뷰: 장단점과 최종 평가 중심
  - 칼럼/오피니언: 저자의 핵심 주장과 논거 정리
`

const summarySystemPrompt = `당신은 YouTube 영상 요약 전문가입니다. 아래 지침에 따라 영상 내용을 한국어로 요약해주세요.

## 출력 형식

영상 종류(뉴스, 강의, 리뷰, 인터뷰, 토론, 브이로그 등)를 자동으로 판단하고, 해당 종류에 맞는 구조로 요약합니다.

다음 형식을 따릅니다:

🎬 [영상 제목]

💡 핵심 인사이트
• [가장 중요한 포인트 1]
• [가장 중요한 포인트 2]
• [가장 중요한 포인트 3~5개]

📋 상세 요약
[영상의 흐름을 따라가며 구조화된 요약을 작성합니다. 소제목을 적극 활용하고, 각 섹션은 핵심 내용만 간결하게 전달합니다.]

🏷️ 키워드: #키워드1 #키워드2 #키워드3

## 규칙
- 한국어로 작성
- 첫 줄에 반드시 🎬 [영상 제목]을 포함
- 핵심 인사이트는 3~5개, 각각 1~2문장
- 상세 요약은 원본의 핵심 논점과 결론을 빠짐없이 포함
- 불필요한 반복이나 간투사는 제거
- 영상 종류에 따라:
  - 뉴스: 사실 위주, 배경 맥락 포함
  - 강의: 주요 개념과 학습 포인트 중심
  - 리뷰: 장단점과 최종 평가 중심
  - 인터뷰: 핵심 질답과 인사이트 중심
  - 토론: 각 입장 정리와 결론
`

// GeminiClient wraps the Google Gemini API for YouTube video summarization.
type GeminiClient struct {
	client *genai.Client
	model  string
	logger *slog.Logger
}

// NewGeminiClient creates a Gemini client with the given API key and model.
func NewGeminiClient(ctx context.Context, apiKey, model string, logger *slog.Logger) (*GeminiClient, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}

	return &GeminiClient{
		client: client,
		model:  model,
		logger: logger,
	}, nil
}

// SummarizeYouTube sends a YouTube URL directly to Gemini for analysis and summarization.
func (g *GeminiClient) SummarizeYouTube(ctx context.Context, videoURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	g.logger.Debug("summarizing YouTube video via Gemini", "url", videoURL)

	resp, err := g.client.Models.GenerateContent(ctx, g.model, []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				genai.NewPartFromURI(videoURL, "video/mp4"),
				genai.NewPartFromText("이 영상을 요약해주세요."),
			},
		},
	}, &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(summarySystemPrompt, genai.RoleUser),
		Temperature:       genai.Ptr(float32(0.3)),
	})
	if err != nil {
		return "", fmt.Errorf("gemini generate from youtube: %w", err)
	}

	return extractTextResponse(resp), nil
}

// URLSummaryResult holds the result of a Gemini URLContext summarization.
type URLSummaryResult struct {
	Summary            string
	RetrievalStatus    genai.URLRetrievalStatus // empty if metadata absent
	HasRetrievalStatus bool                     // false when metadata is nil/empty
}

// IsRetrievalSuccess returns true only when the API explicitly reported success.
func (r URLSummaryResult) IsRetrievalSuccess() bool {
	return r.HasRetrievalStatus && r.RetrievalStatus == genai.URLRetrievalStatusSuccess
}

// IsRetrievalFailure returns true when the API reported a non-success status,
// or when the metadata is absent (nil/empty).
func (r URLSummaryResult) IsRetrievalFailure() bool {
	if !r.HasRetrievalStatus {
		return true // nil/empty metadata → treat as failure
	}
	switch r.RetrievalStatus {
	case genai.URLRetrievalStatusError,
		genai.URLRetrievalStatusPaywall,
		genai.URLRetrievalStatusUnsafe:
		return true
	}
	return false
}

// SummarizeURL sends a web URL to Gemini using the URL Context Tool for analysis and summarization.
func (g *GeminiClient) SummarizeURL(ctx context.Context, webURL string) (URLSummaryResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	g.logger.Debug("summarizing web URL via Gemini URL Context", "url", webURL)

	resp, err := g.client.Models.GenerateContent(ctx, g.model, []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				genai.NewPartFromText("이 웹 페이지를 요약해주세요: " + webURL),
			},
		},
	}, &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(urlSummarySystemPrompt, genai.RoleUser),
		Temperature:       genai.Ptr(float32(0.3)),
		Tools: []*genai.Tool{
			{URLContext: &genai.URLContext{}},
		},
	})
	if err != nil {
		return URLSummaryResult{}, fmt.Errorf("gemini generate from url: %w", err)
	}

	result := URLSummaryResult{Summary: extractTextResponse(resp)}
	if resp != nil && len(resp.Candidates) > 0 {
		if meta := resp.Candidates[0].URLContextMetadata; meta != nil && len(meta.URLMetadata) > 0 {
			result.RetrievalStatus = meta.URLMetadata[0].URLRetrievalStatus
			result.HasRetrievalStatus = true
		}
	}
	return result, nil
}

// SummarizeText summarizes pre-fetched page content without using the URLContext tool.
func (g *GeminiClient) SummarizeText(ctx context.Context, title, body string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	g.logger.Debug("summarizing pre-fetched text via Gemini", "title", title)

	var prompt strings.Builder
	prompt.WriteString("다음 웹 페이지 내용을 요약해주세요.\n\n")
	if title != "" {
		prompt.WriteString("제목: ")
		prompt.WriteString(title)
		prompt.WriteString("\n\n")
	}
	prompt.WriteString("본문:\n")
	prompt.WriteString(body)

	resp, err := g.client.Models.GenerateContent(ctx, g.model, []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				genai.NewPartFromText(prompt.String()),
			},
		},
	}, &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(urlSummarySystemPrompt, genai.RoleUser),
		Temperature:       genai.Ptr(float32(0.3)),
	})
	if err != nil {
		return "", fmt.Errorf("gemini generate from text: %w", err)
	}

	return extractTextResponse(resp), nil
}

func extractTextResponse(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}
	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return ""
	}

	var b strings.Builder
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			b.WriteString(part.Text)
		}
	}
	return strings.TrimSpace(b.String())
}
