// Command gen regenerates internal/servicecatalog/data/services.tsv from
// v2fly/domain-list-community.
//
// It is run by hand, never at build time and never by the server. See
// README.md in this directory for the regeneration procedure and the pruning
// policy.
package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// serviceSpec binds one upstream v2fly/domain-list-community list to a BoxFleet
// service key, display label, and functional category.
type serviceSpec struct {
	List     string
	Key      string
	Label    string
	Category string
}

// allowlist is the committed pruning policy: only these upstream lists are
// embedded, and each list's `include:` directives are expanded transitively.
//
// Order matters. A domain is claimed by the first spec that yields it, so a
// specific service must precede the umbrella list that includes it (youtube
// before google, icloud before apple, github before microsoft).
var allowlist = []serviceSpec{
	// Google properties, most specific first.
	{List: "youtube", Key: "youtube", Label: "YouTube", Category: "video"},
	{List: "google-play", Key: "google-play", Label: "Google Play", Category: "platform"},
	{List: "google-scholar", Key: "google-scholar", Label: "Google Scholar", Category: "education"},
	{List: "android", Key: "android", Label: "Android", Category: "platform"},
	{List: "blogspot", Key: "blogspot", Label: "Blogger", Category: "social"},
	{List: "firebase", Key: "firebase", Label: "Firebase", Category: "developer"},
	{List: "google", Key: "google", Label: "Google", Category: "search"},

	// Microsoft properties.
	{List: "github", Key: "github", Label: "GitHub", Category: "developer"},
	{List: "bing", Key: "bing", Label: "Bing", Category: "search"},
	{List: "onedrive", Key: "onedrive", Label: "OneDrive", Category: "storage"},
	{List: "xbox", Key: "xbox", Label: "Xbox", Category: "gaming"},
	{List: "azure", Key: "azure", Label: "Azure", Category: "cloud"},
	{List: "msn", Key: "msn", Label: "MSN", Category: "news"},
	{List: "microsoft", Key: "microsoft", Label: "Microsoft", Category: "productivity"},

	// Apple properties.
	{List: "icloud", Key: "icloud", Label: "iCloud", Category: "storage"},
	{List: "apple-music", Key: "apple-music", Label: "Apple Music", Category: "music"},
	{List: "apple-tvplus", Key: "apple-tv", Label: "Apple TV+", Category: "video"},
	{List: "apple-podcasts", Key: "apple-podcasts", Label: "Apple Podcasts", Category: "music"},
	{List: "itunes", Key: "itunes", Label: "iTunes", Category: "music"},
	{List: "apple", Key: "apple", Label: "Apple", Category: "platform"},

	// Amazon properties.
	{List: "primevideo", Key: "prime-video", Label: "Prime Video", Category: "video"},
	{List: "imdb", Key: "imdb", Label: "IMDb", Category: "reference"},
	{List: "kindle", Key: "kindle", Label: "Kindle", Category: "reference"},
	{List: "aws", Key: "aws", Label: "AWS", Category: "cloud"},
	{List: "amazon", Key: "amazon", Label: "Amazon", Category: "shopping"},

	// Meta properties.
	{List: "instagram", Key: "instagram", Label: "Instagram", Category: "social"},
	{List: "whatsapp", Key: "whatsapp", Label: "WhatsApp", Category: "messaging"},
	{List: "messenger", Key: "messenger", Label: "Messenger", Category: "messaging"},
	{List: "threads", Key: "threads", Label: "Threads", Category: "social"},
	{List: "oculus", Key: "oculus", Label: "Meta Quest", Category: "gaming"},
	{List: "facebook", Key: "facebook", Label: "Facebook", Category: "social"},
	{List: "meta", Key: "meta", Label: "Meta", Category: "social"},

	// Social and community.
	{List: "twitter", Key: "twitter", Label: "X (Twitter)", Category: "social"},
	{List: "tiktok", Key: "tiktok", Label: "TikTok", Category: "social"},
	{List: "reddit", Key: "reddit", Label: "Reddit", Category: "social"},
	{List: "linkedin", Key: "linkedin", Label: "LinkedIn", Category: "social"},
	{List: "pinterest", Key: "pinterest", Label: "Pinterest", Category: "social"},
	{List: "tumblr", Key: "tumblr", Label: "Tumblr", Category: "social"},
	{List: "bluesky", Key: "bluesky", Label: "Bluesky", Category: "social"},
	{List: "quora", Key: "quora", Label: "Quora", Category: "social"},
	{List: "medium", Key: "medium", Label: "Medium", Category: "news"},
	{List: "vk", Key: "vk", Label: "VK", Category: "social"},
	{List: "zhihu", Key: "zhihu", Label: "Zhihu", Category: "social"},
	{List: "douban", Key: "douban", Label: "Douban", Category: "social"},
	{List: "sina", Key: "sina", Label: "Sina Weibo", Category: "social"},
	{List: "imgur", Key: "imgur", Label: "Imgur", Category: "social"},
	{List: "flickr", Key: "flickr", Label: "Flickr", Category: "social"},
	{List: "dribbble", Key: "dribbble", Label: "Dribbble", Category: "social"},
	{List: "deviantart", Key: "deviantart", Label: "DeviantArt", Category: "social"},
	{List: "pixiv", Key: "pixiv", Label: "pixiv", Category: "social"},

	// Messaging and meetings.
	{List: "telegram", Key: "telegram", Label: "Telegram", Category: "messaging"},
	{List: "signal", Key: "signal", Label: "Signal", Category: "messaging"},
	{List: "discord", Key: "discord", Label: "Discord", Category: "messaging"},
	{List: "line", Key: "line", Label: "LINE", Category: "messaging"},
	{List: "viber", Key: "viber", Label: "Viber", Category: "messaging"},
	{List: "kakao", Key: "kakao", Label: "KakaoTalk", Category: "messaging"},
	{List: "slack", Key: "slack", Label: "Slack", Category: "productivity"},
	{List: "zoom", Key: "zoom", Label: "Zoom", Category: "productivity"},
	{List: "webex", Key: "webex", Label: "Webex", Category: "productivity"},
	{List: "teamviewer", Key: "teamviewer", Label: "TeamViewer", Category: "productivity"},

	// Video and music.
	{List: "netflix", Key: "netflix", Label: "Netflix", Category: "video"},
	{List: "disney", Key: "disney", Label: "Disney+", Category: "video"},
	{List: "hulu", Key: "hulu", Label: "Hulu", Category: "video"},
	{List: "hbo", Key: "hbo", Label: "HBO Max", Category: "video"},
	{List: "twitch", Key: "twitch", Label: "Twitch", Category: "video"},
	{List: "vimeo", Key: "vimeo", Label: "Vimeo", Category: "video"},
	{List: "dailymotion", Key: "dailymotion", Label: "Dailymotion", Category: "video"},
	{List: "bilibili", Key: "bilibili", Label: "Bilibili", Category: "video"},
	{List: "iqiyi", Key: "iqiyi", Label: "iQIYI", Category: "video"},
	{List: "youku", Key: "youku", Label: "Youku", Category: "video"},
	{List: "niconico", Key: "niconico", Label: "Niconico", Category: "video"},
	{List: "plex", Key: "plex", Label: "Plex", Category: "video"},
	{List: "spotify", Key: "spotify", Label: "Spotify", Category: "music"},
	{List: "soundcloud", Key: "soundcloud", Label: "SoundCloud", Category: "music"},

	// Gaming.
	{List: "steam", Key: "steam", Label: "Steam", Category: "gaming"},
	{List: "epicgames", Key: "epic-games", Label: "Epic Games", Category: "gaming"},
	{List: "ea", Key: "ea", Label: "EA", Category: "gaming"},
	{List: "ubisoft", Key: "ubisoft", Label: "Ubisoft", Category: "gaming"},
	{List: "roblox", Key: "roblox", Label: "Roblox", Category: "gaming"},
	{List: "nintendo", Key: "nintendo", Label: "Nintendo", Category: "gaming"},
	{List: "playstation", Key: "playstation", Label: "PlayStation", Category: "gaming"},
	{List: "gog", Key: "gog", Label: "GOG", Category: "gaming"},

	// AI.
	{List: "openai", Key: "openai", Label: "OpenAI", Category: "ai"},
	{List: "anthropic", Key: "anthropic", Label: "Anthropic", Category: "ai"},
	{List: "huggingface", Key: "huggingface", Label: "Hugging Face", Category: "ai"},
	{List: "perplexity", Key: "perplexity", Label: "Perplexity", Category: "ai"},

	// Developer and cloud.
	{List: "gitlab", Key: "gitlab", Label: "GitLab", Category: "developer"},
	{List: "codeberg", Key: "codeberg", Label: "Codeberg", Category: "developer"},
	{List: "sourceforge", Key: "sourceforge", Label: "SourceForge", Category: "developer"},
	{List: "stackexchange", Key: "stackexchange", Label: "Stack Exchange", Category: "developer"},
	{List: "docker", Key: "docker", Label: "Docker", Category: "developer"},
	{List: "jetbrains", Key: "jetbrains", Label: "JetBrains", Category: "developer"},
	{List: "unity", Key: "unity", Label: "Unity", Category: "developer"},
	{List: "heroku", Key: "heroku", Label: "Heroku", Category: "cloud"},
	{List: "vercel", Key: "vercel", Label: "Vercel", Category: "cloud"},
	{List: "netlify", Key: "netlify", Label: "Netlify", Category: "cloud"},
	{List: "digitalocean", Key: "digitalocean", Label: "DigitalOcean", Category: "cloud"},
	{List: "vultr", Key: "vultr", Label: "Vultr", Category: "cloud"},
	{List: "oracle", Key: "oracle", Label: "Oracle", Category: "cloud"},
	{List: "ibm", Key: "ibm", Label: "IBM", Category: "cloud"},
	{List: "salesforce", Key: "salesforce", Label: "Salesforce", Category: "productivity"},
	{List: "atlassian", Key: "atlassian", Label: "Atlassian", Category: "productivity"},
	{List: "cloudflare", Key: "cloudflare", Label: "Cloudflare", Category: "cdn"},
	{List: "akamai", Key: "akamai", Label: "Akamai", Category: "cdn"},
	{List: "fastly", Key: "fastly", Label: "Fastly", Category: "cdn"},
	{List: "jsdelivr", Key: "jsdelivr", Label: "jsDelivr", Category: "cdn"},

	// Productivity, storage, and mail.
	{List: "notion", Key: "notion", Label: "Notion", Category: "productivity"},
	{List: "figma", Key: "figma", Label: "Figma", Category: "productivity"},
	{List: "canva", Key: "canva", Label: "Canva", Category: "productivity"},
	{List: "adobe", Key: "adobe", Label: "Adobe", Category: "productivity"},
	{List: "autodesk", Key: "autodesk", Label: "Autodesk", Category: "productivity"},
	{List: "zoho", Key: "zoho", Label: "Zoho", Category: "productivity"},
	{List: "dropbox", Key: "dropbox", Label: "Dropbox", Category: "storage"},
	{List: "mega", Key: "mega", Label: "MEGA", Category: "storage"},
	{List: "protonmail", Key: "proton", Label: "Proton", Category: "email"},
	{List: "tutanota", Key: "tutanota", Label: "Tuta", Category: "email"},

	// Finance, commerce, and travel.
	{List: "paypal", Key: "paypal", Label: "PayPal", Category: "finance"},
	{List: "stripe", Key: "stripe", Label: "Stripe", Category: "finance"},
	{List: "visa", Key: "visa", Label: "Visa", Category: "finance"},
	{List: "mastercard", Key: "mastercard", Label: "Mastercard", Category: "finance"},
	{List: "wise", Key: "wise", Label: "Wise", Category: "finance"},
	{List: "binance", Key: "binance", Label: "Binance", Category: "finance"},
	{List: "kraken", Key: "kraken", Label: "Kraken", Category: "finance"},
	{List: "ebay", Key: "ebay", Label: "eBay", Category: "shopping"},
	{List: "shopify", Key: "shopify", Label: "Shopify", Category: "shopping"},
	{List: "shopee", Key: "shopee", Label: "Shopee", Category: "shopping"},
	{List: "walmart", Key: "walmart", Label: "Walmart", Category: "shopping"},
	{List: "target", Key: "target", Label: "Target", Category: "shopping"},
	{List: "bestbuy", Key: "bestbuy", Label: "Best Buy", Category: "shopping"},
	{List: "jd", Key: "jd", Label: "JD.com", Category: "shopping"},
	{List: "alibaba", Key: "alibaba", Label: "Alibaba", Category: "shopping"},
	{List: "booking", Key: "booking", Label: "Booking.com", Category: "travel"},
	{List: "airbnb", Key: "airbnb", Label: "Airbnb", Category: "travel"},
	{List: "uber", Key: "uber", Label: "Uber", Category: "travel"},

	// News and reference.
	{List: "nytimes", Key: "nytimes", Label: "The New York Times", Category: "news"},
	{List: "bbc", Key: "bbc", Label: "BBC", Category: "news"},
	{List: "cnn", Key: "cnn", Label: "CNN", Category: "news"},
	{List: "reuters", Key: "reuters", Label: "Reuters", Category: "news"},
	{List: "bloomberg", Key: "bloomberg", Label: "Bloomberg", Category: "news"},
	{List: "wsj", Key: "wsj", Label: "The Wall Street Journal", Category: "news"},
	{List: "economist", Key: "economist", Label: "The Economist", Category: "news"},
	{List: "wikimedia", Key: "wikimedia", Label: "Wikipedia", Category: "reference"},
	{List: "archive", Key: "archive", Label: "Internet Archive", Category: "reference"},

	// Education.
	{List: "coursera", Key: "coursera", Label: "Coursera", Category: "education"},
	{List: "udemy", Key: "udemy", Label: "Udemy", Category: "education"},
	{List: "edx", Key: "edx", Label: "edX", Category: "education"},
	{List: "khanacademy", Key: "khanacademy", Label: "Khan Academy", Category: "education"},
	{List: "duolingo", Key: "duolingo", Label: "Duolingo", Category: "education"},
	{List: "skillshare", Key: "skillshare", Label: "Skillshare", Category: "education"},

	// Security and networking tools.
	{List: "tailscale", Key: "tailscale", Label: "Tailscale", Category: "security"},
	{List: "bitwarden", Key: "bitwarden", Label: "Bitwarden", Category: "security"},
	{List: "lastpass", Key: "lastpass", Label: "LastPass", Category: "security"},
	{List: "speedtest", Key: "speedtest", Label: "Speedtest", Category: "diagnostics"},

	// Umbrella platform with a very large include set; keep it after the
	// specific services so it cannot swallow them.
	{List: "tencent", Key: "tencent", Label: "Tencent", Category: "platform"},

	// Functional buckets rather than a single vendor.
	{List: "category-ads-all", Key: "ads", Label: "Ads and tracking", Category: "ads"},
	{List: "category-porn", Key: "adult", Label: "Adult", Category: "adult"},
}

