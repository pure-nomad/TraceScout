package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
)

type UserFlags struct {
	Interval int
	Keywords []string
	URLs     []string
	Headers  []string
}

func main() {
	fmt.Println("TraceScout - Made By Erg0sum")

	var interval int
	var keywords string
	var headers string
	var list string

	flag.IntVar(
		&interval,
		"interval",
		60,
		"Polling interval in seconds for Trace.axd updates — example: -interval 300",
	)
	flag.StringVar(
		&keywords,
		"keywords",
		"session,authorization,password,username,token",
		"Comma‑separated keywords to scan in Trace.axd requests — example: -keywords auth,token",
	)
	flag.StringVar(
		&headers,
		"headers",
		"",
		"Comma‑separated HTTP headers (e.g., -headers \"User-Agent: bugbounty,Cookie: session=true\")",
	)

	flag.StringVar(
		&list,
		"list",
		"",
		"Path to file with URLs to monitor; example: -list hosts.txt",
	)
	flag.Parse()
	options, err := formatFlags(interval, keywords, headers, list)
	if err != nil {
		log.Fatalln("Error formatting use flags: ", err)
	}

	log.Println("User Flags")
	log.Println(options)
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

func formatFlags(interval int, keywords string, headers string, list string) (*UserFlags, error) {

	kw := strings.Split(keywords, ",")
	hd := strings.Split(headers, ",")

	var uri []string
	if list != "" {
		var err error
		uri, err = readURLs(list)
		if err != nil {
			return nil, err
		}
	}

	return &UserFlags{
		Interval: interval,
		Keywords: kw,
		Headers:  hd,
		URLs:     uri,
	}, nil
}
