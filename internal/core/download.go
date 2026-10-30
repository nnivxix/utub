package core

// TODO(M1): download — build the argv and own the subprocess.
//
// Contract, fixed by docs/architecture.md §7:
//
//	yt-dlp -f <selector>
//	  -P home:<OutDir> -P temp:<OutDir>/.utub-tmp
//	  -o <Request.Config.FilenameTemplate>
//	  --newline --no-colors
//	  --progress-template "download:DL %(progress.downloaded_bytes)s ..."
//	  --concurrent-fragments 16
//	  --http-chunk-size 10M
//	  --retries 10 --fragment-retries 10 --continue
//	  --merge-output-format mp4
//	  <url>
//
// Notes for the implementation:
//   - --concurrent-fragments is the "parallel chunked" win: 1080p+ formats are
//     DASH and fetch N fragments at once.
//   - downloader=auto upgrades to aria2c (-x16 -s16 -k1M -j16) when it is on
//     PATH, and silently falls back to the builtin downloader when it is not.
//   - cancellation kills the whole process group (Setpgid), otherwise ffmpeg
//     and aria2c children survive the TUI exiting.
//   - .part files are left behind on purpose so --continue resumes.
//   - the final path is discovered from yt-dlp output, not guessed, so
//     post-processor renames (extraction, remux) are accounted for.
