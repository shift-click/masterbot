package chart

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/fogleman/gg"
	"golang.org/x/image/font"
)

// Pinpoint represents a detected key price point on the chart.
type Pinpoint struct {
	Index  int
	Time   time.Time
	Price  float64
	IsHigh bool // true = swing high, false = swing low
}

// DetectPinpoints runs a Zig-Zag + RDP pipeline to find key price points.
func DetectPinpoints(candles []CandlePoint, assetType string, periodLabel string) []Pinpoint {
	if len(candles) < 3 {
		return nil
	}

	maxPoints := maxPinpointsForPeriod(periodLabel)
	deviation := adaptiveDeviation(assetType, periodLabel)

	// Stage 1: Zig-Zag swing detection.
	swings := zigzagSwings(candles, deviation)
	if len(swings) == 0 {
		return nil
	}

	// Stage 2: RDP filtering — reserve 2 slots for global extremes.
	rdpTarget := maxPoints - 2
	if rdpTarget < 1 {
		rdpTarget = 1
	}
	filtered := rdpFilterToCount(swings, candles, rdpTarget)

	// Ensure global high and low are included.
	filtered = ensureExtremes(filtered, candles)

	// Final cap: keep extremes by removing least-significant interior points.
	if len(filtered) > maxPoints {
		filtered = capPreservingExtremes(filtered, candles, maxPoints)
	}

	return filtered
}

func maxPinpointsForPeriod(periodLabel string) int {
	switch periodLabel {
	case "1일":
		return 3
	case "1주", "1개월":
		return 4
	default: // 3개월, 6개월, 1년
		return 5
	}
}

func adaptiveDeviation(assetType string, periodLabel string) float64 {
	days := periodLabelToDays(periodLabel)
	var base float64
	switch assetType {
	case "coin":
		base = 5.0
	default:
		base = 3.0
	}
	// Logarithmic scaling: dev = base × ln(days) / ln(30)
	// This gives each timeframe a distinct deviation value.
	ln30 := math.Log(30)
	dev := base * math.Log(float64(days)) / ln30
	// Clamp to minimum only (no max clamp — log naturally converges).
	minDev := 2.0
	if assetType == "coin" {
		minDev = 3.0
	}
	if dev < minDev {
		dev = minDev
	}
	return dev
}

func periodLabelToDays(label string) int {
	switch label {
	case "1일":
		return 1
	case "1주":
		return 7
	case "1개월":
		return 30
	case "3개월":
		return 90
	case "6개월":
		return 180
	case "1년":
		return 365
	default:
		return 30
	}
}

// zigzagSwings finds swing highs and lows using a Zig-Zag algorithm.
func zigzagSwings(candles []CandlePoint, deviationPct float64) []Pinpoint {
	if len(candles) < 2 {
		return nil
	}

	state := newSwingState(candles)
	for i := 1; i < len(candles); i++ {
		state.consume(candles, i, deviationPct/100.0)
	}
	return state.swings
}

// rdpFilterToCount applies Ramer-Douglas-Peucker to reduce pinpoints to targetN.
// Uses binary search on epsilon.
func rdpFilterToCount(swings []Pinpoint, candles []CandlePoint, targetN int) []Pinpoint {
	if len(swings) <= targetN {
		return swings
	}

	// Convert swings to (x, y) points for RDP.
	points := make([]xyPoint, len(swings))
	for i, s := range swings {
		points[i] = xyPoint{x: float64(s.Index), y: s.Price, orig: i}
	}

	// Find price range for epsilon scaling.
	minP, maxP := swings[0].Price, swings[0].Price
	for _, s := range swings {
		if s.Price < minP {
			minP = s.Price
		}
		if s.Price > maxP {
			maxP = s.Price
		}
	}
	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = 1
	}

	// Binary search for epsilon that gives us ~ targetN points.
	lo, hi := 0.0, priceRange
	var bestKept []xyPoint
	for iter := 0; iter < 20; iter++ {
		mid := (lo + hi) / 2
		kept := rdpSimplify(points, mid)
		if len(kept) <= targetN {
			hi = mid
			bestKept = kept
		} else {
			lo = mid
		}
	}
	if bestKept == nil {
		// Fallback: just take first targetN.
		return swings[:targetN]
	}
	result := make([]Pinpoint, len(bestKept))
	for i, k := range bestKept {
		result[i] = swings[k.orig]
	}
	return result
}

