package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nnivxix/utub/internal/config"
	"github.com/nnivxix/utub/internal/media"
)

// jobRequest is a valid 1080p request whose output directory really exists, so
// Start's checks pass and only the download itself is under test.
func jobRequest(t *testing.T) Request {
	t.Helper()

	req := videoRequest(t)
	req.OutDir = t.TempDir()
	return req
}

// missingBin points the executor at a binary that cannot exist, so a start
// fails immediately without shelling out to the real yt-dlp.
func missingBin(dir string) Executor {
	return Executor{Bin: filepath.Join(dir, "no-such-yt-dlp")}
}

// waitForJobState polls a job until it reaches want. The manager updates state
// from its own goroutine, so tests cannot assume it is final on return from
// Start.
func waitForJobState(t *testing.T, job *Job, want State) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := job.State(); got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s state = %s, want %s", job.ID, job.State(), want)
}

// collectEvents drains a job's stream until it closes, failing the test when it
// never does.
func collectEvents(t *testing.T, ch <-chan Event) []Event {
	t.Helper()

	var events []Event
	timeout := time.After(15 * time.Second)

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("event stream did not close")
			return events
		}
	}
}

func TestEngineStartValidatesBeforeSpawning(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		narrow func(*Request)
		want   error
	}{
		{
			name: "empty url",
			narrow: func(r *Request) {
				r.URL = ""
			},
			want: media.ErrEmptyURL,
		},
		{
			name: "not a youtube link",
			narrow: func(r *Request) {
				r.URL = "https://vimeo.com/12345"
			},
			want: media.ErrUnsupportedURL,
		},
		{
			name: "empty download dir",
			narrow: func(r *Request) {
				r.OutDir = " "
			},
			want: ErrBadOutDir,
		},
		{
			name: "missing download dir",
			narrow: func(r *Request) {
				r.OutDir = filepath.Join(t.TempDir(), "gone")
			},
			want: ErrBadOutDir,
		},
		{
			name: "download dir is a file",
			narrow: func(r *Request) {
				r.OutDir = file
			},
			want: ErrBadOutDir,
		},
		{
			name: "forced aria2c without the binary",
			narrow: func(r *Request) {
				r.Config.Downloader = config.DownloaderAria2c
			},
			want: ErrMissingAria2c,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := &Engine{Executor: Executor{}}
			req := jobRequest(t)
			if tc.narrow != nil {
				tc.narrow(&req)
			}

			job, events, err := engine.Start(req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Start() error = %v, want %v", err, tc.want)
			}
			if job != nil || events != nil {
				t.Errorf("Start() returned a job and a stream alongside an error")
			}
			if jobs := engine.List(); jobs != nil {
				t.Errorf("List() = %v, want nil: a rejected job must not be registered", jobs)
			}
		})
	}
}

// TestEngineStartCarriesProbeAndCanonicalURL pins the contract the TUI relies
// on in M1.8: the probe result travels with the request (no second probe) and
// the job stores the canonical link, not the raw pasted one.
func TestEngineStartCarriesProbeAndCanonicalURL(t *testing.T) {
	t.Parallel()

	var probes int
	engine := &Engine{
		Prober: Prober{Run: func(context.Context, string, ...string) ([]byte, []byte, error) {
			probes++
			return nil, nil, errors.New("probe must not run during Start")
		}},
		Executor: missingBin(t.TempDir()),
	}

	source := media.MediaInfo{
		ID:      "aqz-KE-bpKQ",
		Title:   "Big Buck Bunny",
		Source:  media.SourceYouTubeMusic,
		Formats: []media.Format{{ID: "137", Ext: "mp4", VCodec: "avc1.640028", Height: 1080}},
	}

	req := jobRequest(t)
	req.URL = "https://music.youtube.com/watch?v=aqz-KE-bpKQ&list=PL123&t=30"
	req.Source = source

	job, events, err := engine.Start(req)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	collectEvents(t, events)

	if probes != 0 {
		t.Errorf("Start ran %d probes, want 0: the caller already probed", probes)
	}
	if want := "https://music.youtube.com/watch?v=aqz-KE-bpKQ&t=30"; job.Request.URL != want {
		t.Errorf("Request.URL = %q, want the canonical %q", job.Request.URL, want)
	}
	if job.Source.ID != source.ID || job.Source.Title != source.Title {
		t.Errorf("Job.Source = %+v, want the probe's metadata", job.Source)
	}
	if job.Request.OutDir != req.OutDir {
		t.Errorf("Request.OutDir = %q, want %q", job.Request.OutDir, req.OutDir)
	}
}

func TestEngineCancelUnknownJob(t *testing.T) {
	t.Parallel()

	err := (&Engine{}).Cancel("job-nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Cancel() error = %v, want ErrNotFound", err)
	}
}

func TestEngineCancelFinishedJob(t *testing.T) {
	t.Parallel()

	engine := &Engine{Executor: missingBin(t.TempDir())}

	job, events, err := engine.Start(jobRequest(t))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	collectEvents(t, events)
	waitForJobState(t, job, StateFailed)

	if err := engine.Cancel(job.ID); !errors.Is(err, ErrFinished) {
		t.Fatalf("Cancel(finished) error = %v, want ErrFinished", err)
	}
}

func TestEngineListNewestFirst(t *testing.T) {
	t.Parallel()

	engine := &Engine{Executor: missingBin(t.TempDir())}

	var ids []string
	engine.NewID = func() string {
		id := "job-" + string(rune('a'+len(ids)))
		ids = append(ids, id)
		return id
	}

	for range 3 {
		if _, _, err := engine.Start(jobRequest(t)); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	}

	jobs := engine.List()
	if len(jobs) != 3 {
		t.Fatalf("List() returned %d jobs, want 3", len(jobs))
	}
	for i, want := range []string{"job-c", "job-b", "job-a"} {
		if jobs[i].ID != want {
			t.Errorf("List()[%d].ID = %q, want %q (newest first)", i, jobs[i].ID, want)
		}
	}
}

func TestDefaultJobIDIsUniqueAndPrefixed(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 100)
	for range 100 {
		id := defaultJobID()
		if len(id) < len("job-")+16 || id[:4] != "job-" {
			t.Fatalf("id = %q, want a job-<hex> id", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}
