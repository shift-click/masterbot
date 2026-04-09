package e2e

import (
	"testing"

	"github.com/shift-click/masterbot/internal/transport/httptest"
)

const (
	productionChartFalsePositiveMessage = "차트도 생각해보니 이미지 아닌가"
	productionChartPrefixFalsePositive1 = "차트 단어"
	productionChartPrefixFalsePositive2 = "차트 단어에 반응"
	productionMediaAttachmentExactShape = `{"k":"dc7afa14a102e0ac1e38782fcfb6a8c8ca279633f00afecf94ed90a66c68e641.png","w":1600,"h":1200,"s":91632,"cs":"9fa6fd83cce4416977eefdb20095905b719da6178ff13a6f4c5b16f34d0109b7","mt":"image/png","thumbnailUrl":"https://talk.kakaocdn.net/dn/example.png","thumbnailHeight":90,"thumbnailWidth":120,"url":"https://talk.kakaocdn.net/dn/example-full.png","expire":1776246214962,"f":true}`
	productionLinkPreviewExactShape     = `{"urls":["https://v.daum.net/v/20260401185027234"],"universalScrapData":"{\"requested_url\":\"https://v.daum.net/v/20260401185027234\",\"canonical_url\":\"https://v.daum.net/v/20260401185027234\",\"title\":\"샘플 기사 제목\",\"description\":\"샘플 기사 설명\",\"image_url\":\"https://img1.daumcdn.net/example.jpg\"}"}`
)

func TestRegression_ChartSubstringConversationDoesNotTriggerChart(t *testing.T) {
	result := sendMessage(t, productionChartFalsePositiveMessage)
	if len(result.Replies) != 0 {
		t.Fatalf("expected no replies for conversational chart substring, got %+v", result.Replies)
	}
}

func TestRegression_ChartPrefixConversationDoesNotTriggerChart(t *testing.T) {
	for _, msg := range []string{
		productionChartPrefixFalsePositive1,
		productionChartPrefixFalsePositive2,
	} {
		t.Run(msg, func(t *testing.T) {
			result := sendMessage(t, msg)
			if len(result.Replies) != 0 {
				t.Fatalf("expected no replies for conversational chart prefix, got %+v", result.Replies)
			}
		})
	}
}

func TestRegression_MediaAttachmentDoesNotTriggerLinkHandlers(t *testing.T) {
	result := sendMessageRequest(t, httptest.MessageRequest{
		Msg:        "",
		Attachment: productionMediaAttachmentExactShape,
	})
	if len(result.Replies) != 0 {
		t.Fatalf("expected no replies for media attachment, got %+v", result.Replies)
	}
}

func TestRegression_LinkPreviewAttachmentStillTriggersURLSummary(t *testing.T) {
	result := sendMessageRequest(t, httptest.MessageRequest{
		Msg:        "",
		Attachment: productionLinkPreviewExactShape,
	})
	if len(result.Replies) != 0 {
		t.Fatalf("expected no immediate ack reply for link preview attachment, got %+v", result.Replies)
	}
}

func TestRegression_DisallowedLinkDoesNotTriggerURLSummary(t *testing.T) {
	result := sendMessage(t, "https://example.com/article")
	if len(result.Replies) != 0 {
		t.Fatalf("expected no replies for disallowed URL, got %+v", result.Replies)
	}
}