type xyPoint struct {
	x, y float64
	orig int
}

func rdpSimplify(points []xyPoint, epsilon float64) []xyPoint {
	if len(points) <= 2 {
		return points
	}

	// Find point with max distance from line(first, last).
	first := points[0]
	last := points[len(points)-1]
	maxDist := 0.0
	maxIdx := 0
	for i := 1; i < len(points)-1; i++ {
		d := perpendicularDistance(points[i], first, last)
		if d > maxDist {
			maxDist = d
			maxIdx = i
		}
	}

	if maxDist > epsilon {
		left := rdpSimplify(points[:maxIdx+1], epsilon)
		right := rdpSimplify(points[maxIdx:], epsilon)
		return append(left[:len(left)-1], right...)
	}
	return []xyPoint{first, last}
}

func perpendicularDistance(p, lineStart, lineEnd xyPoint) float64 {
	dx := lineEnd.x - lineStart.x
	dy := lineEnd.y - lineStart.y
	if dx == 0 && dy == 0 {
		return math.Hypot(p.x-lineStart.x, p.y-lineStart.y)
	}
	return math.Abs(dy*p.x-dx*p.y+lineEnd.x*lineStart.y-lineEnd.y*lineStart.x) / math.Hypot(dx, dy)
}

// ensureExtremes adds global high/low if not already present.
func ensureExtremes(pinpoints []Pinpoint, candles []CandlePoint) []Pinpoint {
	if len(candles) == 0 {
		return pinpoints
	}

	globalHighIdx, globalLowIdx := 0, 0
	for i, c := range candles {
		if c.High > candles[globalHighIdx].High {
			globalHighIdx = i
		}
		if c.Low < candles[globalLowIdx].Low {
			globalLowIdx = i
		}
	}

	hasHigh, hasLow := false, false
	for _, p := range pinpoints {
		if p.Index == globalHighIdx {
			hasHigh = true
		}
		if p.Index == globalLowIdx {
			hasLow = true
		}
	}

	if !hasHigh {
		pinpoints = append(pinpoints, Pinpoint{
			Index:  globalHighIdx,
			Time:   candles[globalHighIdx].Time,
			Price:  candles[globalHighIdx].High,
			IsHigh: true,
		})
	}
	if !hasLow && globalLowIdx != globalHighIdx {
		pinpoints = append(pinpoints, Pinpoint{
			Index:  globalLowIdx,
			Time:   candles[globalLowIdx].Time,
			Price:  candles[globalLowIdx].Low,
			IsHigh: false,
		})
	}

	// Sort by index for consistent rendering order.
	sort.Slice(pinpoints, func(i, j int) bool {
		return pinpoints[i].Index < pinpoints[j].Index
	})

	return pinpoints
}

// capPreservingExtremes trims pinpoints to maxN while keeping global high/low.
func capPreservingExtremes(pinpoints []Pinpoint, candles []CandlePoint, maxN int) []Pinpoint {
	if len(pinpoints) <= maxN {
		return pinpoints
	}

	globalHighIdx, globalLowIdx := findGlobalExtremes(candles)
	extremes, rest := splitExtremePinpoints(pinpoints, globalHighIdx, globalLowIdx)
	remaining := maxN - len(extremes)
	if remaining < 0 {
		remaining = 0
	}
	if remaining > len(rest) {
		remaining = len(rest)
	}
	result := append(extremes, rest[:remaining]...)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Index < result[j].Index
	})
	return result
}

// --- Pinpoint annotation rendering ---

// dynamicMarkerSize returns a marker size proportional to candle spacing.
func dynamicMarkerSize(candleSpacing float64) float64 {
	size := candleSpacing * 0.6
	if size < 4.0 {
		size = 4.0
	}
	if size > 12.0 {
		size = 12.0
	}
	return size
}

