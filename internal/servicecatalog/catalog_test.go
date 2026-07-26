package servicecatalog

import (
	"strings"
	"testing"
)

const fixture = "# comment\n" +
	"!version\t2026-01-02\n" +
	"!source\tfixture\n" +
	"S\tyoutube\tYouTube\tvideo\n" +
	"D\tyoutube.com\n" +
	"F\tyt3.googleusercontent.com\n" +
	"S\tgoogle\tGoogle\tsearch\n" +
	"D\tgoogle.com\n" +
	"D\tgoogleusercontent.com\n" +
	"S\tgoogle-scholar\tGoogle Scholar\teducation\n" +
	"D\tscholar.google.co.uk\n"

func fixtureCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := parse(fixture)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return c
}

func assertClassification(t *testing.T, got Classification, service, category, source string) {
	t.Helper()
	if got.Service != service || got.Category != category || got.Source != source {
		t.Fatalf("got service=%q category=%q source=%q, want service=%q category=%q source=%q",
			got.Service, got.Category, got.Source, service, category, source)
	}
}

func TestClassifySuffixAndSubdomain(t *testing.T) {
	c := fixtureCatalog(t)

	for _, host := range []string{"youtube.com", "www.youtube.com", "a.b.youtube.com"} {
		got := c.Classify(host)
		assertClassification(t, got, "youtube", "video", SourceCatalog)
		if got.Label != "YouTube" {
			t.Fatalf("host %q: got label %q, want %q", host, got.Label, "YouTube")
		}
	}

	// Label boundaries must be respected: this is not a subdomain of youtube.com.
	assertClassification(t, c.Classify("notyoutube.com"), "notyoutube.com", CategoryUnclassified, SourcePublicSuffix)
}

func TestClassifyExactBeatsSuffix(t *testing.T) {
	c := fixtureCatalog(t)

	assertClassification(t, c.Classify("yt3.googleusercontent.com"), "youtube", "video", SourceCatalog)
	assertClassification(t, c.Classify("lh3.googleusercontent.com"), "google", "search", SourceCatalog)
	// An exact rule matches only that host, never a subdomain of it.
	assertClassification(t, c.Classify("cdn.yt3.googleusercontent.com"), "google", "search", SourceCatalog)
}

func TestClassifyLongestSuffixWins(t *testing.T) {
	c := fixtureCatalog(t)

	assertClassification(t, c.Classify("scholar.google.co.uk"), "google-scholar", "education", SourceCatalog)
	assertClassification(t, c.Classify("citations.scholar.google.co.uk"), "google-scholar", "education", SourceCatalog)
	// google.co.uk is not in the fixture, so the shorter rule must not apply.
	assertClassification(t, c.Classify("maps.google.co.uk"), "google.co.uk", CategoryUnclassified, SourcePublicSuffix)
}

func TestClassifyUppercaseAndTrailingDot(t *testing.T) {
	c := fixtureCatalog(t)

	// log_events.target_host is stored with its original casing.
	assertClassification(t, c.Classify("WWW.YouTube.COM"), "youtube", "video", SourceCatalog)
	assertClassification(t, c.Classify("  www.youtube.com.  "), "youtube", "video", SourceCatalog)
	assertClassification(t, c.Classify("YT3.GoogleUserContent.com"), "youtube", "video", SourceCatalog)
}

func TestClassifyEmptyHost(t *testing.T) {
	c := fixtureCatalog(t)

	for _, host := range []string{"", "   ", "."} {
		got := c.Classify(host)
		assertClassification(t, got, ServiceUnknown, CategoryUnknown, SourceUnknown)
		if got.Label == "" {
			t.Fatalf("host %q: empty label", host)
		}
	}
}

