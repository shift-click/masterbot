package transport

// Origin identifies which transport adapter produced a message.
type Origin struct {
	AdapterID string // e.g. "main", "sub", "default"
}

// Message represents an incoming message from Iris WebSocket or HTTP endpoint.
// Fields match the Iris event format exactly.
type Message struct {
	Msg    string     // 복호화된 메시지 내용
	Room   string     // 채팅방 이름 (1:1 채팅이면 발신자 이름)
	Sender string     // 메시지 발신자 이름
	Raw    RawChatLog // chat_logs 테이블 원시 행
	Origin Origin     // 메시지를 수신한 transport adapter 식별
}

// RawChatLog contains raw database row data from the KakaoTalk chat_logs table.
type RawChatLog struct {
	ID         string // _id
	ChatID     string // chat_id — reply 시 room 필드에 사용
	UserID     string // user_id
	Message    string // 복호화된 메시지 (msg 필드와 동일)
	Attachment string // 복호화된 attachment 내용
	V          string // v 열 (enc 정보 포함, JSON 문자열)
}

// ReplyType identifies the kind of reply to send via Iris.
type ReplyType string

const (
	ReplyTypeText          ReplyType = "text"
	ReplyTypeImage         ReplyType = "image"
	ReplyTypeImageMultiple ReplyType = "image_multiple"
)

// ReplyRequest is the payload sent to Iris POST /reply.
// Fields match the Iris API spec: type, room (chat_id), data.
type ReplyRequest struct {
	Type      ReplyType `json:"type"`
	Room      string    `json:"room"`
	Data      any       `json:"data"`       // text: string, image: base64 string, image_multiple: []string
	AdapterID string    `json:"-"` // CompositeAdapter가 라우팅에 사용; Iris에는 전송하지 않음
}

// QueryRequest is the payload sent to Iris POST /query.
type QueryRequest struct {
	Query string   `json:"query"`
	Bind  []string `json:"bind,omitempty"`
}

// BulkQueryRequest supports multiple queries in a single POST /query call.
type BulkQueryRequest struct {
	Queries []QueryRequest `json:"queries"`
}

// QueryResponse is the response from Iris POST /query.
type QueryResponse struct {
	Data []map[string]any `json:"data"`
}
