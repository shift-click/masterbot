package providers

import "strings"

const baseballTeamSaintLouis = "세인트루이스"

// baseballTeamNames maps English/Japanese team names to Korean names.
// Keys are lowercase for case-insensitive lookup.
var baseballTeamNames = map[string]string{
	// MLB - American League East
	"baltimore orioles": "볼티모어", "orioles": "볼티모어",
	"boston red sox": "보스턴", "red sox": "보스턴",
	"new york yankees": "양키스", "yankees": "양키스",
	"tampa bay rays": "탬파베이", "rays": "탬파베이",
	"toronto blue jays": "토론토", "blue jays": "토론토",
	// MLB - American League Central
	"chicago white sox": "화이트삭스", "white sox": "화이트삭스",
	"cleveland guardians": "클리블랜드", "guardians": "클리블랜드",
	"detroit tigers": "디트로이트", "tigers": "디트로이트",
	"kansas city royals": "캔자스시티", "royals": "캔자스시티",
	"minnesota twins": "미네소타", "twins": "미네소타",
	// MLB - American League West
	"houston astros": "휴스턴", "astros": "휴스턴",
	"los angeles angels": "에인절스", "angels": "에인절스",
	"oakland athletics": "오클랜드", "athletics": "오클랜드",
	"seattle mariners": "시애틀", "mariners": "시애틀",
	"texas rangers": "텍사스", "rangers": "텍사스",
	// MLB - National League East
	"atlanta braves": "애틀랜타", "braves": "애틀랜타",
	"miami marlins": "마이애미", "marlins": "마이애미",
	"new york mets": "메츠", "mets": "메츠",
	"philadelphia phillies": "필라델피아", "phillies": "필라델피아",
	"washington nationals": "워싱턴", "nationals": "워싱턴",
	// MLB - National League Central
	"chicago cubs": "컵스", "cubs": "컵스",
	"cincinnati reds": "신시내티", "reds": "신시내티",
	"milwaukee brewers": "밀워키", "brewers": "밀워키",
	"pittsburgh pirates": "피츠버그", "pirates": "피츠버그",
	"st. louis cardinals": baseballTeamSaintLouis, "cardinals": baseballTeamSaintLouis,
	"st louis cardinals": baseballTeamSaintLouis,
	// MLB - National League West
	"arizona diamondbacks": "애리조나", "diamondbacks": "애리조나", "d-backs": "애리조나",
	"colorado rockies": "콜로라도", "rockies": "콜로라도",
	"los angeles dodgers": "다저스", "dodgers": "다저스",
	"san diego padres": "샌디에이고", "padres": "샌디에이고",
	"san francisco giants": "자이언츠", "giants": "자이언츠",

	// NPB - Central League (일본어 → 한글)
	"読売ジャイアンツ": "요미우리", "巨人": "요미우리", "読売": "요미우리",
	"東京ヤクルトスワローズ": "야쿠르트", "ヤクルト": "야쿠르트",
	"横浜DeNAベイスターズ": "요코하마", "DeNA": "요코하마", "横浜": "요코하마",
	"中日ドラゴンズ": "주니치", "中日": "주니치",
	"阪神タイガース": "한신", "阪神": "한신",
	"広島東洋カープ": "히로시마", "広島": "히로시마",
	// NPB - Pacific League
	"オリックス・バファローズ": "오릭스", "オリックス": "오릭스",
	"千葉ロッテマリーンズ": "지바롯데", "ロッテ": "지바롯데",
	"福岡ソフトバンクホークス": "소프트뱅크", "ソフトバンク": "소프트뱅크",
	"東北楽天ゴールデンイーグルス": "라쿠텐", "楽天": "라쿠텐",
	"北海道日本ハムファイターズ": "니혼햄", "日本ハム": "니혼햄", "日ハム": "니혼햄",
	"埼玉西武ライオンズ": "세이부", "西武": "세이부",
}

// TranslateBaseballTeamName translates a team name to Korean.
// Returns the original name if no mapping exists.
func TranslateBaseballTeamName(name string) string {
	key := strings.TrimSpace(strings.ToLower(name))
	if kr, ok := baseballTeamNames[key]; ok {
		return kr
	}
	// Try original case (for Japanese characters)
	if kr, ok := baseballTeamNames[strings.TrimSpace(name)]; ok {
		return kr
	}
	return name
}
