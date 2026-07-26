// Package servicecatalog maps a network-event target host onto a
// product-level service.
//
// Classification is a pure function over an embedded snapshot of
// v2fly/domain-list-community plus operator-supplied overrides — the same
// geosite ecosystem sing-box and Mihomo consume. The package never touches the
// database and never reaches the network: callers load overrides themselves
// and hand them in via WithOverrides.
//
// Classification happens at read time, not at ingest time, so a dataset bump
// retroactively improves historical views with no backfill.
package servicecatalog

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.org/x/net/publicsuffix"

	"github.com/haoxin/boxfleet/internal/servicecatalog/data"
)

// Source records which rule produced a Classification.
const (
	SourceOverride     = "override"
	SourceCatalog      = "catalog"
	SourcePublicSuffix = "publicsuffix"
	SourceIP           = "ip"
	SourceUnknown      = "unknown"
)

// Category values assigned by the fallback chain rather than by the dataset.
const (
	CategoryDirectIP     = "direct-ip"
	CategoryUnclassified = "unclassified"
	CategoryUnknown      = "unknown"
	CategoryCustom       = "custom"
)

// ServiceUnknown is the service key used when there is no host to classify.
const ServiceUnknown = "unknown"

// Classification is the result of resolving one host.
type Classification struct {
	Service  string
	Label    string
	Category string
	Source   string
}

// Override is an operator-supplied rule that takes precedence over the
// embedded dataset. Suffix matches the host itself and any subdomain of it.
type Override struct {
	Suffix   string
	Service  string
	Label    string
	Category string
}

type entry struct {
	service  string
	label    string
	category string
}

// Catalog resolves hosts to services. A Catalog is immutable and safe for
// concurrent use; WithOverrides derives a new one rather than mutating.
type Catalog struct {
	version   string
	source    string
	suffixes  map[string]entry
	exacts    map[string]entry
	overrides map[string]entry
}

var (
	defaultOnce    sync.Once
	defaultCatalog *Catalog
)

// Default returns the embedded catalog. The dataset is generated and
// committed, so a parse failure is a build-time defect rather than a runtime
// condition and is reported as a panic.
func Default() *Catalog {
	defaultOnce.Do(func() {
		parsed, err := parse(data.ServicesTSV)
		if err != nil {
			panic("servicecatalog: embedded dataset is invalid: " + err.Error())
		}
		defaultCatalog = parsed
	})
	return defaultCatalog
}

// Version is the embedded dataset stamp, e.g. "2026-07-26". Overrides do not
// change it.
func Version() string { return Default().version }

// Source describes where the embedded dataset came from.
func Source() string { return Default().source }

// WithOverrides returns a catalog that consults overrides ahead of the
// embedded dataset. The receiver is left untouched.
//
// Overrides with an empty suffix or an empty service are ignored. An empty
// label defaults to the service key and an empty category to CategoryCustom,
// so every classification carries a displayable label and a groupable
// category.
func (c *Catalog) WithOverrides(overrides []Override) *Catalog {
	if len(overrides) == 0 {
		return c
	}
	normalized := make(map[string]entry, len(overrides))
	for _, o := range overrides {
		suffix := normalizeHost(o.Suffix)
		service := strings.TrimSpace(o.Service)
		if suffix == "" || service == "" {
			continue
		}
		label := strings.TrimSpace(o.Label)
		if label == "" {
			label = service
		}
		category := strings.TrimSpace(o.Category)
		if category == "" {
			category = CategoryCustom
		}
		normalized[suffix] = entry{service: service, label: label, category: category}
	}
	if len(normalized) == 0 {
		return c
	}
	derived := *c
	derived.overrides = normalized
	return &derived
}

