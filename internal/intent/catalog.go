package intent

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/shift-click/masterbot/internal/commandmeta"
)

type Entry struct {
	ID              string
	Name            string
	Description     string
	SlashAliases    []string
	ExplicitAliases []string
	NormalizeKeys   []string
	FallbackScope   commandmeta.FallbackScope
	AllowAutoQuery  bool
	ACLExempt       bool
	Example         string
	Category        string
	HelpVisible     bool
}

type Catalog struct {
	entries     map[string]Entry
	slash       map[string]string
	explicit    map[string]string
	normalized  map[string]string
	displayName map[string]string
}

func NewCatalog(entries []Entry) (*Catalog, error) {
	c := &Catalog{
		entries:     make(map[string]Entry, len(entries)),
		slash:       make(map[string]string),
		explicit:    make(map[string]string),
		normalized:  make(map[string]string),
		displayName: make(map[string]string),
	}

	for _, raw := range entries {
		if err := c.registerEntry(normalizeEntry(raw)); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (c *Catalog) registerEntry(entry Entry) error {
	if entry.ID == "" {
		return fmt.Errorf("intent id is required")
	}
	if entry.Name == "" {
		return fmt.Errorf("intent %q name is required", entry.ID)
	}
	if _, exists := c.entries[entry.ID]; exists {
		return fmt.Errorf("intent %q already registered", entry.ID)
	}
	c.entries[entry.ID] = entry
	c.displayName[entry.ID] = entry.Name

	for _, alias := range append([]string{entry.Name, entry.ID}, entry.NormalizeKeys...) {
		if err := c.registerAlias(c.normalized, alias, entry.ID, "normalized"); err != nil {
			return err
		}
	}
	for _, alias := range append([]string{entry.Name}, entry.SlashAliases...) {
		if err := c.registerAlias(c.slash, alias, entry.ID, "slash"); err != nil {
			return err
		}
	}
	for _, alias := range entry.ExplicitAliases {
		if err := c.registerAlias(c.explicit, alias, entry.ID, "explicit"); err != nil {
			return err
		}
	}
	return nil
}

func (c *Catalog) registerAlias(dst map[string]string, raw, id, scope string) error {
	key := normalize(raw)
	if key == "" {
		return nil
	}
	if prev, exists := dst[key]; exists && prev != id {
		return fmt.Errorf("%s alias %q already registered by %q", scope, raw, prev)
	}
	dst[key] = id
	return nil
}

func (c *Catalog) Entry(id string) (Entry, bool) {
	id = normalize(id)
	entry, ok := c.entries[id]
	return entry, ok
}

func (c *Catalog) Entries() []Entry {
	entries := make([]Entry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func (c *Catalog) Normalize(value string) (string, bool) {
	id, ok := c.normalized[normalize(value)]
	return id, ok
}

func (c *Catalog) ResolveSlash(value string) (Entry, bool) {
	id, ok := c.slash[normalize(value)]
	if !ok {
		return Entry{}, false
	}
	return c.entries[id], true
}

func (c *Catalog) Resolve(value string) (Entry, bool) {
	if entry, ok := c.ResolveSlash(value); ok {
		return entry, true
	}
	if id, ok := c.normalized[normalize(value)]; ok {
		return c.entries[id], true
	}
	return Entry{}, false
}

func (c *Catalog) ResolveExplicit(value string) (Entry, bool) {
	id, ok := c.explicit[normalize(value)]
	if !ok {
		return Entry{}, false
	}
	return c.entries[id], true
}

func normalizeEntry(entry Entry) Entry {
	entry.ID = normalize(entry.ID)
	entry.Name = strings.TrimSpace(entry.Name)
	entry.Description = strings.TrimSpace(entry.Description)
	entry.SlashAliases = dedupe(entry.SlashAliases)
	entry.ExplicitAliases = dedupe(entry.ExplicitAliases)
	entry.NormalizeKeys = dedupe(entry.NormalizeKeys)
	entry.Example = strings.TrimSpace(entry.Example)
	return entry
}

func dedupe(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := normalize(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func normalize(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

var (
	defaultCatalogOnce sync.Once
	defaultCatalog     *Catalog
	defaultCatalogErr  error
)

func DefaultCatalog() *Catalog {
	defaultCatalogOnce.Do(func() {
		defaultCatalog, defaultCatalogErr = NewCatalog(defaultEntries())
	})
	if defaultCatalogErr != nil {
		panic(defaultCatalogErr)
	}
	return defaultCatalog
}

func defaultEntries() []Entry {
	descriptors := commandmeta.Descriptors()
	entries := make([]Entry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(entries, Entry{
			ID:              descriptor.ID,
			Name:            descriptor.Name,
			Description:     descriptor.Description,
			SlashAliases:    descriptor.SlashAliases,
			ExplicitAliases: descriptor.ExplicitAliases,
			NormalizeKeys:   descriptor.NormalizeKeys,
			FallbackScope:   descriptor.FallbackScope,
			AllowAutoQuery:  descriptor.AllowAutoQuery,
			ACLExempt:       descriptor.ACLExempt,
			Example:         descriptor.Example,
			Category:        descriptor.Category,
			HelpVisible:     descriptor.HelpVisible,
		})
	}
	return entries
}