const (
	defaultRepo = "v2fly/domain-list-community"
	defaultRef  = "master"
)

func main() {
	var (
		src     = flag.String("src", "", "path to an extracted domain-list-community `data` directory; when empty the upstream tarball is downloaded")
		repo    = flag.String("repo", defaultRepo, "GitHub repository to download when -src is empty")
		ref     = flag.String("ref", defaultRef, "git ref to download when -src is empty")
		out     = flag.String("out", "internal/servicecatalog/data/services.tsv", "output TSV path")
		version = flag.String("version", "", "dataset version stamp; defaults to today's UTC date")
	)
	flag.Parse()

	if *version == "" {
		*version = time.Now().UTC().Format("2006-01-02")
	}

	lists, origin, err := loadLists(*src, *repo, *ref)
	if err != nil {
		fail(err)
	}
	catalog, err := build(lists)
	if err != nil {
		fail(err)
	}
	if err := write(*out, *version, origin, catalog); err != nil {
		fail(err)
	}

	rules := 0
	for _, svc := range catalog {
		rules += len(svc.Suffixes) + len(svc.Exacts)
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d services, %d rules, source %s\n", *out, len(catalog), rules, origin)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "servicecatalog/gen:", err)
	os.Exit(1)
}

// service is one emitted catalog block.
type service struct {
	serviceSpec
	Suffixes []string
	Exacts   []string
}

// loadLists returns the raw upstream lists keyed by list name plus a
// human-readable provenance string.
func loadLists(src, repo, ref string) (map[string][]string, string, error) {
	if src != "" {
		lists, err := loadListsFromDir(src)
		if err != nil {
			return nil, "", err
		}
		return lists, fmt.Sprintf("%s (local: %s)", defaultRepo, filepath.Clean(src)), nil
	}
	return loadListsFromTarball(repo, ref)
}

func loadListsFromDir(dir string) (map[string][]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	lists := make(map[string][]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		lists[entry.Name()] = strings.Split(string(raw), "\n")
	}
	if len(lists) == 0 {
		return nil, fmt.Errorf("no list files found under %s", dir)
	}
	return lists, nil
}

func loadListsFromTarball(repo, ref string) (map[string][]string, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/tarball/%s", repo, ref)
	resp, err := http.Get(url) //nolint:gosec // operator-supplied repository, run by hand
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, "", err
	}
	defer gz.Close()

	lists := make(map[string][]string, 2048)
	root := ""
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		clean := path.Clean(header.Name)
		parts := strings.Split(clean, "/")
		if len(parts) != 3 || parts[1] != "data" {
			continue
		}
		root = parts[0]
		raw, err := io.ReadAll(tr)
		if err != nil {
			return nil, "", err
		}
		lists[parts[2]] = strings.Split(string(raw), "\n")
	}
	if len(lists) == 0 {
		return nil, "", fmt.Errorf("download %s: archive contained no data/ entries", url)
	}
	return lists, fmt.Sprintf("%s@%s (%s)", repo, ref, root), nil
}

