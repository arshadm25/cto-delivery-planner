// Command csv-compare compares Redmine CSV issue exports with the issue index
// written by redmine-discovery. It reads local files only and makes no
// network requests.
//
//	csv-compare -tz Asia/Kolkata -discovery discovery-out export1.csv export2.csv
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/csvcompare"
)

func main() {
	discovery := flag.String("discovery", "discovery-out", "directory with projects.csv and issue-index.csv from redmine-discovery")
	out := flag.String("out", "compare-out", "directory for compare-report.md and compare-details.csv")
	tz := flag.String("tz", "", "time zone of the user who exported the CSVs, from Redmine › My account › Preferences (required), e.g. Asia/Kolkata")
	dateFmt := flag.String("date-format", "02-01-2006", "Go layout of dates in the CSV; the default matches DD-MM-YYYY")
	timeFmt := flag.String("time-format", "03:04 PM", "Go layout of times in the CSV; the default matches 05:45 PM")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: csv-compare -tz ZONE [flags] export.csv [export.csv ...]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *tz == "" || flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		fail("unknown time zone %q: %v", *tz, err)
	}
	layout := csvcompare.Layout{Date: *dateFmt, DateTime: *dateFmt + " " + *timeFmt, Location: loc}

	var exports []*csvcompare.Export
	for _, path := range flag.Args() {
		f, err := os.Open(path)
		if err != nil {
			fail("%v", err)
		}
		ex, err := csvcompare.ParseExport(filepath.Base(path), f, layout)
		f.Close()
		if err != nil {
			fail("%v", err)
		}
		exports = append(exports, ex)
	}

	projects, err := load(filepath.Join(*discovery, "projects.csv"), csvcompare.LoadProjects)
	if err != nil {
		fail("%v", err)
	}
	index, err := load(filepath.Join(*discovery, "issue-index.csv"), csvcompare.LoadIndex)
	if err != nil {
		fail("%v", err)
	}

	r := csvcompare.Compare(exports, projects, index)

	if err := os.MkdirAll(*out, 0o700); err != nil {
		fail("%v", err)
	}
	if err := write(filepath.Join(*out, "compare-report.md"), func(f *os.File) error { return csvcompare.WriteMarkdown(f, r) }); err != nil {
		fail("%v", err)
	}
	if err := write(filepath.Join(*out, "compare-details.csv"), func(f *os.File) error { return csvcompare.WriteDetailsCSV(f, r) }); err != nil {
		fail("%v", err)
	}
	fmt.Printf("Compared %d CSV issues with %d API issues in the same projects: %d matched, %d identical.\n",
		r.CSVUnique, r.APIInScope, r.Matched, r.Identical)
	fmt.Printf("Report written to %s/compare-report.md\n", *out)
}

func load[T any](path string, fn func(io.Reader) (T, error)) (T, error) {
	var zero T
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	return fn(f)
}

func write(path string, fn func(*os.File) error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "csv-compare: "+format+"\n", a...)
	os.Exit(1)
}