func drawPinpointAnnotations(dc *gg.Context, pinpoints []Pinpoint, layout chartLayout, priceToY func(float64) float64, face font.Face, totalCandles int) {
	if len(pinpoints) == 0 || face == nil || totalCandles < 1 {
		return
	}
	dc.SetFontFace(face)

	n := len(pinpoints)
	labels := make([]labelInfo, n)
	candleSpacing := layout.chartW / float64(totalCandles)
	markerSize := dynamicMarkerSize(candleSpacing)

	for i, pin := range pinpoints {
		x := layout.marginLeft + float64(pin.Index)*candleSpacing + candleSpacing/2
		y := priceToY(pin.Price)
		text := formatPinpointLabel(pin)

		labels[i] = labelInfo{
			pin:   pin,
			x:     x,
			origY: y,
			text:  text,
			above: pin.IsHigh,
		}
	}

	// Vertical nudge to prevent overlap.
	nudgeLabels(labels, dc, markerSize)

	// Draw annotations.
	chartRight := layout.marginLeft + layout.chartW
	drawCtx := pinpointDrawContext{face: face, markerSize: markerSize, chartRight: chartRight}
	for _, l := range labels {
		drawCtx.drawSinglePinpoint(dc, l.pin, l.x, l.nudgedY, l.text, l.above)
	}
}

func formatPinpointLabel(pin Pinpoint) string {
	priceStr := formatPrice(pin.Price)
	if !pin.Time.IsZero() {
		return fmt.Sprintf("%s %d/%d", priceStr, pin.Time.Month(), pin.Time.Day())
	}
	return priceStr
}

func nudgeLabels(labels []labelInfo, dc *gg.Context, markerSize float64) {
	// Initialize nudgedY from origY for all labels.
	for i := range labels {
		labels[i].nudgedY = labels[i].origY
	}
	if len(labels) < 2 {
		return
	}
	_, textH := dc.MeasureString("0")
	padY := 3.0
	gap6 := 6.0
	badgeH := textH + padY*2
	minGap := badgeH + gap6

	// Compute the badge top-Y for each label (matching drawSinglePinpoint layout).
	type indexed struct {
		idx    int
		badgeY float64
	}
	sorted := make([]indexed, len(labels))
	for i := range labels {
		y := labels[i].origY
		if labels[i].above {
			// Badge is placed above marker: by = y - markerSize - bh - 6
			sorted[i] = indexed{idx: i, badgeY: y - markerSize - badgeH - gap6}
		} else {
			// Badge is placed below marker: by = y + markerSize + 6
			sorted[i] = indexed{idx: i, badgeY: y + markerSize + gap6}
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].badgeY < sorted[j].badgeY })

	// Push overlapping badges downward.
	for i := 1; i < len(sorted); i++ {
		overlap := sorted[i-1].badgeY + minGap - sorted[i].badgeY
		if overlap > 0 {
			sorted[i].badgeY += overlap
		}
	}

	// Write back: convert adjusted badgeY back to the data-point Y that
	// drawSinglePinpoint would need to produce that badge position.
	for _, s := range sorted {
		l := &labels[s.idx]
		if l.above {
			// badgeY = nudgedY - markerSize - badgeH - gap6
			l.nudgedY = s.badgeY + markerSize + badgeH + gap6
		} else {
			// badgeY = nudgedY + markerSize + gap6
			l.nudgedY = s.badgeY - markerSize - gap6
		}
	}
}

type pinpointDrawContext struct {
	face       font.Face
	markerSize float64
	chartRight float64
}

func (ctx pinpointDrawContext) drawSinglePinpoint(dc *gg.Context, pin Pinpoint, x, y float64, text string, above bool) {
	// Marker.
	if pin.IsHigh {
		dc.SetHexColor(hexBearish)
		drawTriangle(dc, x, y-ctx.markerSize-2, ctx.markerSize, false)
	} else {
		dc.SetHexColor(hexBullish)
		drawTriangle(dc, x, y+ctx.markerSize+2, ctx.markerSize, true)
	}

	// Price badge.
	dc.SetFontFace(ctx.face)
	w, h := dc.MeasureString(text)
	padX := 6.0
	padY := 3.0
	bw := w + padX*2
	bh := h + padY*2

	var bx, by float64
	bx = x - bw/2
	if above {
		by = y - ctx.markerSize - bh - 6
	} else {
		by = y + ctx.markerSize + 6
	}

	// Clamp to chart bounds (left and right).
	if bx < 0 {
		bx = 0
	}
	if bx+bw > ctx.chartRight {
		bx = ctx.chartRight - bw
	}

	// Background.
	if pin.IsHigh {
		dc.SetRGBA(highestR, highestG, highestB, 0.85)
	} else {
		dc.SetRGBA(lowestR, lowestG, lowestB, 0.85)
	}
	dc.DrawRoundedRectangle(bx, by, bw, bh, 3)
	dc.Fill()

	// Text.
	dc.SetHexColor("#FFFFFF")
	dc.DrawString(text, bx+padX, by+padY+h)
}

