package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type UserFlags struct {
	Interval int
	Keywords []string
	URLs     []string
	Headers  []string
	Timeout  int
	Verbose  bool
}

type StartEntry struct {
	ID        int       `json:"id"`
	Timestamp time.Time `json:"timestamp"`
}

type FullEntry struct {
	ID          int                 `json:"id"`
	SessionID   string              `json:"sessionId,omitempty"`
	Method      string              `json:"method,omitempty"`
	URL         string              `json:"url,omitempty"`
	StatusCode  int                 `json:"statusCode,omitempty"`
	RequestTime time.Time           `json:"requestTime,omitempty"`
	TraceInfo   []map[string]string `json:"traceInfo,omitempty"`
	Headers     map[string]string   `json:"headers,omitempty"`
	Cookies     map[string]string   `json:"cookies,omitempty"`
}

func main() {
	interval := flag.Int("interval", 60, "Polling interval in seconds for Trace.axd updates")
	keywords := flag.String("keywords", "session,auth,token", "Comma-separated keywords to scan")
	headers := flag.String("headers", "", "Comma-separated HTTP headers (Name:Value)")
	list := flag.String("list", "", "Path to file with URLs to monitor")
	timeout := flag.Int("timeout", 15, "HTTP timeout for requests in seconds")
	verbose := flag.Bool("verbose", false, "Verbose logging")
	flag.Parse()

	if *list == "" {
		log.Fatal("-list <file> is required")
	}

	urls, err := readURLs(*list)
	if err != nil {
		log.Fatalf("reading URLs: %v", err)
	}

	opts := &UserFlags{
		Interval: *interval,
		Keywords: splitCSV(*keywords),
		Headers:  splitCSV(*headers),
		URLs:     urls,
		Timeout:  *timeout,
		Verbose:  *verbose,
	}

	if err := rotateOldFiles("cache"); err != nil {
		log.Printf("warning: rotate cache failed: %v", err)
	}

	entries, err := fetchRequestIDS(opts)
	if err != nil {
		log.Fatalf("initial fetch IDS: %v", err)
	}
	last := getLastEntry(entries)
	if last == nil {
		log.Fatal("no start entries found")
	}
	lastLoggedID := last.ID
	saveJSON(last, "laststart.json")

	fulls, err := fetchAllRequestInformation(opts, 0, lastLoggedID)
	if err != nil {
		log.Fatalf("initial full fetch failed: %v", err)
	}
	saveJSON(fulls, fmt.Sprintf("log_update_%d.json", time.Now().Unix()))

	ticker := time.NewTicker(time.Duration(opts.Interval) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		entries, err := fetchRequestIDS(opts)
		if err != nil {
			log.Printf("fetch IDS: %v", err)
			continue
		}
		current := getLastEntry(entries)
		if current == nil || current.ID <= lastLoggedID {
			if opts.Verbose {
				log.Println("no new entries")
			}
			continue
		}

		newFulls, err := fetchAllRequestInformation(opts, lastLoggedID, current.ID)
		if err != nil {
			log.Printf("fetch full info: %v", err)
			continue
		}

		filename := fmt.Sprintf("log_update_%d_%d.json", lastLoggedID+1, current.ID)
		saveJSON(newFulls, filename)
		lastLoggedID = current.ID
		saveJSON(current, "laststart.json")
		log.Printf("saved %d new entries to %s", len(newFulls), filename)
	}
}

func fetchAllRequestInformation(opts *UserFlags, since, until int) ([]*FullEntry, error) {
	client := &http.Client{Timeout: time.Duration(opts.Timeout) * time.Second}
	var mu sync.Mutex
	var results []*FullEntry

	type job struct {
		urlBase string
		id      int
	}

	jobs := make(chan job)
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				detailURL := fmt.Sprintf("%s?id=%d", j.urlBase, j.id-1)
				resp, err := client.Get(detailURL)
				if err != nil {
					log.Printf("error GET %s: %v", detailURL, err)
					continue
				}
				doc, err := html.Parse(resp.Body)
				resp.Body.Close()
				if err != nil {
					log.Printf("parse detail HTML: %v", err)
					continue
				}
				fe, err := parseFullEntry(doc)
				if err != nil {
					log.Printf("parse full entry: %v", err)
					continue
				}
				fe.ID = j.id
				mu.Lock()
				results = append(results, fe)
				mu.Unlock()
			}
		}()
	}
	go func() {
		for _, base := range opts.URLs {
			for id := since + 1; id <= until; id++ {
				jobs <- job{urlBase: base, id: id}
			}
		}
		close(jobs)
	}()

	wg.Wait()
	return results, nil
}

func parseFullEntry(n *html.Node) (*FullEntry, error) {
	return &FullEntry{}, nil
}

func fetchRequestIDS(opts *UserFlags) ([]*StartEntry, error) {
	client := &http.Client{Timeout: time.Duration(opts.Timeout) * time.Second}
	var mu sync.Mutex
	var all []*StartEntry

	var wg sync.WaitGroup
	for _, u := range opts.URLs {
		wg.Add(1)
		go func(urlStr string) {
			defer wg.Done()
			resp, err := client.Get(urlStr)
			if err != nil {
				log.Printf("GET %s: %v", urlStr, err)
				return
			}
			doc, err := html.Parse(resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Printf("parse list HTML: %v", err)
				return
			}
			entries := parseStartEntries(doc)
			mu.Lock()
			all = append(all, entries...)
			mu.Unlock()
		}(u)
	}
	wg.Wait()
	return all, nil
}

func getLastEntry(entries []*StartEntry) *StartEntry {
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries[len(entries)-1]
}

func parseStartEntries(n *html.Node) []*StartEntry {
	var entries []*StartEntry
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "tr" {
			var tds []*html.Node
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "td" {
					tds = append(tds, c)
				}
			}
			if len(tds) >= 2 {
				idStr := getText(tds[0])
				timeStr := getText(tds[1])
				id, _ := strconv.Atoi(strings.TrimSpace(idStr))
				ts, _ := time.Parse("02/01/2006 15:04:05", strings.TrimSpace(timeStr))
				entries = append(entries, &StartEntry{ID: id, Timestamp: ts})
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return entries
}

func getText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(getText(c))
	}
	return sb.String()
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func saveJSON(v interface{}, filename string) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(filename, data, 0644)
}

func rotateOldFiles(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	files, err := filepath.Glob("*.json")
	if err != nil {
		return err
	}
	for _, f := range files {
		newName := fmt.Sprintf("%s/%s_%d", dir, f, time.Now().UnixNano())
		os.Rename(f, newName)
	}
	return nil
}

func readURLs(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var out []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "\uFEFF")
		if line == "" {
			continue
		}
		if !strings.Contains(line, "://") {
			line = "https://" + line
		}
		if !strings.Contains(line, "Trace.axd") {
			return nil, fmt.Errorf("URLs must point to Trace.axd")
		}
		u, _ := url.Parse(line)
		out = append(out, u.Scheme+"://"+u.Hostname()+u.Path)
	}
	return out, nil
}