// build expands the allowlist into per-service rule sets, claiming each domain
// for the first spec that yields it and dropping rules a broader same-service
// rule already covers.
func build(lists map[string][]string) ([]service, error) {
	suffixOwner := map[string]string{}
	exactOwner := map[string]string{}
	collected := make([]service, 0, len(allowlist))
	seenKeys := map[string]string{}

	for _, spec := range allowlist {
		if prior, ok := seenKeys[spec.Key]; ok {
			return nil, fmt.Errorf("duplicate service key %q (lists %q and %q)", spec.Key, prior, spec.List)
		}
		seenKeys[spec.Key] = spec.List

		suffixes, exacts, err := expand(lists, spec.List)
		if err != nil {
			return nil, err
		}
		svc := service{serviceSpec: spec}
		for _, domain := range suffixes {
			if _, taken := suffixOwner[domain]; taken {
				continue
			}
			suffixOwner[domain] = spec.Key
			svc.Suffixes = append(svc.Suffixes, domain)
		}
		for _, domain := range exacts {
			if _, taken := exactOwner[domain]; taken {
				continue
			}
			exactOwner[domain] = spec.Key
			svc.Exacts = append(svc.Exacts, domain)
		}
		collected = append(collected, svc)
	}

	var empty []string
	for i := range collected {
		svc := &collected[i]
		svc.Suffixes = pruneSuffixes(svc.Key, svc.Suffixes, suffixOwner)
		svc.Exacts = pruneExacts(svc.Key, svc.Exacts, suffixOwner)
		sort.Strings(svc.Suffixes)
		sort.Strings(svc.Exacts)
		if len(svc.Suffixes)+len(svc.Exacts) == 0 {
			empty = append(empty, fmt.Sprintf("%s (list %s)", svc.Key, svc.List))
		}
	}
	// A service that claims nothing is fully covered by an earlier allowlist
	// entry. That is an allowlist bug, not a data condition: fix the ordering or
	// drop the entry rather than shipping a service key nothing can ever match.
	if len(empty) > 0 {
		return nil, fmt.Errorf("services contributed no rules: %s", strings.Join(empty, ", "))
	}
	return collected, nil
}

