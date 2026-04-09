package bot

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/shift-click/masterbot/internal/commandmeta"
	"github.com/shift-click/masterbot/internal/intent"
)

type Registry struct {
	catalog *intent.Catalog

	mu        sync.RWMutex
	handlers  map[string]Handler
	order     []string
	fallbacks []fallbackEntry
}

func NewRegistry(catalog *intent.Catalog) *Registry {
	if catalog == nil {
		catalog = intent.DefaultCatalog()
	}
	return &Registry{
		catalog:  catalog,
		handlers: make(map[string]Handler),
		order:    make([]string, 0),
	}
}

func (r *Registry) Catalog() *intent.Catalog {
	return r.catalog
}

func (r *Registry) Register(handler Handler) error {
	if handler == nil {
		return errors.New("handler is nil")
	}

	intentID, err := r.resolveHandlerIntent(handler)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[intentID]; exists {
		return fmt.Errorf("handler %q already registered", intentID)
	}
	r.handlers[intentID] = handler
	r.order = append(r.order, intentID)
	return nil
}

func (r *Registry) AddFallback(handler FallbackHandler) error {
	return r.AddAutoFallback(handler)
}

func (r *Registry) AddAutoFallback(handler FallbackHandler) error {
	return r.addFallback(handler, fallbackScopeAuto)
}

func (r *Registry) AddDeterministicFallback(handler FallbackHandler) error {
	return r.addFallback(handler, fallbackScopeDeterministic)
}

func (r *Registry) addFallback(handler FallbackHandler, scope fallbackScope) error {
	if handler == nil {
		return errors.New("fallback handler is nil")
	}

	id := fallbackHandlerID(r.catalog, handler)
	if id == "" {
		return fmt.Errorf("fallback handler %T is not present in intent catalog", handler)
	}

	var aclExempt bool
	if entry, ok := r.catalog.Entry(id); ok {
		aclExempt = entry.ACLExempt
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallbacks = append(r.fallbacks, fallbackEntry{
		id:        id,
		handler:   handler,
		scope:     scope,
		aclExempt: aclExempt,
	})
	return nil
}

func (r *Registry) Handler(intentID string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.handlers[intentID]
	return handler, ok
}

func (r *Registry) ResolveSlash(command string) (intent.Entry, Handler, bool) {
	entry, ok := r.catalog.ResolveSlash(command)
	if !ok {
		return intent.Entry{}, nil, false
	}
	handler, ok := r.Handler(entry.ID)
	if !ok {
		return intent.Entry{}, nil, false
	}
	return entry, handler, true
}

func (r *Registry) ResolveExplicit(command string) (intent.Entry, Handler, bool) {
	entry, ok := r.catalog.ResolveExplicit(command)
	if !ok {
		return intent.Entry{}, nil, false
	}
	handler, ok := r.Handler(entry.ID)
	if !ok {
		return intent.Entry{}, nil, false
	}
	return entry, handler, true
}

func (r *Registry) Fallbacks() []fallbackEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]fallbackEntry(nil), r.fallbacks...)
}

func (r *Registry) IntentIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.handlers))
	for id := range r.handlers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Registry) OrderedIntentIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

