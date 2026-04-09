package providers

import (
	"testing"
)

func TestParseCoupangURL_Regular(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantProduct  string
		wantItem     string
		wantVendor   string
		wantErr      bool
	}{
		{
			name:        "regular URL with all params",
			input:       "https://www.coupang.com/vp/products/9334776688?itemId=20787679097&vendorItemId=75190137576",
			wantProduct: "9334776688",
			wantItem:    "20787679097",
			wantVendor:  "75190137576",
		},
		{
			name:        "regular URL without query params",
			input:       "https://www.coupang.com/vp/products/9334776688",
			wantProduct: "9334776688",
		},
		{
			name:        "mobile URL",
			input:       "https://m.coupang.com/vm/products/9334776688?itemId=123",
			wantProduct: "9334776688",
			wantItem:    "123",
		},
		{
			name:        "URL with extra params",
			input:       "https://www.coupang.com/vp/products/9334776688?itemId=20787679097&vendorItemId=75190137576&src=1191000&spec=10999999",
			wantProduct: "9334776688",
			wantItem:    "20787679097",
			wantVendor:  "75190137576",
		},
		{
			name:    "non-coupang URL",
			input:   "https://www.google.com/search?q=test",
			wantErr: true,
		},
		{
			name:    "empty URL",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			input:   "not-a-url",
			wantErr: true,
		},
		{
			name:        "URL without scheme",
			input:       "www.coupang.com/vp/products/12345",
			wantProduct: "12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			got, err := ParseCoupangURL(ctx, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseCoupangURL(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCoupangURL(%q) unexpected error: %v", tt.input, err)
			}
			if got.ProductID != tt.wantProduct {
				t.Errorf("ProductID = %q, want %q", got.ProductID, tt.wantProduct)
			}
			if got.ItemID != tt.wantItem {
				t.Errorf("ItemID = %q, want %q", got.ItemID, tt.wantItem)
			}
			if got.VendorItemID != tt.wantVendor {
				t.Errorf("VendorItemID = %q, want %q", got.VendorItemID, tt.wantVendor)
			}
		})
	}
}

func TestIsCoupangURL(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"https://link.coupang.com/a/d5E9Rd", true},
		{"https://www.coupang.com/vp/products/9334776688", true},
		{"https://m.coupang.com/vm/products/9334776688", true},
		{"이거 봐봐 https://link.coupang.com/a/abc123 좋아보이지?", true},
		{"https://www.google.com", false},
		{"비트코인 가격 알려줘", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := IsCoupangURL(tt.text)
			if got != tt.want {
				t.Errorf("IsCoupangURL(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractCoupangURL(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{
			"이거 봐봐 https://www.coupang.com/vp/products/9334776688?itemId=123 좋아보이지?",
			"https://www.coupang.com/vp/products/9334776688?itemId=123",
		},
		{
			"https://link.coupang.com/a/d5E9Rd",
			"https://link.coupang.com/a/d5E9Rd",
		},
		{
			"비트코인 가격",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := ExtractCoupangURL(tt.text)
			if got != tt.want {
				t.Errorf("ExtractCoupangURL(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
