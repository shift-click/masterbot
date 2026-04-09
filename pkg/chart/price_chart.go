// Package chart generates TradingView-style price trend PNG charts.
package chart

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"
	"time"

	"github.com/fogleman/gg"
	"golang.org/x/image/font"
)

// ChartDirection indicates the price trend direction for dynamic coloring.
type ChartDirection string

const (
	DirectionNone ChartDirection = ""     // default blue (#2962FF)
	DirectionUp   ChartDirection = "up"   // bullish green (#26A69A)
	DirectionDown ChartDirection = "down" // bearish red (#EF5350)
)

// CandlePoint represents a single OHLCV data point for candlestick rendering.
type CandlePoint struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// PriceChartData provides the data needed to render a price chart.
type PriceChartData struct {
	Prices       []int       // chronological close prices (used for line chart fallback)
	Timestamps   []time.Time // corresponding timestamps (same length as Prices)
	CurrentPrice int
	LowestPrice  int
	HighestPrice int
	MeanPrice    int
	ProductName  string
	IsAtLowest   bool
	PeriodLabel  string // e.g. "3개월"

	// Extended fields (zero-value preserves backward compatibility)
	Direction ChartDirection // dynamic line color: up=green, down=red, ""=blue
	SubTitle  string         // e.g. "₩107,268,686 (+2.25%)" below ProductName
	Volumes   []float64      // optional volume bars (same length as Prices)

	// Candlestick fields — when Candles is non-empty, candlestick mode is used.
	Candles   []CandlePoint // OHLCV data for candlestick rendering
	AssetType string        // "coin" or "stock" — used for pinpoint deviation tuning
}

const hexHighest = "#EF5350" // red for highest price line

// ChartConfig controls chart appearance.
type ChartConfig struct {
	Width  int
	Height int
}

// DefaultConfig returns a wide chart configuration.
// 1920x960 (2:1) gives candles generous horizontal spacing while maintaining vertical room.
func DefaultConfig() ChartConfig {
	return ChartConfig{
		Width:  1920,
		Height: 960,
	}
}

// TradingView dark theme colors (0-1 range for gg).
const (
	hexBg      = "#131722"
	hexGrid    = "#1E222D"
	hexLine    = "#2962FF"
	hexLowest  = "#26A69A"
	hexText    = "#D1D4DC"
	hexTextDim = "#787B86"
)

// RGB components for gradient (pre-computed from #2962FF).
const (
	lineR = 0x29 / 255.0
	lineG = 0x62 / 255.0
	lineB = 0xFF / 255.0
)

// RGB components for lowest color (pre-computed from #26A69A).
const (
	lowestR = 0x26 / 255.0
	lowestG = 0xA6 / 255.0
	lowestB = 0x9A / 255.0
)

// RGB components for highest color (pre-computed from #EF5350).
const (
	highestR = 0xEF / 255.0
	highestG = 0x53 / 255.0
	highestB = 0x50 / 255.0
)

// Font sizes at 2x resolution.
const (
	fontSizeHeader = 26.0
	fontSizePrice  = 22.0
	fontSizePeriod = 20.0
	fontSizeAxis   = 18.0
)

