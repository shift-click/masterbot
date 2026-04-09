package command

import "strings"

// Location represents a geographic location with its coordinates.
type Location struct {
	Name      string  // full name (e.g. "서울특별시 강남구")
	ShortName string  // short name (e.g. "강남구")
	City      string  // parent city (e.g. "서울")
	Lat       float64
	Lon       float64
}

// nationalCities are the 10 major cities shown in the national overview.
var nationalCities = []Location{
	{Name: "서울", ShortName: "서울", City: "서울", Lat: 37.5665, Lon: 126.9780},
	{Name: "인천", ShortName: "인천", City: "인천", Lat: 37.4563, Lon: 126.7052},
	{Name: "수원", ShortName: "수원", City: "수원", Lat: 37.2636, Lon: 127.0286},
	{Name: "춘천", ShortName: "춘천", City: "춘천", Lat: 37.8813, Lon: 127.7300},
	{Name: "강릉", ShortName: "강릉", City: "강릉", Lat: 37.7519, Lon: 128.8761},
	{Name: "대전", ShortName: "대전", City: "대전", Lat: 36.3504, Lon: 127.3845},
	{Name: "광주", ShortName: "광주", City: "광주", Lat: 35.1595, Lon: 126.8526},
	{Name: "대구", ShortName: "대구", City: "대구", Lat: 35.8714, Lon: 128.6014},
	{Name: "부산", ShortName: "부산", City: "부산", Lat: 35.1796, Lon: 129.0756},
	{Name: "제주", ShortName: "제주", City: "제주", Lat: 33.4996, Lon: 126.5312},
}