func (r *Registry) VisibleEntries(chatID string, access *AccessController) []intent.Entry {
	entries := make([]intent.Entry, 0)
	for _, entry := range r.catalog.Entries() {
		if !entry.HelpVisible {
			continue
		}
		if _, ok := r.Handler(entry.ID); !ok {
			continue
		}
		if access != nil && !access.IsAllowed(chatID, entry.ID) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func (r *Registry) Validate() error {
	handlerIDs := r.IntentIDs()
	if len(handlerIDs) == 0 {
		return errors.New("no handlers registered")
	}

	missing := r.missingCatalogHandlers(r.fallbackIntentIDs())
	if len(missing) > 0 {
		return fmt.Errorf("missing handlers for catalog intents: %v", missing)
	}
	return r.validateResolvedHandlers(handlerIDs)
}

func (r *Registry) fallbackIntentIDs() map[string]struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	fallbackIDs := make(map[string]struct{}, len(r.fallbacks))
	for _, fb := range r.fallbacks {
		fallbackIDs[fb.id] = struct{}{}
	}
	return fallbackIDs
}

func (r *Registry) missingCatalogHandlers(fallbackIDs map[string]struct{}) []string {
	missing := make([]string, 0)
	for _, entry := range r.catalog.Entries() {
		if _, ok := r.Handler(entry.ID); ok {
			continue
		}
		if _, ok := fallbackIDs[entry.ID]; ok {
			continue
		}
		missing = append(missing, entry.ID)
	}
	return missing
}

func (r *Registry) validateResolvedHandlers(handlerIDs []string) error {
	for _, intentID := range handlerIDs {
		handler, ok := r.Handler(intentID)
		if !ok {
			continue
		}
		if _, err := r.resolveHandlerIntent(handler); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) resolveHandlerIntent(handler Handler) (string, error) {
	if described, ok := handler.(DescribedHandler); ok {
		descriptor := normalizeDescriptor(described.Descriptor())
		if descriptor.ID == "" {
			return "", fmt.Errorf("handler %T descriptor intent id is empty", handler)
		}
		entry, ok := r.catalog.Entry(descriptor.ID)
		if !ok {
			return "", fmt.Errorf("handler %T descriptor %q is not present in intent catalog", handler, descriptor.ID)
		}
		if err := validateDescriptorAgainstCatalog(handler, descriptor, entry); err != nil {
			return "", err
		}
		if err := validateHandlerMethods(handler, descriptor); err != nil {
			return "", err
		}
		return entry.ID, nil
	}

	intentID, ok := r.catalog.Normalize(handler.Name())
	if !ok {
		return "", fmt.Errorf("handler %q is not present in intent catalog", handler.Name())
	}
	return intentID, nil
}

func validateDescriptorAgainstCatalog(handler Handler, descriptor commandmeta.Descriptor, entry intent.Entry) error {
	switch {
	case descriptor.Name != entry.Name:
		return fmt.Errorf("handler %T metadata drift: catalog name %q != descriptor name %q", handler, entry.Name, descriptor.Name)
	case descriptor.Description != entry.Description:
		return fmt.Errorf("handler %T metadata drift: catalog description %q != descriptor description %q", handler, entry.Description, descriptor.Description)
	case !reflect.DeepEqual(normalizeValues(descriptor.SlashAliases), normalizeValues(entry.SlashAliases)):
		return fmt.Errorf("handler %T metadata drift: slash aliases do not match catalog for %q", handler, descriptor.ID)
	case !reflect.DeepEqual(normalizeValues(descriptor.ExplicitAliases), normalizeValues(entry.ExplicitAliases)):
		return fmt.Errorf("handler %T metadata drift: explicit aliases do not match catalog for %q", handler, descriptor.ID)
	case !reflect.DeepEqual(normalizeValues(descriptor.NormalizeKeys), normalizeValues(entry.NormalizeKeys)):
		return fmt.Errorf("handler %T metadata drift: normalize keys do not match catalog for %q", handler, descriptor.ID)
	case descriptor.FallbackScope != entry.FallbackScope:
		return fmt.Errorf("handler %T metadata drift: fallback scope %q != catalog %q for %q", handler, descriptor.FallbackScope, entry.FallbackScope, descriptor.ID)
	case descriptor.AllowAutoQuery != entry.AllowAutoQuery:
		return fmt.Errorf("handler %T metadata drift: allow_auto_query does not match catalog for %q", handler, descriptor.ID)
	}
	return nil
}

func validateHandlerMethods(handler Handler, descriptor commandmeta.Descriptor) error {
	switch {
	case strings.TrimSpace(handler.Name()) != descriptor.Name:
		return fmt.Errorf("handler %T metadata drift: Name()=%q, descriptor=%q", handler, handler.Name(), descriptor.Name)
	case !reflect.DeepEqual(normalizeValues(handler.Aliases()), normalizeValues(descriptor.SlashAliases)):
		return fmt.Errorf("handler %T metadata drift: Aliases() do not match descriptor for %q", handler, descriptor.ID)
	case strings.TrimSpace(handler.Description()) != descriptor.Description:
		return fmt.Errorf("handler %T metadata drift: Description()=%q, descriptor=%q", handler, handler.Description(), descriptor.Description)
	}
	return nil
}

func normalizeDescriptor(descriptor commandmeta.Descriptor) commandmeta.Descriptor {
	descriptor.ID = strings.TrimSpace(strings.ToLower(descriptor.ID))
	descriptor.Name = strings.TrimSpace(descriptor.Name)
	descriptor.Description = strings.TrimSpace(descriptor.Description)
	descriptor.SlashAliases = normalizeValues(descriptor.SlashAliases)
	descriptor.ExplicitAliases = normalizeValues(descriptor.ExplicitAliases)
	descriptor.NormalizeKeys = normalizeValues(descriptor.NormalizeKeys)
	return descriptor
}

func normalizeValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return strings.ToLower(normalized[i]) < strings.ToLower(normalized[j])
	})
	return normalized
}
