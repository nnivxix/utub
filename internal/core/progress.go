package core

// TODO(M1): progress — parse the machine-readable progress stream.
//
// The engine passes a --progress-template so it never has to scrape the human
// "[download]  43.2% of 62.4MiB at 6.2MiB/s ETA 00:48" line:
//
//	download:DL %(progress.downloaded_bytes)s %(progress.total_bytes)s \
//	  %(progress.total_bytes_estimate)s %(progress.speed)s %(progress.eta)s \
//	  %(progress.fragment_index)s %(progress.fragment_count)s
//
// Notes for the implementation:
//   - fields can be "NA" or empty; a missing total is not an error, it means
//     Percent stays PercentUnknown until ffmpeg reports progress.
//   - fragment_index/fragment_count give progress for formats whose total byte
//     size yt-dlp cannot predict.
//   - ticks are coalesced to ~10 Hz before they become Events.
//   - keep a regex fallback for the legacy human line in case a future yt-dlp
//     renames the template fields.