func TestClassifyIPLiterals(t *testing.T) {
	c := fixtureCatalog(t)

	tests := []struct {
		host string
		want string
	}{
		{"1.2.3.4", "1.2.3.4"},
		{"192.168.0.1", "192.168.0.1"},
		{"2001:db8::1", "2001:db8::1"},
		{"[2001:db8::1]", "2001:db8::1"},
		{"2001:0DB8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
		{"::1", "::1"},
		{"::ffff:1.2.3.4", "1.2.3.4"},
	}
	for _, tc := range tests {
		got := c.Classify(tc.host)
		assertClassification(t, got, tc.want, CategoryDirectIP, SourceIP)
		if got.Label != tc.want {
			t.Fatalf("host %q: got label %q, want %q", tc.host, got.Label, tc.want)
		}
	}
}

func TestClassifyPublicSuffixFallback(t *testing.T) {
	c := fixtureCatalog(t)

	assertClassification(t, c.Classify("api.example.org"), "example.org", CategoryUnclassified, SourcePublicSuffix)
	// Multi-part public suffix: eTLD+1 must keep both trailing labels.
	assertClassification(t, c.Classify("foo.co.uk"), "foo.co.uk", CategoryUnclassified, SourcePublicSuffix)
	assertClassification(t, c.Classify("mail.foo.co.uk"), "foo.co.uk", CategoryUnclassified, SourcePublicSuffix)
	// A bare public suffix has no eTLD+1; the raw host is the last resort.
	assertClassification(t, c.Classify("co.uk"), "co.uk", CategoryUnclassified, SourceUnknown)
}

func TestWithOverridesTakePrecedence(t *testing.T) {
	base := fixtureCatalog(t)
	derived := base.WithOverrides([]Override{
		{Suffix: "YouTube.com", Service: "internal-video", Label: "Internal video", Category: "internal"},
		{Suffix: ".example.org.", Service: "corp"},
	})

	assertClassification(t, derived.Classify("www.youtube.com"), "internal-video", "internal", SourceOverride)
	// An override with no label or category still yields displayable values.
	got := derived.Classify("api.example.org")
	assertClassification(t, got, "corp", CategoryCustom, SourceOverride)
	if got.Label != "corp" {
		t.Fatalf("got label %q, want %q", got.Label, "corp")
	}
	// Hosts the overrides do not cover still resolve against the dataset.
	assertClassification(t, derived.Classify("lh3.googleusercontent.com"), "google", "search", SourceCatalog)

	// The receiver must be unchanged.
	assertClassification(t, base.Classify("www.youtube.com"), "youtube", "video", SourceCatalog)
}

func TestWithOverridesIgnoresIncompleteRules(t *testing.T) {
	base := fixtureCatalog(t)

	if got := base.WithOverrides(nil); got != base {
		t.Fatalf("WithOverrides(nil) returned a new catalog")
	}
	derived := base.WithOverrides([]Override{
		{Suffix: "", Service: "nowhere"},
		{Suffix: "youtube.com", Service: "   "},
	})
	if derived != base {
		t.Fatalf("WithOverrides with only invalid rules returned a new catalog")
	}
}

func TestWithOverridesLongestSuffixWins(t *testing.T) {
	base := fixtureCatalog(t)
	derived := base.WithOverrides([]Override{
		{Suffix: "example.org", Service: "corp", Category: "internal"},
		{Suffix: "vpn.example.org", Service: "corp-vpn", Category: "internal"},
	})

	assertClassification(t, derived.Classify("api.example.org"), "corp", "internal", SourceOverride)
	assertClassification(t, derived.Classify("gw.vpn.example.org"), "corp-vpn", "internal", SourceOverride)
	// An override never intercepts an IP literal.
	assertClassification(t, derived.Classify("10.0.0.1"), "10.0.0.1", CategoryDirectIP, SourceIP)
}

func TestParseRejectsMalformedDatasets(t *testing.T) {
	tests := map[string]string{
		"missing version":     "S\tx\tX\tc\nD\tx.com\n",
		"no rules":            "!version\t1\n",
		"rule before service": "!version\t1\nD\tx.com\n",
		"unknown kind":        "!version\t1\nS\tx\tX\tc\nZ\tx.com\n",
		"short service":       "!version\t1\nS\tx\tX\n",
		"missing tab":         "!version\t1\nS\tx\tX\tc\nD\n",
	}
	for name, text := range tests {
		if _, err := parse(text); err == nil {
			t.Fatalf("%s: expected a parse error", name)
		}
	}
}

func TestEmbeddedCatalog(t *testing.T) {
	c := Default()

	if Version() == "" {
		t.Fatal("embedded dataset has no version stamp")
	}
	if !strings.Contains(Source(), "domain-list-community") {
		t.Fatalf("unexpected dataset source %q", Source())
	}
	if len(c.suffixes) < 1000 {
		t.Fatalf("embedded dataset has only %d suffix rules", len(c.suffixes))
	}

	tests := []struct {
		host    string
		service string
	}{
		{"www.youtube.com", "youtube"},
		{"i.ytimg.com", "youtube"},
		{"www.google.com", "google"},
		{"github.com", "github"},
		{"api.telegram.org", "telegram"},
		{"nflxvideo.net", "netflix"},
	}
	for _, tc := range tests {
		if got := c.Classify(tc.host); got.Service != tc.service {
			t.Errorf("Classify(%q) = %q (source %q), want %q", tc.host, got.Service, got.Source, tc.service)
		}
	}

	// Every catalog rule must resolve back to a non-empty label and category so
	// the audit view never renders a blank group.
	for domain := range c.suffixes {
		got := c.Classify(domain)
		if got.Label == "" || got.Category == "" {
			t.Fatalf("rule %q classifies to an empty label or category: %+v", domain, got)
		}
	}
}
