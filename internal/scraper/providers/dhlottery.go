package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultDHLotteryLatestDrawURL = "https://www.dhlottery.co.kr/lt645/selectPstLt645Info.do"
	defaultDHLotteryShopBaseURL   = "https://www.dhlottery.co.kr/wnprchsplcsrch/selectLtWnShp.do"
)

type DHLottery struct {
	client       *BreakerHTTPClient
	logger       *slog.Logger
	latestDrawURL string
	shopBaseURL   string
}

type DHLotteryDraw struct {
	Round        int
	DrawDate     time.Time
	Numbers      [6]int
	BonusNumber  int
	Rank1Winners int
	Rank1Prize   int64
	Rank2Winners int
	Rank2Prize   int64
	Rank3Winners int
	Rank3Prize   int64
	Rank4Winners int
	Rank4Prize   int64
	Rank5Winners int
	Rank5Prize   int64
	TotalWinners int
	TotalSales   int64
}

type DHLotteryFirstPrizeShop struct {
	ShopID        string
	ShopName      string
	Region        string
	District      string
	Address1      string
	Address2      string
	Address3      string
	Address4      string
	FullAddress   string
	WinMethodCode string
	WinMethodText string
	Latitude      float64
	Longitude     float64
}

func NewDHLottery(logger *slog.Logger) *DHLottery {
	if logger == nil {
		logger = slog.Default()
	}
	return &DHLottery{
		client:        DefaultBreakerClient(10*time.Second, "dhlottery", logger),
		logger:        logger.With("component", "dhlottery"),
		latestDrawURL: defaultDHLotteryLatestDrawURL,
		shopBaseURL:   defaultDHLotteryShopBaseURL,
	}
}

func (d *DHLottery) FetchLatestDraw(ctx context.Context) (*DHLotteryDraw, error) {
	body, err := d.doGet(ctx, d.latestDrawURL)
	if err != nil {
		return nil, err
	}

	var response struct {
		Data struct {
			List []struct {
				Round        int   `json:"ltEpsd"`
				Num1         int   `json:"tm1WnNo"`
				Num2         int   `json:"tm2WnNo"`
				Num3         int   `json:"tm3WnNo"`
				Num4         int   `json:"tm4WnNo"`
				Num5         int   `json:"tm5WnNo"`
				Num6         int   `json:"tm6WnNo"`
				BonusNumber  int   `json:"bnsWnNo"`
				DrawDate     string `json:"ltRflYmd"`
				Rank1Winners int   `json:"rnk1WnNope"`
				Rank1Prize   int64 `json:"rnk1WnAmt"`
				Rank2Winners int   `json:"rnk2WnNope"`
				Rank2Prize   int64 `json:"rnk2WnAmt"`
				Rank3Winners int   `json:"rnk3WnNope"`
				Rank3Prize   int64 `json:"rnk3WnAmt"`
				Rank4Winners int   `json:"rnk4WnNope"`
				Rank4Prize   int64 `json:"rnk4WnAmt"`
				Rank5Winners int   `json:"rnk5WnNope"`
				Rank5Prize   int64 `json:"rnk5WnAmt"`
				TotalWinners int   `json:"sumWnNope"`
				TotalSales   int64 `json:"rlvtEpsdSumNtslAmt"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse latest draw: %w", err)
	}
	if len(response.Data.List) == 0 {
		return nil, fmt.Errorf("latest draw response is empty")
	}

	item := response.Data.List[0]
	drawDate, err := time.ParseInLocation("20060102", strings.TrimSpace(item.DrawDate), dhlotteryKSTLocation())
	if err != nil {
		return nil, fmt.Errorf("parse draw date %q: %w", item.DrawDate, err)
	}

	return &DHLotteryDraw{
		Round:        item.Round,
		DrawDate:     drawDate,
		Numbers:      [6]int{item.Num1, item.Num2, item.Num3, item.Num4, item.Num5, item.Num6},
		BonusNumber:  item.BonusNumber,
		Rank1Winners: item.Rank1Winners,
		Rank1Prize:   item.Rank1Prize,
		Rank2Winners: item.Rank2Winners,
		Rank2Prize:   item.Rank2Prize,
		Rank3Winners: item.Rank3Winners,
		Rank3Prize:   item.Rank3Prize,
		Rank4Winners: item.Rank4Winners,
		Rank4Prize:   item.Rank4Prize,
		Rank5Winners: item.Rank5Winners,
		Rank5Prize:   item.Rank5Prize,
		TotalWinners: item.TotalWinners,
		TotalSales:   item.TotalSales,
	}, nil
}

func (d *DHLottery) FetchFirstPrizeShops(ctx context.Context, round int) ([]DHLotteryFirstPrizeShop, error) {
	url := fmt.Sprintf("%s?srchWnShpRnk=1&srchLtEpsd=%d", d.shopBaseURL, round)
	body, err := d.doGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var response struct {
		Data struct {
			List []struct {
				ShopID        string  `json:"ltShpId"`
				ShopName      string  `json:"shpNm"`
				Region        string  `json:"region"`
				Address1      string  `json:"tm1ShpLctnAddr"`
				Address2      string  `json:"tm2ShpLctnAddr"`
				Address3      string  `json:"tm3ShpLctnAddr"`
				Address4      string  `json:"tm4ShpLctnAddr"`
				FullAddress   string  `json:"shpAddr"`
				WinMethodCode string  `json:"atmtPsvYn"`
				WinMethodText string  `json:"atmtPsvYnTxt"`
				Latitude      float64 `json:"shpLat"`
				Longitude     float64 `json:"shpLot"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse first prize shops: %w", err)
	}

	shops := make([]DHLotteryFirstPrizeShop, 0, len(response.Data.List))
	for _, item := range response.Data.List {
		shops = append(shops, DHLotteryFirstPrizeShop{
			ShopID:        strings.TrimSpace(item.ShopID),
			ShopName:      strings.TrimSpace(item.ShopName),
			Region:        strings.TrimSpace(item.Region),
			District:      strings.TrimSpace(item.Address2),
			Address1:      strings.TrimSpace(item.Address1),
			Address2:      strings.TrimSpace(item.Address2),
			Address3:      strings.TrimSpace(item.Address3),
			Address4:      strings.TrimSpace(item.Address4),
			FullAddress:   strings.TrimSpace(item.FullAddress),
			WinMethodCode: strings.TrimSpace(item.WinMethodCode),
			WinMethodText: strings.TrimSpace(item.WinMethodText),
			Latitude:      item.Latitude,
			Longitude:     item.Longitude,
		})
	}
	return shops, nil
}

func (d *DHLottery) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; JucoBot/2.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dhlottery HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func dhlotteryKSTLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil || loc == nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}
