package tui

import (
	"errors"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/nnivxix/utub/internal/config"
)

// settingsModel wraps a huh form bound to a working copy of the config. Nothing
// is written until the form completes (enter on the last field) or ctrl+s.
type settingsModel struct {
	keys keyMap
	form *huh.Form

	cfg       config.Config // working copy; saved only on success
	dir       string        // bound to the directory input
	fragments string        // bound to the fragment-count input

	width  int
	height int

	notice  string
	errText string
}

func newSettings(keys keyMap, cfg config.Config) settingsModel {
	model := settingsModel{
		keys:      keys,
		cfg:       cfg,
		dir:       cfg.DownloadDir,
		fragments: strconv.Itoa(cfg.ConcurrentFragments),
	}
	model.build()
	return model
}

// build constructs the form once, binding fields to the model's own storage so
// values are readable without asking huh for them.
func (s *settingsModel) build() {
	s.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("dir").
				Title("Download directory").
				Description("~ is expanded; the folder is created if missing").
				Value(&s.dir).
				Validate(func(value string) error {
					if strings.TrimSpace(value) == "" {
						return errors.New("directory must not be empty")
					}
					return nil
				}),

			huh.NewSelect[string]().
				Key("codec").
				Title("Video codec").
				Options(
					huh.NewOption("H.264 only — plays everywhere (max 1080p)", config.CodecH264),
					huh.NewOption("Any codec — adds VP9/AV1 up to 4K", config.CodecAny),
				).
				Value(&s.cfg.VideoCodec),

			huh.NewInput().
				Key("fragments").
				Title("Concurrent fragments").
				Description("DASH fragments fetched in parallel (1-64)").
				Value(&s.fragments).
				Validate(func(value string) error {
					n, err := strconv.Atoi(strings.TrimSpace(value))
					if err != nil {
						return errors.New("must be a number")
					}
					if n < 1 || n > 64 {
						return errors.New("must be between 1 and 64")
					}
					return nil
				}),

			huh.NewSelect[string]().
				Key("downloader").
				Title("Downloader").
				Options(
					huh.NewOption("Auto — aria2c when installed", config.DownloaderAuto),
					huh.NewOption("Built-in yt-dlp downloader", config.DownloaderBuiltin),
					huh.NewOption("aria2c only", config.DownloaderAria2c),
				).
				Value(&s.cfg.Downloader),
		),
	).WithWidth(s.formWidth())
}

func (s settingsModel) Init() tea.Cmd {
	if s.form == nil {
		return nil
	}
	return s.form.Init()
}

func (s settingsModel) Update(msg tea.Msg) (settingsModel, tea.Cmd) {
	// The router forwards window sizes to every screen, including the ones
	// that have never been opened. Without a form there is nothing to lay out.
	if s.form == nil {
		return s, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		s.form.WithWidth(s.formWidth())
		return s, nil

	case tea.KeyPressMsg:
		// ctrl+s saves from anywhere in the form, skipping the final enter.
		if key.Matches(msg, s.keys.Save) {
			return s.save()
		}
	}

	model, cmd := s.form.Update(msg)
	if form, ok := model.(*huh.Form); ok {
		s.form = form
	}

	if s.form.State == huh.StateCompleted {
		return s.save()
	}
	return s, cmd
}

// save validates the working copy, proves the directory is writable, then
// writes config.toml atomically. A failure leaves the form on screen with the
// reason, so nothing is half-applied.
func (s settingsModel) save() (settingsModel, tea.Cmd) {
	cfg := s.cfg
	cfg.DownloadDir = strings.TrimSpace(s.dir)

	fragments, err := strconv.Atoi(strings.TrimSpace(s.fragments))
	if err != nil {
		s.errText = "concurrent fragments must be a number"
		return s, nil
	}
	cfg.ConcurrentFragments = fragments

	if err := cfg.Validate(); err != nil {
		s.errText = strings.TrimPrefix(err.Error(), "config: ")
		return s, nil
	}

	dir, err := cfg.EnsureDownloadDir()
	if err != nil {
		s.errText = strings.TrimPrefix(err.Error(), "config: ")
		return s, nil
	}
	if err := cfg.Save(); err != nil {
		s.errText = strings.TrimPrefix(err.Error(), "config: ")
		return s, nil
	}

	s.cfg = cfg
	s.errText = ""
	s.notice = "saved"

	return s, func() tea.Msg { return configSavedMsg{cfg: cfg, dir: dir} }
}

func (s settingsModel) View() string {
	if s.form == nil {
		return ""
	}

	var b strings.Builder

	b.WriteString(styleHeading.Render("Settings"))
	b.WriteString("\n\n")
	b.WriteString(s.form.View())
	b.WriteString("\n")

	switch {
	case s.errText != "":
		b.WriteString(styleDanger.Render("✗ " + s.errText))
		b.WriteString("\n")
	case s.notice != "":
		b.WriteString(styleGood.Render("✓ "+s.notice) + " " + styleMuted.Render(shortenHome(config.Path())))
		b.WriteString("\n")
	default:
		b.WriteString(styleMuted.Render("writes to " + shortenHome(config.Path())))
		b.WriteString("\n")
	}

	return b.String()
}

func (s settingsModel) formWidth() int {
	width := s.width
	if width <= 0 {
		width = 80
	}
	return min(max(width-8, 40), 76)
}