type swingState struct {
	lastHigh       float64
	lastHighIdx    int
	lastLow        float64
	lastLowIdx     int
	lookingForHigh bool
	swings         []Pinpoint
}

func newSwingState(candles []CandlePoint) swingState {
	return swingState{
		lastHigh:       candles[0].High,
		lastHighIdx:    0,
		lastLow:        candles[0].Low,
		lastLowIdx:     0,
		lookingForHigh: candles[1].Close > candles[0].Close,
	}
}

func (s *swingState) consume(candles []CandlePoint, idx int, threshold float64) {
	if s.lookingForHigh {
		s.updateHigh(candles, idx)
		if s.shouldFlipToLow(candles[idx], threshold) {
			s.swings = append(s.swings, pinpointAt(candles, s.lastHighIdx, s.lastHigh, true))
			s.lastLow = candles[idx].Low
			s.lastLowIdx = idx
			s.lookingForHigh = false
		}
		return
	}

	s.updateLow(candles, idx)
	if s.shouldFlipToHigh(candles[idx], threshold) {
		s.swings = append(s.swings, pinpointAt(candles, s.lastLowIdx, s.lastLow, false))
		s.lastHigh = candles[idx].High
		s.lastHighIdx = idx
		s.lookingForHigh = true
	}
}

func (s *swingState) updateHigh(candles []CandlePoint, idx int) {
	if candles[idx].High > s.lastHigh {
		s.lastHigh = candles[idx].High
		s.lastHighIdx = idx
	}
}

func (s *swingState) updateLow(candles []CandlePoint, idx int) {
	if candles[idx].Low < s.lastLow {
		s.lastLow = candles[idx].Low
		s.lastLowIdx = idx
	}
}

func (s swingState) shouldFlipToLow(candle CandlePoint, threshold float64) bool {
	return s.lastHigh > 0 && (s.lastHigh-candle.Low)/s.lastHigh >= threshold
}

func (s swingState) shouldFlipToHigh(candle CandlePoint, threshold float64) bool {
	return s.lastLow > 0 && (candle.High-s.lastLow)/s.lastLow >= threshold
}

func pinpointAt(candles []CandlePoint, idx int, price float64, isHigh bool) Pinpoint {
	return Pinpoint{
		Index:  idx,
		Time:   candles[idx].Time,
		Price:  price,
		IsHigh: isHigh,
	}
}

func findGlobalExtremes(candles []CandlePoint) (int, int) {
	globalHighIdx, globalLowIdx := 0, 0
	for i, candle := range candles {
		if candle.High > candles[globalHighIdx].High {
			globalHighIdx = i
		}
		if candle.Low < candles[globalLowIdx].Low {
			globalLowIdx = i
		}
	}
	return globalHighIdx, globalLowIdx
}

func splitExtremePinpoints(pinpoints []Pinpoint, globalHighIdx, globalLowIdx int) ([]Pinpoint, []Pinpoint) {
	extremes := make([]Pinpoint, 0, 2)
	rest := make([]Pinpoint, 0, len(pinpoints))
	for _, point := range pinpoints {
		if point.Index == globalHighIdx || point.Index == globalLowIdx {
			extremes = append(extremes, point)
			continue
		}
		rest = append(rest, point)
	}
	return extremes, rest
}

func drawTriangle(dc *gg.Context, cx, cy, size float64, pointUp bool) {
	half := size / 2
	if pointUp {
		dc.MoveTo(cx, cy-half)
		dc.LineTo(cx-half, cy+half)
		dc.LineTo(cx+half, cy+half)
	} else {
		dc.MoveTo(cx, cy+half)
		dc.LineTo(cx-half, cy-half)
		dc.LineTo(cx+half, cy-half)
	}
	dc.ClosePath()
	dc.Fill()
}

// labelInfo is used internally for label collision avoidance.
type labelInfo struct {
	pin      Pinpoint
	x, origY float64
	nudgedY  float64 // Y after collision avoidance
	text     string
	above    bool
}
