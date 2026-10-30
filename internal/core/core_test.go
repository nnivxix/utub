package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStageString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		stage Stage
		want  string
	}{
		{StageProbe, "probe"},
		{StageVideo, "video"},
		{StageAudio, "audio"},
		{StageMerge, "merge"},
		{StagePost, "post"},
		{StageDone, "done"},
		{Stage(99), "unknown"},
	}

	for _, tc := range cases {
		if got := tc.stage.String(); got != tc.want {
			t.Errorf("Stage(%d).String() = %q, want %q", int(tc.stage), got, tc.want)
		}
	}
}

func TestStateString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		state State
		want  string
	}{
		{StatePending, "pending"},
		{StateRunning, "running"},
		{StateDone, "done"},
		{StateFailed, "failed"},
		{StateCanceled, "canceled"},
		{State(99), "unknown"},
	}

	for _, tc := range cases {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", int(tc.state), got, tc.want)
		}
	}
}

func TestStateIsTerminal(t *testing.T) {
	t.Parallel()

	cases := map[State]bool{
		StatePending:  false,
		StateRunning:  false,
		StateDone:     true,
		StateFailed:   true,
		StateCanceled: true,
	}

	for state, want := range cases {
		if got := state.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v, want %v", state, got, want)
		}
	}
}

func TestEventTerminal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		event Event
		want  bool
	}{
		{"mid download", Event{Stage: StageVideo, Percent: 0.5}, false},
		{"merge", Event{Stage: StageMerge, Percent: PercentUnknown}, false},
		{"done", Event{Stage: StageDone}, true},
		{"failed", Event{Stage: StageVideo, Err: errors.New("boom")}, true},
	}

	for _, tc := range cases {
		if got := tc.event.Terminal(); got != tc.want {
			t.Errorf("%s: Terminal() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func newTestJob() *Job {
	return &Job{
		ID:      "job-1",
		Request: Request{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		Created: time.Unix(0, 0),
	}
}

func TestJobLifecycle(t *testing.T) {
	t.Parallel()

	t.Run("new job is pending with no stage or output", func(t *testing.T) {
		job := newTestJob()

		if got := job.State(); got != StatePending {
			t.Fatalf("State() = %s, want pending", got)
		}
		if got := job.Stage(); got != StageProbe {
			t.Fatalf("Stage() = %s, want probe (zero value)", got)
		}
		if got := job.OutputPath(); got != "" {
			t.Fatalf("OutputPath() = %q, want empty", got)
		}
		if got := job.Err(); got != nil {
			t.Fatalf("Err() = %v, want nil", got)
		}
	})

	t.Run("success records path and done stage", func(t *testing.T) {
		job := newTestJob()
		job.start()
		job.setStage(StageVideo)
		job.succeed("/tmp/video.mp4")

		if got := job.State(); got != StateDone {
			t.Fatalf("State() = %s, want done", got)
		}
		if got := job.Stage(); got != StageDone {
			t.Fatalf("Stage() = %s, want done", got)
		}
		if got := job.OutputPath(); got != "/tmp/video.mp4" {
			t.Fatalf("OutputPath() = %q", got)
		}
		if got := job.Err(); got != nil {
			t.Fatalf("Err() = %v, want nil", got)
		}
	})

	t.Run("failure records the error", func(t *testing.T) {
		job := newTestJob()
		boom := errors.New("boom")
		job.start()
		job.fail(boom)

		if got := job.State(); got != StateFailed {
			t.Fatalf("State() = %s, want failed", got)
		}
		if got := job.Err(); !errors.Is(got, boom) {
			t.Fatalf("Err() = %v, want boom", got)
		}
	})

	t.Run("cancel is not an error", func(t *testing.T) {
		job := newTestJob()
		job.start()
		job.cancel()

		if got := job.State(); got != StateCanceled {
			t.Fatalf("State() = %s, want canceled", got)
		}
		if got := job.Err(); got != nil {
			t.Fatalf("Err() = %v, want nil for a cancellation", got)
		}
	})

	t.Run("first terminal state wins", func(t *testing.T) {
		job := newTestJob()
		job.start()
		job.cancel()
		job.fail(errors.New("killed, exit status -1"))

		if got := job.State(); got != StateCanceled {
			t.Fatalf("State() = %s, want canceled", got)
		}
		if got := job.Err(); got != nil {
			t.Fatalf("Err() = %v, want nil: a non-zero exit after cancel must not fail the job", got)
		}
	})

	t.Run("setStage starts a pending job", func(t *testing.T) {
		job := newTestJob()
		job.setStage(StageAudio)

		if got := job.State(); got != StateRunning {
			t.Fatalf("State() = %s, want running", got)
		}
		if got := job.Stage(); got != StageAudio {
			t.Fatalf("Stage() = %s, want audio", got)
		}
	})
}

func TestNoopManagerReturnsErrNotImplemented(t *testing.T) {
	t.Parallel()

	var manager Manager = NoopManager{}
	ctx := context.Background()

	if _, err := manager.Probe(ctx, "https://youtu.be/dQw4w9WgXcQ"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Probe() error = %v, want ErrNotImplemented", err)
	}
	if _, _, err := manager.Start(Request{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Start() error = %v, want ErrNotImplemented", err)
	}
	if err := manager.Cancel("job-1"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Cancel() error = %v, want ErrNotImplemented", err)
	}
	if jobs := manager.List(); jobs != nil {
		t.Errorf("List() = %v, want nil", jobs)
	}
}

// TestNoopManagerProbeReturnsZeroMediaInfo pins the shape surfaces can rely on
// while M1 is unwritten: no partial MediaInfo, no panics.
func TestNoopManagerProbeReturnsZeroMediaInfo(t *testing.T) {
	t.Parallel()

	info, err := NoopManager{}.Probe(context.Background(), "https://youtu.be/dQw4w9WgXcQ")
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("error = %v, want ErrNotImplemented", err)
	}
	if info.Title != "" || info.Source != 0 || len(info.Formats) != 0 {
		t.Fatalf("Probe() = %+v, want zero media.MediaInfo", info)
	}
	if got := info.VideoFormats(false); len(got) != 0 {
		t.Fatalf("VideoFormats() on zero MediaInfo = %d rows, want 0", len(got))
	}
}