// DrawPriceChart generates a price chart as PNG bytes.
// If data.Candles is populated, renders a candlestick chart; otherwise falls back to line chart.
func DrawPriceChart(data PriceChartData, cfg ChartConfig) ([]byte, error) {
	candleMode := len(data.Candles) >= 2
	if !candleMode && len(data.Prices) < 2 {
		return nil, fmt.Errorf("need at least 2 price points")
	}
	if cfg.Width == 0 || cfg.Height == 0 {
		cfg = DefaultConfig()
	}
	dc := gg.NewContext(cfg.Width, cfg.Height)
	faceRegular, err := loadFontFace(pretendardRegularData, fontSizePrice)
	if err != nil {
		return nil, fmt.Errorf("load regular font: %w", err)
	}
	faceSemiBold, err := loadFontFace(pretendardSemiBoldData, fontSizeHeader)
	if err != nil {
		return nil, fmt.Errorf("load semibold font: %w", err)
	}
	dc.SetHexColor(hexBg)
	dc.Clear()

	faceAxis, _ := loadFontFace(pretendardRegularData, fontSizeAxis)
	lc := resolveLineColor(data.Direction)
	layout := resolveChartLayout(data, cfg)

	if candleMode {
		// Derive scale from candle OHLC data.
		scale := resolveCandleScale(data)
		priceToY := func(price float64) float64 {
			return scale.toYf(layout, price)
		}

		drawGridWithYAxisLabels(dc, layout, scale.toPriceScale(), faceAxis)

		// Candlestick rendering.
		drawCandlesticks(dc, data.Candles, layout, priceToY)
		drawVolumeBarsCandle(dc, data.Candles, layout)

		// Current price dashed line.
		currentClose := data.Candles[len(data.Candles)-1].Close
		currentY := priceToY(currentClose)
		drawDashed(dc, layout.marginLeft, currentY, layout.marginLeft+layout.chartW, currentY, dashedStyle{R: lc.R, G: lc.G, B: lc.B, A: 0.3, Width: 1, Dash: 8, Gap: 6})
		drawCurrentPointGlowColored(dc, point{X: layout.marginLeft + layout.chartW, Y: currentY}, lc)

		// Pinpoint annotations.
		pinpoints := DetectPinpoints(data.Candles, data.AssetType, data.PeriodLabel)
		facePinpoint, _ := loadFontFace(pretendardRegularData, fontSizeAxis)
		drawPinpointAnnotations(dc, pinpoints, layout, priceToY, facePinpoint, len(data.Candles))

		// Price badge for current price on Y-axis.
		dc.SetFontFace(faceRegular)
		badgeX := layout.marginLeft + layout.chartW + 8
		drawPriceBadge(dc, badgeX, currentY, currentClose, lc.Hex)

		drawProductHeader(dc, data.ProductName, layout, faceSemiBold, data.SubTitle != "")
		drawSubTitle(dc, data.SubTitle, data.Direction, layout, faceRegular)
		drawXAxisLabels(dc, data, layout, faceAxis, cfg)
	} else {
		// Legacy line chart fallback.
		scale := resolvePriceScale(data)
		priceToY := func(price float64) float64 {
			return scale.toY(layout, price)
		}
		points := buildPricePoints(data.Prices, layout, priceToY)
		last := points[len(points)-1]

		drawGridWithYAxisLabels(dc, layout, scale, faceAxis)
		drawReferenceLines(dc, data, layout, priceToY)
		drawGradientFill(dc, points, gradientFillSpec{
			bottomY: layout.marginTop + layout.chartH,
			leftX:   layout.marginLeft,
			rightX:  layout.marginLeft + layout.chartW,
			width:   cfg.Width,
			height:  cfg.Height,
		}, lc)
		drawSplineCurveColored(dc, points, lc)
		drawDashed(dc, layout.marginLeft, last.Y, layout.marginLeft+layout.chartW, last.Y, dashedStyle{R: lc.R, G: lc.G, B: lc.B, A: 0.3, Width: 1, Dash: 8, Gap: 6})
		drawCurrentPointGlowColored(dc, last, lc)
		drawVolumeBars(dc, data, layout, lc)
		drawPriceBadgesForSeriesColored(dc, data, layout, priceToY, faceRegular, last.Y, lc)
		drawProductHeader(dc, data.ProductName, layout, faceSemiBold, data.SubTitle != "")
		drawSubTitle(dc, data.SubTitle, data.Direction, layout, faceRegular)
		drawXAxisLabels(dc, data, layout, faceAxis, cfg)
	}

	var buf bytes.Buffer
	if err := encodePNGFast(&buf, dc.Image()); err != nil {
		return nil, fmt.Errorf("encode PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// --- Candlestick rendering ---

// candleScale handles float64 price scaling for OHLC data.
type candleScale struct {
	minP       float64
	maxP       float64
	priceRange float64
}

func resolveCandleScale(data PriceChartData) candleScale {
	if len(data.Candles) == 0 {
		return candleScale{}
	}
	minP := data.Candles[0].Low
	maxP := data.Candles[0].High
	for _, c := range data.Candles {
		if c.Low < minP {
			minP = c.Low
		}
		if c.High > maxP {
			maxP = c.High
		}
	}
	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = math.Max(1, maxP/20)
		minP -= priceRange
		maxP += priceRange
		priceRange = maxP - minP
	}
	pad := math.Max(1, priceRange/20)
	minP -= pad
	maxP += pad
	return candleScale{minP: minP, maxP: maxP, priceRange: maxP - minP}
}

func (s candleScale) toYf(layout chartLayout, price float64) float64 {
	ratio := (price - s.minP) / s.priceRange
	return layout.marginTop + layout.chartH - ratio*layout.chartH
}

func (s candleScale) toPriceScale() priceScale {
	return priceScale{minP: s.minP, maxP: s.maxP, priceRange: s.priceRange}
}

const (
	hexBullish = "#26A69A"
	hexBearish = "#EF5350"
	hexDoji    = "#787B86"
)

func drawCandlesticks(dc *gg.Context, candles []CandlePoint, layout chartLayout, priceToY func(float64) float64) {
	n := len(candles)
	if n == 0 {
		return
	}

	style := resolveCandlestickStyle(layout, n)

	for i, c := range candles {
		drawSingleCandlestick(dc, c, style, i, priceToY)
	}
}

func drawVolumeBarsCandle(dc *gg.Context, candles []CandlePoint, layout chartLayout) {
	n := len(candles)
	if n == 0 {
		return
	}

	var maxVol float64
	for _, c := range candles {
		if c.Volume > maxVol {
			maxVol = c.Volume
		}
	}
	if maxVol == 0 {
		return
	}

	volAreaH := layout.chartH * 0.15
	volBaseY := layout.marginTop + layout.chartH
	candleSpacing := layout.chartW / float64(n)
	barW := candleSpacing * 0.50
	if barW > 12 {
		barW = 12
	}
	if barW < 1 {
		barW = 1
	}

	for i, c := range candles {
		if c.Volume <= 0 {
			continue
		}
		x := layout.marginLeft + float64(i)*candleSpacing + candleSpacing/2

		ratio := c.Volume / maxVol
		barH := ratio * volAreaH
		y := volBaseY - barH

		if c.Close >= c.Open {
			dc.SetRGBA(lowestR, lowestG, lowestB, 0.3) // green
		} else {
			dc.SetRGBA(highestR, highestG, highestB, 0.3) // red
		}
		dc.DrawRectangle(x-barW/2, y, barW, barH)
		dc.Fill()
	}
}

// encodePNGFast converts RGBA to NRGBA (avoids per-pixel alpha conversion overhead)
// and encodes with BestSpeed for ~10x faster PNG encoding.
func encodePNGFast(buf *bytes.Buffer, img image.Image) error {
	rgba, ok := img.(*image.RGBA)
	if !ok {
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		return enc.Encode(buf, img)
	}
	bounds := rgba.Bounds()
	nrgba := &image.NRGBA{
		Pix:    rgba.Pix, // same memory layout when alpha is 0xFF (opaque)
		Stride: rgba.Stride,
		Rect:   bounds,
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(buf, nrgba)
}

// DrawPriceChartBase64 generates a chart and returns it as a base64-encoded string.
func DrawPriceChartBase64(data PriceChartData, cfg ChartConfig) (string, error) {
	pngBytes, err := DrawPriceChart(data, cfg)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pngBytes), nil
}

// --- internal types & helpers ---

type point struct{ X, Y float64 }
type chartLayout struct {
	marginLeft    float64
	marginRight   float64
	marginTop     float64
	marginBottom  float64
	chartW        float64
	chartH        float64
	hasTimestamps bool
}

type priceScale struct {
	minP       float64
	maxP       float64
	priceRange float64
}

type dashedStyle struct {
	R     float64
	G     float64
	B     float64
	A     float64
	Width float64
	Dash  float64
	Gap   float64
}

func resolveChartLayout(data PriceChartData, cfg ChartConfig) chartLayout {
	layout := chartLayout{
		marginLeft:   100,
		marginRight:  110,
		marginTop:    20,
		marginBottom: 20,
	}
	if data.ProductName != "" {
		layout.marginTop = 52
	}
	if data.SubTitle != "" {
		layout.marginTop += 28
	}
	layout.hasTimestamps = len(data.Timestamps) == len(data.Prices) && len(data.Timestamps) >= 2
	if layout.hasTimestamps {
		layout.marginBottom = max(layout.marginBottom, 48.0)
	}
	if data.PeriodLabel != "" {
		layout.marginBottom = max(layout.marginBottom, 44.0)
	}
	layout.chartW = float64(cfg.Width) - layout.marginLeft - layout.marginRight
	layout.chartH = float64(cfg.Height) - layout.marginTop - layout.marginBottom
	return layout
}

func resolvePriceScale(data PriceChartData) priceScale {
	minP, maxP := float64(data.Prices[0]), float64(data.Prices[0])
	for _, p := range data.Prices {
		value := float64(p)
		if value < minP {
			minP = value
		}
		if value > maxP {
			maxP = value
		}
	}
	if data.LowestPrice > 0 && float64(data.LowestPrice) < minP {
		minP = float64(data.LowestPrice)
	}
	if data.HighestPrice > 0 && float64(data.HighestPrice) > maxP {
		maxP = float64(data.HighestPrice)
	}
	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = math.Max(1, maxP/20)
		minP -= priceRange
		maxP += priceRange
		priceRange = maxP - minP
	}
	pad := math.Max(1, priceRange/20)
	minP -= pad
	maxP += pad
	return priceScale{
		minP:       minP,
		maxP:       maxP,
		priceRange: maxP - minP,
	}
}

func (s priceScale) toY(layout chartLayout, price float64) float64 {
	ratio := (price - s.minP) / s.priceRange
	return layout.marginTop + layout.chartH - ratio*layout.chartH
}

func buildPricePoints(prices []int, layout chartLayout, toY func(float64) float64) []point {
	n := len(prices)
	pts := make([]point, n)
	for i, price := range prices {
		x := layout.marginLeft + float64(i)*layout.chartW/float64(n-1)
		pts[i] = point{X: x, Y: toY(float64(price))}
	}
	return pts
}

func drawGridWithYAxisLabels(dc *gg.Context, layout chartLayout, scale priceScale, faceAxis font.Face) {
	dc.SetLineWidth(1)
	for i := 0; i <= 4; i++ {
		y := layout.marginTop + float64(i)*layout.chartH/4
		dc.SetHexColor(hexGrid)
		dc.DrawLine(layout.marginLeft, y, layout.marginLeft+layout.chartW, y)
		dc.Stroke()
		gridPrice := scale.maxP - float64(i)*scale.priceRange/4
		if faceAxis == nil {
			continue
		}
		dc.SetFontFace(faceAxis)
		dc.SetHexColor(hexTextDim)
		dc.DrawStringAnchored(formatPrice(gridPrice), layout.marginLeft-4, y, 1.0, 0.5)
	}
}

func drawReferenceLines(dc *gg.Context, data PriceChartData, layout chartLayout, priceToY func(float64) float64) {
	if data.HighestPrice > 0 {
		y := priceToY(float64(data.HighestPrice))
		drawDashed(dc, layout.marginLeft, y, layout.marginLeft+layout.chartW, y, dashedStyle{
			R: highestR, G: highestG, B: highestB, A: 0.4, Width: 1.5, Dash: 10, Gap: 6,
		})
	}
	if data.LowestPrice > 0 {
		y := priceToY(float64(data.LowestPrice))
		drawDashed(dc, layout.marginLeft, y, layout.marginLeft+layout.chartW, y, dashedStyle{
			R: lowestR, G: lowestG, B: lowestB, A: 0.6, Width: 1.5, Dash: 10, Gap: 6,
		})
	}
}

func drawSplineCurveColored(dc *gg.Context, points []point, lc lineColor) {
	dc.SetHexColor(lc.Hex)
	dc.SetLineWidth(3)
	dc.SetLineCap(gg.LineCapRound)
	dc.SetLineJoin(gg.LineJoinRound)
	buildSplinePath(dc, points)
	dc.Stroke()
}

func drawCurrentPointGlowColored(dc *gg.Context, pt point, lc lineColor) {
	dc.SetRGBA(lc.R, lc.G, lc.B, 0.25)
	dc.DrawCircle(pt.X, pt.Y, 12)
	dc.Fill()
	dc.SetRGBA(lc.R, lc.G, lc.B, 0.5)
	dc.DrawCircle(pt.X, pt.Y, 7)
	dc.Fill()
	dc.SetHexColor("#FFFFFF")
	dc.DrawCircle(pt.X, pt.Y, 4)
	dc.Fill()
}

func drawPriceBadgesForSeriesColored(dc *gg.Context, data PriceChartData, layout chartLayout, priceToY func(float64) float64, face font.Face, currentY float64, lc lineColor) {
	dc.SetFontFace(face)
	badgeX := layout.marginLeft + layout.chartW + 8
	currentPrice := float64(data.Prices[len(data.Prices)-1])
	drawPriceBadge(dc, badgeX, currentY, currentPrice, lc.Hex)
	if data.LowestPrice > 0 && float64(data.LowestPrice) != currentPrice {
		drawPriceBadge(dc, badgeX, priceToY(float64(data.LowestPrice)), float64(data.LowestPrice), hexLowest)
	}
	if data.HighestPrice > 0 && float64(data.HighestPrice) != currentPrice {
		drawPriceBadge(dc, badgeX, priceToY(float64(data.HighestPrice)), float64(data.HighestPrice), hexHighest)
	}
}

func drawSubTitle(dc *gg.Context, subTitle string, dir ChartDirection, layout chartLayout, face font.Face) {
	if subTitle == "" || face == nil {
		return
	}
	dc.SetFontFace(face)
	lc := resolveLineColor(dir)
	dc.SetHexColor(lc.Hex)
	dc.DrawString(subTitle, layout.marginLeft, layout.marginTop-16)
}

func drawVolumeBars(dc *gg.Context, data PriceChartData, layout chartLayout, lc lineColor) {
	if len(data.Volumes) == 0 || len(data.Volumes) != len(data.Prices) {
		return
	}
	// Check if there's any non-zero volume
	var maxVol float64
	for _, v := range data.Volumes {
		if v > maxVol {
			maxVol = v
		}
	}
	if maxVol == 0 {
		return
	}

	n := len(data.Volumes)
	volAreaH := layout.chartH * 0.18 // bottom 18% of chart area
	volBaseY := layout.marginTop + layout.chartH
	barW := layout.chartW / float64(n)
	if barW > 12 {
		barW = 12
	}

	for i, vol := range data.Volumes {
		if vol <= 0 {
			continue
		}
		ratio := vol / maxVol
		barH := ratio * volAreaH
		x := layout.marginLeft + float64(i)*layout.chartW/float64(n-1) - barW/2
		y := volBaseY - barH
		dc.SetRGBA(lc.R, lc.G, lc.B, 0.2)
		dc.DrawRectangle(x, y, barW, barH)
		dc.Fill()
	}
}

func drawProductHeader(dc *gg.Context, productName string, layout chartLayout, faceSemiBold font.Face, hasSubTitle bool) {
	if productName == "" || faceSemiBold == nil {
		return
	}
	dc.SetFontFace(faceSemiBold)
	dc.SetHexColor(hexText)
	name := truncateString(dc, productName, layout.chartW+layout.marginRight-8)
	y := layout.marginTop - 16
	if hasSubTitle {
		y = layout.marginTop - 42 // push header above subtitle
	}
	dc.DrawString(name, layout.marginLeft, y)
}

func drawXAxisLabels(dc *gg.Context, data PriceChartData, layout chartLayout, faceAxis font.Face, cfg ChartConfig) {
	if layout.hasTimestamps && faceAxis != nil {
		drawTimestampAxisLabels(dc, data.Timestamps, layout, faceAxis)
		return
	}
	if data.PeriodLabel == "" {
		return
	}
	periodFace, _ := loadFontFace(pretendardRegularData, fontSizePeriod)
	if periodFace != nil {
		dc.SetFontFace(periodFace)
	}
	dc.SetHexColor(hexTextDim)
	dc.DrawStringAnchored(data.PeriodLabel, layout.marginLeft+layout.chartW/2, float64(cfg.Height)-12, 0.5, 0.5)
}

func drawTimestampAxisLabels(dc *gg.Context, timestamps []time.Time, layout chartLayout, face font.Face) {
	dc.SetFontFace(face)
	dc.SetHexColor(hexTextDim)
	labelY := layout.marginTop + layout.chartH + 24
	first := timestamps[0]
	last := timestamps[len(timestamps)-1]
	dc.DrawStringAnchored(formatDateLabel(first), layout.marginLeft, labelY, 0.0, 0.5)
	dc.DrawStringAnchored(formatDateLabel(last), layout.marginLeft+layout.chartW, labelY, 1.0, 0.5)
	if len(timestamps) >= 5 {
		mid := timestamps[len(timestamps)/2]
		dc.DrawStringAnchored(formatDateLabel(mid), layout.marginLeft+layout.chartW/2, labelY, 0.5, 0.5)
	}
}

// buildSplinePath creates a Catmull-Rom spline path through all points.
func buildSplinePath(dc *gg.Context, pts []point) {
	if len(pts) < 2 {
		return
	}
	dc.MoveTo(pts[0].X, pts[0].Y)
	if len(pts) == 2 {
		dc.LineTo(pts[1].X, pts[1].Y)
		return
	}

	for i := 0; i < len(pts)-1; i++ {
		// Virtual neighbours for boundary segments.
		var p0, p3 point
		if i == 0 {
			p0 = point{2*pts[0].X - pts[1].X, 2*pts[0].Y - pts[1].Y}
		} else {
			p0 = pts[i-1]
		}
		if i == len(pts)-2 {
			p3 = point{2*pts[len(pts)-1].X - pts[len(pts)-2].X, 2*pts[len(pts)-1].Y - pts[len(pts)-2].Y}
		} else {
			p3 = pts[i+2]
		}

		p1 := pts[i]
		p2 := pts[i+1]

		// Catmull-Rom to Cubic Bezier control points.
		cp1x := p1.X + (p2.X-p0.X)/6.0
		cp1y := p1.Y + (p2.Y-p0.Y)/6.0
		cp2x := p2.X - (p3.X-p1.X)/6.0
		cp2y := p2.Y - (p3.Y-p1.Y)/6.0

		// Monotone clamp: prevent overshooting.
		minY := math.Min(p1.Y, p2.Y)
		maxY := math.Max(p1.Y, p2.Y)
		cp1y = clamp(cp1y, minY, maxY)
		cp2y = clamp(cp2y, minY, maxY)

		dc.CubicTo(cp1x, cp1y, cp2x, cp2y, p2.X, p2.Y)
	}
}

// drawGradientFill fills the area under the spline curve with a fading gradient.
// Uses a separate gg.Context to avoid clip state leaking into the main context.
func drawGradientFill(dc *gg.Context, pts []point, spec gradientFillSpec, lc lineColor) {
	if len(pts) < 2 {
		return
	}

	// Find top of curve.
	topY := spec.bottomY
	for _, p := range pts {
		if p.Y < topY {
			topY = p.Y
		}
	}
	height := spec.bottomY - topY
	if height <= 0 {
		return
	}

	// Render gradient on a temporary context to isolate the clip.
	tmp := gg.NewContext(spec.width, spec.height)
	buildSplinePath(tmp, pts)
	tmp.LineTo(pts[len(pts)-1].X, spec.bottomY)
	tmp.LineTo(pts[0].X, spec.bottomY)
	tmp.ClosePath()
	tmp.Clip()

	stripH := 2.0
	for y := topY; y < spec.bottomY; y += stripH {
		t := (y - topY) / height
		alpha := 0.35 * (1.0 - t*t) // quadratic falloff
		tmp.SetRGBA(lc.R, lc.G, lc.B, alpha)
		tmp.DrawRectangle(spec.leftX, y, spec.rightX-spec.leftX, stripH)
		tmp.Fill()
	}

	// Composite onto main context.
	dc.DrawImage(tmp.Image(), 0, 0)
}

type candlestickStyle struct {
	startX  float64
	spacing float64
	bodyW   float64
	barMode bool
}

func resolveCandlestickStyle(layout chartLayout, candleCount int) candlestickStyle {
	spacing := layout.chartW / float64(candleCount)
	bodyWidth := spacing * 0.50
	if bodyWidth < 1 {
		bodyWidth = 1
	}
	return candlestickStyle{
		startX:  layout.marginLeft,
		spacing: spacing,
		bodyW:   bodyWidth,
		barMode: bodyWidth < 3.0,
	}
}

func drawSingleCandlestick(dc *gg.Context, candle CandlePoint, style candlestickStyle, index int, priceToY func(float64) float64) {
	x := style.startX + float64(index)*style.spacing + style.spacing/2
	yOpen := priceToY(candle.Open)
	yClose := priceToY(candle.Close)
	yHigh := priceToY(candle.High)
	yLow := priceToY(candle.Low)

	dc.SetHexColor(resolveCandlestickColor(candle))
	drawCandlestickWick(dc, x, yHigh, yLow)
	if style.barMode {
		return
	}
	drawCandlestickBody(dc, x, yOpen, yClose, style.bodyW)
}

func resolveCandlestickColor(candle CandlePoint) string {
	if candle.Close == candle.Open {
		return hexDoji
	}
	if candle.Close > candle.Open {
		return hexBullish
	}
	return hexBearish
}

func drawCandlestickWick(dc *gg.Context, x, yHigh, yLow float64) {
	dc.SetLineWidth(1)
	dc.DrawLine(x, yHigh, x, yLow)
	dc.Stroke()
}

func drawCandlestickBody(dc *gg.Context, x, yOpen, yClose, bodyWidth float64) {
	bodyTop := math.Min(yOpen, yClose)
	bodyH := math.Abs(yOpen - yClose)
	if bodyH < 1 {
		bodyH = 1
	}
	dc.DrawRectangle(x-bodyWidth/2, bodyTop, bodyWidth, bodyH)
	dc.Fill()
}

type gradientFillSpec struct {
	bottomY float64
	leftX   float64
	rightX  float64
	width   int
	height  int
}

// drawDashed draws a horizontal dashed line.
func drawDashed(dc *gg.Context, x0, y0, x1, y1 float64, style dashedStyle) {
	dc.SetRGBA(style.R, style.G, style.B, style.A)
	dc.SetLineWidth(style.Width)
	dc.SetDash(style.Dash, style.Gap)
	dc.DrawLine(x0, y0, x1, y1)
	dc.Stroke()
	dc.SetDash() // reset
}

// drawPriceBadge draws a rounded price label badge.
func drawPriceBadge(dc *gg.Context, x, y float64, price float64, bgHex string) {
	label := formatPrice(price)
	w, h := dc.MeasureString(label)
	padX := 10.0
	padY := 6.0
	bw := w + padX*2
	bh := h + padY*2
	bx := x
	by := y - bh/2
	radius := 4.0

	// Background
	dc.SetHexColor(bgHex)
	dc.DrawRoundedRectangle(bx, by, bw, bh, radius)
	dc.Fill()

	// Text
	dc.SetHexColor("#FFFFFF")
	dc.DrawString(label, bx+padX, by+padY+h)
}

// formatPrice formats a price like "19,300.00".
func formatPrice(price float64) string {
	sign := ""
	if price < 0 {
		sign = "-"
		price = -price
	}

	parts := strings.SplitN(fmt.Sprintf("%.2f", price), ".", 2)
	intPart := parts[0]
	fracPart := "00"
	if len(parts) == 2 {
		fracPart = parts[1]
	}

	if len(intPart) <= 3 {
		return sign + intPart + "." + fracPart
	}

	result := make([]byte, 0, len(intPart)+len(intPart)/3+1+len(fracPart))
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	result = append(result, '.')
	result = append(result, fracPart...)
	return sign + string(result)
}

// truncateString truncates a string to fit within maxWidth, adding "..." if needed.
func truncateString(dc *gg.Context, s string, maxWidth float64) string {
	w, _ := dc.MeasureString(s)
	if w <= maxWidth {
		return s
	}
	runes := []rune(s)
	for i := len(runes) - 1; i > 0; i-- {
		candidate := string(runes[:i]) + "..."
		w, _ = dc.MeasureString(candidate)
		if w <= maxWidth {
			return candidate
		}
	}
	return "..."
}

// formatDateLabel formats a time as "1/18" (month/day) for X-axis labels.
func formatDateLabel(t time.Time) string {
	return fmt.Sprintf("%d/%d", t.Month(), t.Day())
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// lineColor resolves the RGB components and hex color for the main chart line.
type lineColor struct {
	R, G, B float64
	Hex     string
}

func resolveLineColor(dir ChartDirection) lineColor {
	switch dir {
	case DirectionUp:
		return lineColor{R: lowestR, G: lowestG, B: lowestB, Hex: hexLowest}
	case DirectionDown:
		return lineColor{R: highestR, G: highestG, B: highestB, Hex: hexHighest}
	default:
		return lineColor{R: lineR, G: lineG, B: lineB, Hex: hexLine}
	}
}
