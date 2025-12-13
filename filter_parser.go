//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	domains := make(map[string]struct{})

	filterDir := "assets/filters"
	files, err := os.ReadDir(filterDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading filter directory: %v\n", err)
		os.Exit(1)
	}

	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".txt") {
			continue
		}

		path := filepath.Join(filterDir, f.Name())
		file, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to open %s: %v\n", f.Name(), err)
			continue
		}

		count := 0
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "[") ||
				strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "#") {
				continue
			}

			if strings.HasPrefix(line, "||") {
				line = line[2:]

				endIdx := len(line)
				for i, c := range line {
					if c == '^' || c == '$' || c == '/' || c == '*' || c == '|' {
						endIdx = i
						break
					}
				}

				domain := strings.ToLower(line[:endIdx])

				if strings.Contains(domain, "*") || !strings.Contains(domain, ".") {
					continue
				}

				if isIPAddress(domain) {
					continue
				}

				if _, exists := domains[domain]; !exists {
					domains[domain] = struct{}{}
					count++
				}
			}
		}

		file.Close()

		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: error reading %s: %v\n", f.Name(), err)
		}

		fmt.Printf("parsed %s: %d domains\n", f.Name(), count)
	}

	domainList := make([]string, 0, len(domains))
	for d := range domains {
		domainList = append(domainList, d)
	}
	sort.Strings(domainList)

	output, err := json.Marshal(domainList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	outputPath := "assets/filters/domains.json"
	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nwrote %d unique domains to %s\n", len(domainList), outputPath)
}

func isIPAddress(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}
