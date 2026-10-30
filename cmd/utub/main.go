// Command utub converts and downloads YouTube and YouTube Music links.
//
// Only the diagnostic flags exist so far: --doctor reports the config location
// and the external binaries the engine will shell out to. Step 7 replaces the
// default branch with the Bubble Tea TUI; until then running utub with no
// arguments explains that and exits.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"text/tabwriter"
	"time"

	"github.com/nnivxix/utub/internal/binmgr"
	"github.com/nnivxix/utub/internal/config"
)

// version is stamped at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

// doctorTimeout bounds the whole report; each individual probe has its own
// shorter timeout inside binmgr.
const doctorTimeout = 30 * time.Second

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	doctor := flag.Bool("doctor", false, "check config and external binaries")
	flag.Usage = usage
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Printf("utub %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)

	case *doctor:
		os.Exit(runDoctor())

	case flag.NArg() > 0:
		fmt.Fprintf(os.Stderr, "utub: unexpected argument %q\n\n", flag.Arg(0))
		usage()
		os.Exit(2)

	default:
		fmt.Fprintln(os.Stderr, "utub: the interactive TUI arrives in step 7.")
		fmt.Fprintln(os.Stderr, "      for now: utub --doctor | utub --version")
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `utub %s — YouTube / YouTube Music downloader

usage:
  utub            start the interactive TUI
  utub --doctor   check config and external binaries
  utub --version  print the version

core requires yt-dlp and ffmpeg on PATH; aria2c is optional and only upgrades
the downloader to multi-connection ranges.
`, version)
}

// runDoctor prints the environment report and returns a process exit code:
// 0 when every required binary is usable, 1 otherwise.
func runDoctor() int {
	fmt.Printf("utub %s  %s/%s\n\n", version, runtime.GOOS, runtime.GOARCH)

	reportConfig()

	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()

	set := binmgr.Locate(ctx)
	reportBinaries(set)

	if err := set.Ready(); err != nil {
		fmt.Printf("\nnot ready: %v\n", err)
		return 1
	}

	fmt.Printf("\nready — %d/%d required binaries available", countAvailable(set), countRequired(set))
	if set.Aria2c.Available() {
		fmt.Print(", aria2c will be used for multi-connection downloads")
	}
	fmt.Println()
	return 0
}

// countRequired returns how many tools are mandatory (yt-dlp, ffmpeg).
func countRequired(set binmgr.Set) int {
	n := 0
	for _, tool := range set.All() {
		if tool.Required {
			n++
		}
	}
	return n
}

// countAvailable returns how many required tools are actually usable.
func countAvailable(set binmgr.Set) int {
	n := 0
	for _, tool := range set.All() {
		if tool.Required && tool.Available() {
			n++
		}
	}
	return n
}

// reportConfig prints where settings live and what they resolve to. An
// unreadable config is reported but does not abort the doctor: the binaries are
// still worth checking.
func reportConfig() {
	path := config.Path()
	cfg, err := config.Load()

	fmt.Printf("config    %s\n", path)
	switch {
	case err != nil:
		fmt.Printf("          invalid: %v\n", err)
		fmt.Println("          falling back to defaults for this report")
		cfg = config.Default()
	case !fileExists(path):
		fmt.Println("          not created yet — using defaults")
	}

	dir, dirErr := cfg.EnsureDownloadDir()
	if dirErr != nil {
		fmt.Printf("download  %s  (unusable: %v)\n", cfg.DownloadDir, dirErr)
	} else {
		fmt.Printf("download  %s\n", dir)
	}

	fmt.Printf("codec     %s", cfg.VideoCodec)
	if cfg.VideoCodec == config.CodecH264 {
		fmt.Print("  (H.264 only — max 1080p, plays everywhere)")
	}
	fmt.Println()
	fmt.Println()
}

// reportBinaries prints one row per tool plus install hints for anything
// required that is unusable.
func reportBinaries(set binmgr.Set) {
	fmt.Println("binaries")

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, tool := range set.All() {
		mark, path, status := describeTool(tool)
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", tool.Name, mark, path, status)
	}
	tw.Flush()

	missing := set.Missing()
	if len(missing) == 0 {
		return
	}

	fmt.Println("\ninstall what is missing:")
	for _, tool := range missing {
		fmt.Printf("  %s\n", tool.Name)
		for _, hint := range tool.Hints {
			fmt.Printf("    %s\n", hint)
		}
	}
}

// describeTool turns a Tool into the three table columns.
func describeTool(tool binmgr.Tool) (mark, path, status string) {
	path = tool.Path
	if path == "" {
		path = "-"
	}

	switch {
	case tool.Available():
		mark, status = "ok", tool.Version
		if status == "" {
			status = "version unknown"
		}

	case errors.Is(tool.Err, binmgr.ErrNotFound):
		mark = "missing"
		status = "not found"
		if !tool.Required {
			status = "not found (optional)"
		}

	case errors.Is(tool.Err, binmgr.ErrTooOld):
		mark = "old"
		status = fmt.Sprintf("have %s, need >= %s", tool.Version, binmgr.MinYTDLPVersion)

	default:
		mark, status = "error", tool.Err.Error()
	}
	return mark, path, status
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
