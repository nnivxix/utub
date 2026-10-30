package core

// TODO(M1): probe — resolve a URL into metadata and formats.
//
// Contract, fixed by docs/architecture.md §6:
//
//	yt-dlp -J --no-playlist --no-warnings --no-progress <url>
//
// One JSON object on stdout, unmarshalled into media.MediaInfo. The URL has
// already passed media URL validation; probe does not re-validate it.
//
// Notes for the implementation:
//   - -J is slow (2-4 s); the caller owns the context and cancels on esc.
//   - stdout carries the JSON, stderr carries warnings that must not pollute it.
//   - a playlist dump returns {"_type":"playlist"} instead of a video; treat
//     that as an error rather than guessing which entry was meant.
//   - exit codes and stderr are classified in errors.go, not here.
