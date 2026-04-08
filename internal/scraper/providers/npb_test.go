package providers

import "testing"

const fixtureNPBFinished = `
<li class="bb-score__item">
  <a class="bb-score__content" href="/npb/game/2021041156/index">
    <p class="bb-score__description">
      <span class="bb-score__venue">名護</span>
    </p>
    <div class="bb-score__team">
      <p class="bb-score__homeLogo bb-score__homeLogo--npbTeam8">日本ハム</p>
      <p class="bb-score__awayLogo bb-score__awayLogo--npbTeam5">阪神</p>
    </div>
    <div class="bb-score__info">
      <div class="bb-score__wrap">
        <div class="bb-score__detail">
          <p class="bb-score__status">
            <span class="bb-score__score bb-score__score--left">8</span>
            <span class="bb-score__score bb-score__score--center">-</span>
            <span class="bb-score__score bb-score__score--right">4</span>
          </p>
          <p class="bb-score__link">試合終了</p>
        </div>
      </div>
    </div>
  </a>
</li>
`

const fixtureNPBScheduled = `
<li class="bb-score__item">
  <a class="bb-score__content" href="/npb/game/2021041200/index">
    <p class="bb-score__description">
      <span class="bb-score__venue">東京ドーム</span>
      <span class="bb-score__time">18:00</span>
    </p>
    <div class="bb-score__team">
      <p class="bb-score__homeLogo bb-score__homeLogo--npbTeam1">巨人</p>
      <p class="bb-score__awayLogo bb-score__awayLogo--npbTeam5">阪神</p>
    </div>
    <div class="bb-score__info">
      <div class="bb-score__wrap">
        <div class="bb-score__detail">
          <p class="bb-score__link">18:00</p>
        </div>
      </div>
    </div>
  </a>
</li>
`

const fixtureNPBLive = `
<li class="bb-score__item">
  <a class="bb-score__content" href="/npb/game/2021041300/index">
    <p class="bb-score__description">
      <span class="bb-score__venue">甲子園</span>
    </p>
    <div class="bb-score__team">
      <p class="bb-score__homeLogo bb-score__homeLogo--npbTeam5">阪神</p>
      <p class="bb-score__awayLogo bb-score__awayLogo--npbTeam1">巨人</p>
    </div>
    <div class="bb-score__info">
      <div class="bb-score__wrap">
        <div class="bb-score__detail">
          <p class="bb-score__status">
            <span class="bb-score__score bb-score__score--left">3</span>
            <span class="bb-score__score bb-score__score--center">-</span>
            <span class="bb-score__score bb-score__score--right">2</span>
          </p>
          <p class="bb-score__link">5回裏</p>
        </div>
      </div>
    </div>
  </a>
</li>
`

func TestParseNPBHTML_Finished(t *testing.T) {
	matches, err := parseNPBHTML(fixtureNPBFinished, "2025-03-28")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	m := matches[0]
	if m.HomeTeam != "日本ハム" {
		t.Errorf("HomeTeam = %q, want %q", m.HomeTeam, "日本ハム")
	}
	if m.AwayTeam != "阪神" {
		t.Errorf("AwayTeam = %q, want %q", m.AwayTeam, "阪神")
	}
	if m.HomeScore != 8 || m.AwayScore != 4 {
		t.Errorf("score = %d:%d, want 8:4", m.HomeScore, m.AwayScore)
	}
	if m.Status != BaseballFinished {
		t.Errorf("status = %q, want %q", m.Status, BaseballFinished)
	}
}

func TestParseNPBHTML_Scheduled(t *testing.T) {
	matches, err := parseNPBHTML(fixtureNPBScheduled, "2025-04-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	m := matches[0]
	if m.Status != BaseballScheduled {
		t.Errorf("status = %q, want %q", m.Status, BaseballScheduled)
	}
	if m.HomeTeam != "巨人" {
		t.Errorf("HomeTeam = %q, want %q", m.HomeTeam, "巨人")
	}
	if m.StartTime.Hour() != 18 {
		t.Errorf("StartTime hour = %d, want 18", m.StartTime.Hour())
	}
}

func TestParseNPBHTML_Live(t *testing.T) {
	matches, err := parseNPBHTML(fixtureNPBLive, "2025-04-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	m := matches[0]
	if m.Status != BaseballLive {
		t.Errorf("status = %q, want %q", m.Status, BaseballLive)
	}
	if m.Inning != 5 {
		t.Errorf("inning = %d, want 5", m.Inning)
	}
	if m.Half != InningBottom {
		t.Errorf("half = %q, want %q", m.Half, InningBottom)
	}
	if m.HomeScore != 3 || m.AwayScore != 2 {
		t.Errorf("score = %d:%d, want 3:2", m.HomeScore, m.AwayScore)
	}
}

func TestParseNPBHTML_NoGames(t *testing.T) {
	matches, err := parseNPBHTML("<html><body>no games</body></html>", "2025-04-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("got %d matches, want 0", len(matches))
	}
}
