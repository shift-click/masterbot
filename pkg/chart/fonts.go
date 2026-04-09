package chart

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
)

//go:embed fonts/Pretendard-Regular.ttf
var pretendardRegularData []byte

//go:embed fonts/Pretendard-SemiBold.ttf
var pretendardSemiBoldData []byte

// Singleton parsed fonts — truetype.Font is immutable, safe for concurrent reads.
var (
	parsedRegular     *truetype.Font
	parsedRegularOnce sync.Once
	parsedRegularErr  error

	parsedSemiBold     *truetype.Font
	parsedSemiBoldOnce sync.Once
	parsedSemiBoldErr  error
)

func getParsedFont(data []byte, once *sync.Once, cached **truetype.Font, cachedErr *error) (*truetype.Font, error) {
	once.Do(func() {
		*cached, *cachedErr = truetype.Parse(data)
	})
	return *cached, *cachedErr
}

func loadFontFace(data []byte, size float64) (font.Face, error) {
	var f *truetype.Font
	var err error

	switch &data[0] {
	case &pretendardRegularData[0]:
		f, err = getParsedFont(data, &parsedRegularOnce, &parsedRegular, &parsedRegularErr)
	case &pretendardSemiBoldData[0]:
		f, err = getParsedFont(data, &parsedSemiBoldOnce, &parsedSemiBold, &parsedSemiBoldErr)
	default:
		f, err = truetype.Parse(data)
	}
	if err != nil {
		return nil, fmt.Errorf("parse font: %w", err)
	}
	return truetype.NewFace(f, &truetype.Options{
		Size: size,
	}), nil
}