// expand reads one upstream list and every list it includes, transitively.
func expand(lists map[string][]string, name string) (suffixes, exacts []string, err error) {
	visited := map[string]bool{}
	var walk func(string) error
	walk = func(list string) error {
		if visited[list] {
			return nil
		}
		visited[list] = true
		lines, ok := lists[list]
		if !ok {
			return fmt.Errorf("upstream list %q not found", list)
		}
		for _, line := range lines {
			rule, ok := parseRule(line)
			if !ok {
				continue
			}
			switch rule.kind {
			case ruleInclude:
				if err := walk(rule.value); err != nil {
					return err
				}
			case ruleSuffix:
				suffixes = append(suffixes, rule.value)
			case ruleExact:
				exacts = append(exacts, rule.value)
			}
		}
		return nil
	}
	if err := walk(name); err != nil {
		return nil, nil, err
	}
	return suffixes, exacts, nil
}

type ruleKind int

const (
	ruleSkip ruleKind = iota
	ruleInclude
	ruleSuffix
	ruleExact
)

type rule struct {
	kind  ruleKind
	value string
}

// parseRule decodes one upstream line. `regexp:` and `keyword:` rules need a
// matcher this catalog deliberately does not have, so they are dropped.
func parseRule(line string) (rule, bool) {
	if idx := strings.IndexByte(line, '#'); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return rule{}, false
	}
	// Trailing ` @attr` tags are metadata (geography, ads, ...), not part of the
	// pattern.
	value := strings.Fields(line)[0]

	kind := ruleSuffix
	if idx := strings.IndexByte(value, ':'); idx >= 0 {
		prefix, rest := value[:idx], value[idx+1:]
		switch prefix {
		case "include":
			return rule{kind: ruleInclude, value: rest}, rest != ""
		case "full":
			kind, value = ruleExact, rest
		case "domain":
			value = rest
		default:
			return rule{kind: ruleSkip}, false
		}
	}

	value = strings.ToLower(strings.Trim(value, "."))
	if !validDomain(value) {
		return rule{kind: ruleSkip}, false
	}
	return rule{kind: kind, value: value}, true
}

