package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/pure-nomad/cordkit"
)

type UserFlags struct {
	Interval       int
	Keywords       []string
	URLs           []string
	Headers        []string
	CordkitEnabled bool
	Timeout        int
	Verbose        bool
}

func main() {
	fmt.Println("TraceScout - Made By Erg0sum")

	var (
		interval   = flag.Int("interval", 60, "Polling interval in seconds for Trace.axd updates — example: -interval 300")
		keywords   = flag.String("keywords", "session,authorization,password,username,token", "Comma‑separated keywords to scan")
		headers    = flag.String("headers", "", "Comma‑separated HTTP headers; e.g., \"User-Agent: bugbounty,Cookie: session=true\"")
		list       = flag.String("list", "", "Path to file with URLs to monitor; example: -list hosts.txt")
		configPath = flag.String("config", "", "Path to config JSON file; required if cordkit is enabled")
		ckEnabled  = flag.Bool("cordkit", false, "Enable Cordkit (Discord Notifications)")
		timeout    = flag.Int("timeout", 15, "HTTP timeout for requests")
		verbose    = flag.Bool("verbose", false, "Verbose Logging")
	)

	flag.Parse()

	if *ckEnabled && *configPath == "" {
		log.Fatal("error: -cordkit requires -config <path to JSON config>")
	}

	if *list == "" {
		fmt.Fprintln(os.Stderr, "error: -list <file> is required; -h for help")
		os.Exit(2)
	}

	read_list, err := readURLs(*list)
	if err != nil {
		log.Panicf("%s", err)
	}

	var opts *UserFlags

	opts = &UserFlags{
		Interval:       *interval,
		Keywords:       splitCSV(*keywords),
		Headers:        splitCSV(*headers),
		URLs:           read_list,
		CordkitEnabled: *ckEnabled,
		Timeout:        *timeout,
		Verbose:        *verbose,
	}

	if *configPath != "" {
		opts, err = parseConfig(*configPath)
		if err != nil {
			log.Panicf("%s", err)
		}
	}

	if opts.Interval <= 0 {
		log.Fatal("interval must be > 0")
	}

	var bot *cordkit.Bot
	if opts.CordkitEnabled {
		bot, err = cordkitInit(*configPath)
		if err != nil {
			log.Panicf("cordkit init failed: %v", err)
		}
		defer bot.Stop()
		bot.Start()
		bot.SendMsg("1372558505562869905", "testing")
	}

	log.Println("User Flags")
	log.Println(opts)
}

func cordkitInit(config string) (*cordkit.Bot, error) {
	b, err := cordkit.NewBot(config)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func splitCSV(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseConfig(filename string) (*UserFlags, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Interval       int    `json:"interval"`
		Keywords       string `json:"keywords"`
		Headers        string `json:"headers"`
		CordkitEnabled bool   `json:"cordkit_enabled"`
		Timeout        int    `json:"timeout"`
		Verbose        bool   `json:"verbose"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	return &UserFlags{
		Interval:       raw.Interval,
		Keywords:       splitCSV(raw.Keywords),
		Headers:        splitCSV(raw.Headers),
		CordkitEnabled: raw.CordkitEnabled,
		Timeout:        raw.Timeout,
		Verbose:        raw.Verbose,
	}, nil
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
			return nil, fmt.Errorf("please assure your URLs are pointing to the Trace.axd file")
		}

		u, err := url.Parse(line)
		if err != nil {
			return nil, err
		}

		host := u.Hostname()
		path := u.Path
		out = append(out, u.Scheme+"://"+host+path)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
