package coupang

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shift-click/masterbot/internal/scraper/providers"
)

func (t *CoupangTracker) fetchRefresh(ctx context.Context, cu *providers.CoupangURL, product *CoupangProductRecord, allowFallback bool) (*resolvedProduct, error) {
	if product != nil &&
		product.SourceMapping.FallcentProductID != "" &&
		product.SourceMapping.State == CoupangSourceMappingVerified &&
		t.fallcent != nil {
		detail, err := t.fallcent.FetchProduct(ctx, product.SourceMapping.FallcentProductID)
		if err == nil && detail != nil && providers.FallcentMatchesCoupang(cu, detail) {
			return &resolvedProduct{
				Name:              detail.Name,
				ImageURL:          detail.ImageURL,
				Price:             detail.Price,
				ItemID:            firstNonEmpty(product.ItemID, detail.ItemID),
				VendorItemID:      firstNonEmpty(product.VendorItemID, detail.VendorItemID),
				ComparativeMin:    detail.LowestPrice,
				RefreshSource:     "fallcent",
				FallcentProductID: detail.FallcentProductID,
				SearchKeyword:     product.SourceMapping.SearchKeyword,
				MappingState:      CoupangSourceMappingVerified,
			}, nil
		}
		if err == nil && detail == nil {
			err = fmt.Errorf("fallcent detail was empty")
		}
		t.logger.Warn("fallcent detail verification failed", "track_id", product.TrackID, "fallcent_product_id", product.SourceMapping.FallcentProductID, "error", err)
		_ = t.store.MarkSourceMappingState(ctx, product.TrackID, CoupangSourceMappingNeedsRecheck, "detail verification failed")
	}

	return t.resolveForTracking(ctx, cu, product, "refresh", allowFallback)
}

func (t *CoupangTracker) resolveForTracking(ctx context.Context, cu *providers.CoupangURL, product *CoupangProductRecord, reason string, allowFallback bool, attachmentTitle ...string) (*resolvedProduct, error) {
	if resolved := t.resolveWithFallcentDirect(ctx, cu, reason); resolved != nil {
		return resolved, nil
	}

	keywords := t.collectSearchKeywords(product)
	keywords = t.prependAttachmentKeyword(keywords, cu, attachmentTitle)
	keywords, auxProduct, auxErr := t.enrichResolutionKeywords(ctx, cu, keywords, allowFallback)
	if resolved := t.resolveWithFallcent(ctx, cu, keywords, reason); resolved != nil {
		return resolved, nil
	}
	return t.resolveWithAuxiliary(ctx, cu, reason, allowFallback, keywords, auxProduct, auxErr)
}

func (t *CoupangTracker) prependAttachmentKeyword(keywords []string, cu *providers.CoupangURL, attachmentTitle []string) []string {
	if len(attachmentTitle) == 0 {
		return keywords
	}
	title := strings.TrimSpace(attachmentTitle[0])
	if title == "" {
		return keywords
	}
	t.logger.Info("using attachment title as keyword", "title", title, "product_id", cu.ProductID)
	return append([]string{title}, keywords...)
}

func (t *CoupangTracker) enrichResolutionKeywords(ctx context.Context, cu *providers.CoupangURL, keywords []string, allowFallback bool) ([]string, *providers.CoupangProduct, error) {
	var auxProduct *providers.CoupangProduct
	var auxErr error
	if len(keywords) == 0 && allowFallback && t.scraper != nil {
		auxProduct, auxErr = t.scraper.FetchCurrent(ctx, cu)
		if auxErr != nil {
			t.logger.Warn("coupang aux scraper failed", "product_id", cu.ProductID, "error", auxErr)
		} else if auxProduct != nil {
			keywords = append(keywords, auxProduct.Name)
		}
	}
	if len(keywords) == 0 && allowFallback && t.naverTitle != nil {
		naverTitle, naverErr := t.naverTitle.ResolveTitle(ctx, cu.ProductID)
		if naverErr == nil && naverTitle != "" {
			t.logger.Info("using naver title as keyword", "title", naverTitle, "product_id", cu.ProductID)
			keywords = append(keywords, naverTitle)
		} else if naverErr != nil {
			t.logger.Debug("naver title resolution failed", "product_id", cu.ProductID, "error", naverErr)
		}
	}
	return keywords, auxProduct, auxErr
}