func validDomain(domain string) bool {
	if domain == "" || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			default:
				return false
			}
		}
	}
	return true
}

// pruneSuffixes drops a suffix rule when the nearest ancestor rule across all
// services already belongs to the same service, which is exactly when the
// longest-suffix lookup would reach the same answer without it.
func pruneSuffixes(key string, domains []string, owner map[string]string) []string {
	kept := domains[:0]
	for _, domain := range domains {
		if nearestOwner(parent(domain), owner) == key {
			continue
		}
		kept = append(kept, domain)
	}
	return kept
}

// pruneExacts drops an exact rule that a same-service suffix rule already
// covers, including a suffix rule for the host itself.
func pruneExacts(key string, domains []string, owner map[string]string) []string {
	kept := domains[:0]
	for _, domain := range domains {
		if nearestOwner(domain, owner) == key {
			continue
		}
		kept = append(kept, domain)
	}
	return kept
}

func nearestOwner(domain string, owner map[string]string) string {
	for domain != "" {
		if key, ok := owner[domain]; ok {
			return key
		}
		domain = parent(domain)
	}
	return ""
}

func parent(domain string) string {
	if idx := strings.IndexByte(domain, '.'); idx >= 0 {
		return domain[idx+1:]
	}
	return ""
}

func write(out, version, origin string, catalog []service) error {
	rules := 0
	for _, svc := range catalog {
		rules += len(svc.Suffixes) + len(svc.Exacts)
	}

	var b strings.Builder
	b.WriteString("# BoxFleet service catalog. Generated by internal/servicecatalog/gen — do not edit by hand.\n")
	b.WriteString("# Upstream data is MIT licensed; see gen/README.md for the regeneration procedure.\n")
	b.WriteString("# S<TAB>key<TAB>label<TAB>category starts a service block; D suffix rules and F exact\n")
	b.WriteString("# rules that follow belong to it.\n")
	fmt.Fprintf(&b, "!version\t%s\n", version)
	fmt.Fprintf(&b, "!source\t%s\n", origin)
	fmt.Fprintf(&b, "!services\t%d\n", len(catalog))
	fmt.Fprintf(&b, "!rules\t%d\n", rules)
	for _, svc := range catalog {
		fmt.Fprintf(&b, "S\t%s\t%s\t%s\n", svc.Key, svc.Label, svc.Category)
		for _, domain := range svc.Suffixes {
			fmt.Fprintf(&b, "D\t%s\n", domain)
		}
		for _, domain := range svc.Exacts {
			fmt.Fprintf(&b, "F\t%s\n", domain)
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, []byte(b.String()), 0o644)
}