// Classify resolves host, which may carry any casing and an optional trailing
// dot or IPv6 brackets. The fallback chain is: empty, IP literal, override,
// embedded catalog, eTLD+1, raw host.
func (c *Catalog) Classify(host string) Classification {
	name := normalizeHost(host)
	if name == "" {
		return Classification{
			Service:  ServiceUnknown,
			Label:    "Unknown",
			Category: CategoryUnknown,
			Source:   SourceUnknown,
		}
	}
	if ip := net.ParseIP(name); ip != nil {
		canonical := ip.String()
		return Classification{
			Service:  canonical,
			Label:    canonical,
			Category: CategoryDirectIP,
			Source:   SourceIP,
		}
	}
	if found, ok := lookupSuffix(c.overrides, name); ok {
		return classification(found, SourceOverride)
	}
	if found, ok := c.exacts[name]; ok {
		return classification(found, SourceCatalog)
	}
	if found, ok := lookupSuffix(c.suffixes, name); ok {
		return classification(found, SourceCatalog)
	}
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(name); err == nil && etld1 != "" {
		return Classification{
			Service:  etld1,
			Label:    etld1,
			Category: CategoryUnclassified,
			Source:   SourcePublicSuffix,
		}
	}
	return Classification{
		Service:  name,
		Label:    name,
		Category: CategoryUnclassified,
		Source:   SourceUnknown,
	}
}

func classification(e entry, source string) Classification {
	return Classification{Service: e.service, Label: e.label, Category: e.category, Source: source}
}

// lookupSuffix walks name right-to-left on label boundaries, so the longest
// matching suffix wins and "notyoutube.com" never matches "youtube.com".
func lookupSuffix(rules map[string]entry, name string) (entry, bool) {
	if len(rules) == 0 {
		return entry{}, false
	}
	for candidate := name; candidate != ""; {
		if found, ok := rules[candidate]; ok {
			return found, true
		}
		idx := strings.IndexByte(candidate, '.')
		if idx < 0 {
			break
		}
		candidate = candidate[idx+1:]
	}
	return entry{}, false
}

// normalizeHost lowercases, removes IPv6 brackets, and drops leading and
// trailing dots, so both "Example.COM." and the ".example.com" spelling
// operators tend to type into an override resolve to the same key.
// log_events.target_host is stored with its original casing, so every lookup
// must normalize.
func normalizeHost(host string) string {
	name := strings.TrimSpace(host)
	if len(name) > 1 && name[0] == '[' && name[len(name)-1] == ']' {
		name = name[1 : len(name)-1]
	}
	name = strings.Trim(name, ".")
	return strings.ToLower(name)
}

// parse reads the generated TSV. `S` starts a service block; the `D` (suffix)
// and `F` (exact) rules that follow belong to it.
func parse(text string) (*Catalog, error) {
	c := &Catalog{
		suffixes: make(map[string]entry, 32768),
		exacts:   make(map[string]entry, 1024),
	}
	var current entry
	haveService := false

	for lineNo, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			key, value, _ := strings.Cut(strings.TrimPrefix(line, "!"), "\t")
			switch key {
			case "version":
				c.version = value
			case "source":
				c.source = value
			}
			continue
		}
		kind, rest, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("line %d: missing tab separator", lineNo+1)
		}
		switch kind {
		case "S":
			fields := strings.Split(rest, "\t")
			if len(fields) != 3 || fields[0] == "" {
				return nil, fmt.Errorf("line %d: malformed service record", lineNo+1)
			}
			current = entry{service: fields[0], label: fields[1], category: fields[2]}
			haveService = true
		case "D", "F":
			if !haveService {
				return nil, fmt.Errorf("line %d: rule before any service record", lineNo+1)
			}
			if rest == "" {
				return nil, fmt.Errorf("line %d: empty domain", lineNo+1)
			}
			if kind == "D" {
				c.suffixes[rest] = current
			} else {
				c.exacts[rest] = current
			}
		default:
			return nil, fmt.Errorf("line %d: unknown record kind %q", lineNo+1, kind)
		}
	}
	if c.version == "" {
		return nil, fmt.Errorf("dataset is missing a !version header")
	}
	if len(c.suffixes)+len(c.exacts) == 0 {
		return nil, fmt.Errorf("dataset contains no rules")
	}
	return c, nil
}