func (t *CoupangTracker) resolveWithFallcentDirect(ctx context.Context, cu *providers.CoupangURL, reason string) *resolvedProduct {
	if t.fallcent == nil || cu.ItemID == "" {
		return nil
	}
	if !t.resolutions.Allow(time.Now()) {
		t.logger.Debug("fallcent direct lookup skipped: budget exhausted", "reason", reason, "product_id", cu.ProductID)
		return nil
	}
	detail, err := t.fallcent.LookupByCoupangID(ctx, cu.ProductID, cu.ItemID)
	if err != nil {
		t.logger.Debug("fallcent direct lookup failed", "product_id", cu.ProductID, "item_id", cu.ItemID, "error", err)
		return nil
	}
	if detail == nil || !providers.FallcentMatchesCoupang(cu, detail) {
		return nil
	}
	t.logger.Info("resolved via fallcent direct lookup", "product_id", cu.ProductID, "item_id", cu.ItemID, "fallcent_id", detail.FallcentProductID, "name", detail.Name)
	return &resolvedProduct{
		Name:              detail.Name,
		ImageURL:          detail.ImageURL,
		Price:             detail.Price,
		ItemID:            firstNonEmpty(cu.ItemID, detail.ItemID),
		VendorItemID:      firstNonEmpty(cu.VendorItemID, detail.VendorItemID),
		ComparativeMin:    detail.LowestPrice,
		RefreshSource:     "fallcent_direct_" + reason,
		FallcentProductID: detail.FallcentProductID,
		SearchKeyword:     detail.Name,
		MappingState:      CoupangSourceMappingVerified,
	}
}

func (t *CoupangTracker) resolveWithFallcent(ctx context.Context, cu *providers.CoupangURL, keywords []string, reason string) *resolvedProduct {
	if t.fallcent == nil {
		return nil
	}
	if !t.resolutions.Allow(time.Now()) {
		t.logger.Debug("fallcent resolution skipped: budget exhausted", "reason", reason, "product_id", cu.ProductID)
		return nil
	}
	detail, err := t.fallcent.ResolveProduct(ctx, cu, keywords)
	if err == nil && detail != nil {
		return &resolvedProduct{
			Name:              detail.Name,
			ImageURL:          detail.ImageURL,
			Price:             detail.Price,
			ItemID:            firstNonEmpty(cu.ItemID, detail.ItemID),
			VendorItemID:      firstNonEmpty(cu.VendorItemID, detail.VendorItemID),
			ComparativeMin:    detail.LowestPrice,
			RefreshSource:     "fallcent_" + reason,
			FallcentProductID: detail.FallcentProductID,
			SearchKeyword:     detail.SearchKeyword,
			MappingState:      CoupangSourceMappingVerified,
		}
	}
	if err == nil && detail == nil {
		err = fmt.Errorf("fallcent resolution was empty")
	}
	t.logger.Warn("fallcent resolution failed", "reason", reason, "product_id", cu.ProductID, "error", err)
	return nil
}

func (t *CoupangTracker) resolveWithAuxiliary(ctx context.Context, cu *providers.CoupangURL, reason string, allowFallback bool, keywords []string, auxProduct *providers.CoupangProduct, auxErr error) (*resolvedProduct, error) {
	if !allowFallback || !t.config.AllowAuxiliaryFallback || t.scraper == nil {
		return nil, fallbackLimitError(reason)
	}
	if auxProduct == nil && auxErr == nil {
		auxProduct, auxErr = t.scraper.FetchCurrent(ctx, cu)
	}
	if auxErr != nil {
		return nil, auxErr
	}
	if auxProduct == nil {
		return nil, fmt.Errorf("no auxiliary product available")
	}
	return &resolvedProduct{
		Name:              auxProduct.Name,
		ImageURL:          auxProduct.ImageURL,
		Price:             auxProduct.Price,
		ItemID:            firstNonEmpty(cu.ItemID, auxProduct.ItemID),
		VendorItemID:      firstNonEmpty(cu.VendorItemID, auxProduct.VendorItemID),
		ComparativeMin:    0,
		RefreshSource:     "coupang_aux_" + reason,
		FallcentProductID: "",
		SearchKeyword:     firstResolutionKeyword(keywords, auxProduct.Name),
		MappingState:      CoupangSourceMappingFailed,
	}, nil
}

func fallbackLimitError(reason string) error {
	if reason == "registration" {
		return ErrCoupangRegistrationLimited
	}
	return ErrCoupangRefreshLimited
}

func isCoupangAccessBlocked(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "http 403") {
		return false
	}
	return strings.Contains(msg, "coupang.com")
}

func firstResolutionKeyword(keywords []string, fallback string) string {
	if len(keywords) > 0 {
		return keywords[0]
	}
	return fallback
}

func (t *CoupangTracker) collectSearchKeywords(product *CoupangProductRecord) []string {
	if product == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var keywords []string
	for _, value := range []string{
		product.SourceMapping.SearchKeyword,
		product.Name,
	} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		keywords = append(keywords, value)
	}
	return keywords
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