// allLocations contains all known locations for lookup.
var allLocations = func() []Location {
	locs := make([]Location, 0, 128)
	locs = append(locs, nationalCities...)

	// Seoul districts (25 자치구)
	seoulDistricts := []Location{
		{Name: "서울특별시 강남구", ShortName: "강남구", City: "서울", Lat: 37.4979, Lon: 127.0276},
		{Name: "서울특별시 강동구", ShortName: "강동구", City: "서울", Lat: 37.5301, Lon: 127.1238},
		{Name: "서울특별시 강북구", ShortName: "강북구", City: "서울", Lat: 37.6396, Lon: 127.0255},
		{Name: "서울특별시 강서구", ShortName: "강서구", City: "서울", Lat: 37.5510, Lon: 126.8495},
		{Name: "서울특별시 관악구", ShortName: "관악구", City: "서울", Lat: 37.4784, Lon: 126.9516},
		{Name: "서울특별시 광진구", ShortName: "광진구", City: "서울", Lat: 37.5384, Lon: 127.0822},
		{Name: "서울특별시 구로구", ShortName: "구로구", City: "서울", Lat: 37.4954, Lon: 126.8874},
		{Name: "서울특별시 금천구", ShortName: "금천구", City: "서울", Lat: 37.4519, Lon: 126.8955},
		{Name: "서울특별시 노원구", ShortName: "노원구", City: "서울", Lat: 37.6542, Lon: 127.0568},
		{Name: "서울특별시 도봉구", ShortName: "도봉구", City: "서울", Lat: 37.6688, Lon: 127.0471},
		{Name: "서울특별시 동대문구", ShortName: "동대문구", City: "서울", Lat: 37.5744, Lon: 127.0396},
		{Name: "서울특별시 동작구", ShortName: "동작구", City: "서울", Lat: 37.5124, Lon: 126.9393},
		{Name: "서울특별시 마포구", ShortName: "마포구", City: "서울", Lat: 37.5663, Lon: 126.9014},
		{Name: "서울특별시 서대문구", ShortName: "서대문구", City: "서울", Lat: 37.5791, Lon: 126.9368},
		{Name: "서울특별시 서초구", ShortName: "서초구", City: "서울", Lat: 37.4837, Lon: 127.0324},
		{Name: "서울특별시 성동구", ShortName: "성동구", City: "서울", Lat: 37.5633, Lon: 127.0371},
		{Name: "서울특별시 성북구", ShortName: "성북구", City: "서울", Lat: 37.5894, Lon: 127.0167},
		{Name: "서울특별시 송파구", ShortName: "송파구", City: "서울", Lat: 37.5145, Lon: 127.1050},
		{Name: "서울특별시 양천구", ShortName: "양천구", City: "서울", Lat: 37.5170, Lon: 126.8666},
		{Name: "서울특별시 영등포구", ShortName: "영등포구", City: "서울", Lat: 37.5264, Lon: 126.8963},
		{Name: "서울특별시 용산구", ShortName: "용산구", City: "서울", Lat: 37.5326, Lon: 126.9910},
		{Name: "서울특별시 은평구", ShortName: "은평구", City: "서울", Lat: 37.6027, Lon: 126.9291},
		{Name: "서울특별시 종로구", ShortName: "종로구", City: "서울", Lat: 37.5735, Lon: 126.9790},
		{Name: "서울특별시 중구", ShortName: "중구", City: "서울", Lat: 37.5641, Lon: 126.9979},
		{Name: "서울특별시 중랑구", ShortName: "중랑구", City: "서울", Lat: 37.6066, Lon: 127.0927},
	}
	locs = append(locs, seoulDistricts...)

	// Other major cities
	others := []Location{
		{Name: "성남", ShortName: "성남", City: "성남", Lat: 37.4200, Lon: 127.1267},
		{Name: "용인", ShortName: "용인", City: "용인", Lat: 37.2411, Lon: 127.1776},
		{Name: "고양", ShortName: "고양", City: "고양", Lat: 37.6584, Lon: 126.8320},
		{Name: "안양", ShortName: "안양", City: "안양", Lat: 37.3943, Lon: 126.9568},
		{Name: "안산", ShortName: "안산", City: "안산", Lat: 37.3219, Lon: 126.8309},
		{Name: "화성", ShortName: "화성", City: "화성", Lat: 37.1997, Lon: 126.8312},
		{Name: "평택", ShortName: "평택", City: "평택", Lat: 36.9922, Lon: 127.1128},
		{Name: "시흥", ShortName: "시흥", City: "시흥", Lat: 37.3800, Lon: 126.8032},
		{Name: "파주", ShortName: "파주", City: "파주", Lat: 37.7599, Lon: 126.7801},
		{Name: "김포", ShortName: "김포", City: "김포", Lat: 37.6153, Lon: 126.7156},
		{Name: "광명", ShortName: "광명", City: "광명", Lat: 37.4786, Lon: 126.8641},
		{Name: "군포", ShortName: "군포", City: "군포", Lat: 37.3614, Lon: 126.9352},
		{Name: "하남", ShortName: "하남", City: "하남", Lat: 37.5393, Lon: 127.2148},
		{Name: "의정부", ShortName: "의정부", City: "의정부", Lat: 37.7381, Lon: 127.0337},
		{Name: "청주", ShortName: "청주", City: "청주", Lat: 36.6424, Lon: 127.4890},
		{Name: "천안", ShortName: "천안", City: "천안", Lat: 36.8151, Lon: 127.1139},
		{Name: "전주", ShortName: "전주", City: "전주", Lat: 35.8242, Lon: 127.1480},
		{Name: "포항", ShortName: "포항", City: "포항", Lat: 36.0190, Lon: 129.3435},
		{Name: "창원", ShortName: "창원", City: "창원", Lat: 35.2280, Lon: 128.6811},
		{Name: "울산", ShortName: "울산", City: "울산", Lat: 35.5384, Lon: 129.3114},
		{Name: "세종", ShortName: "세종", City: "세종", Lat: 36.4800, Lon: 127.2590},
		{Name: "원주", ShortName: "원주", City: "원주", Lat: 37.3422, Lon: 127.9202},
		{Name: "속초", ShortName: "속초", City: "속초", Lat: 38.2070, Lon: 128.5918},
		{Name: "여수", ShortName: "여수", City: "여수", Lat: 34.7604, Lon: 127.6622},
		{Name: "목포", ShortName: "목포", City: "목포", Lat: 34.8118, Lon: 126.3922},
		{Name: "순천", ShortName: "순천", City: "순천", Lat: 34.9506, Lon: 127.4872},
		{Name: "경주", ShortName: "경주", City: "경주", Lat: 35.8562, Lon: 129.2247},
		{Name: "김해", ShortName: "김해", City: "김해", Lat: 35.2285, Lon: 128.8894},
		{Name: "제주시", ShortName: "제주시", City: "제주", Lat: 33.5104, Lon: 126.5219},
		{Name: "서귀포", ShortName: "서귀포", City: "제주", Lat: 33.2541, Lon: 126.5600},
	}
	locs = append(locs, others...)

	return locs
}()

// locationIndex is a map from normalized name to Location for fast lookup.
var locationIndex = buildLocationIndex(allLocations)

func buildLocationIndex(locs []Location) map[string]Location {
	idx := make(map[string]Location, len(locs)*3)
	for _, loc := range locs {
		key := normalizeLocationName(loc.ShortName)
		if _, exists := idx[key]; !exists {
			idx[key] = loc
		}
		key = normalizeLocationName(loc.Name)
		if _, exists := idx[key]; !exists {
			idx[key] = loc
		}
		// abbreviated form without suffix (구/시/군)
		stripped := stripLocationSuffix(loc.ShortName)
		if stripped != loc.ShortName {
			key = normalizeLocationName(stripped)
			if _, exists := idx[key]; !exists {
				idx[key] = loc
			}
		}
	}
	return idx
}

func normalizeLocationName(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

func stripLocationSuffix(name string) string {
	for _, suffix := range []string{"특별시", "광역시", "특별자치시", "특별자치도", "구", "시", "군"} {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

// LookupLocation finds a Location by user input.
func LookupLocation(input string) (Location, bool) {
	key := normalizeLocationName(strings.TrimSpace(input))
	if key == "" {
		return Location{}, false
	}
	loc, ok := locationIndex[key]
	return loc, ok
}

// NationalCities returns the 10 major cities for national overview.
func NationalCities() []Location {
	out := make([]Location, len(nationalCities))
	copy(out, nationalCities)
	return out
}
