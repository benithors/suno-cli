// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.
//
// pp:data-source live
//
// `generate` — create a custom song from lyrics. POST /api/generate/v2-web/
// with create_mode "custom". Captcha-gated; accepts a fresh token via flag,
// stdin, or command when Suno challenges the request.

package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newSunoGenerateCreateCmd(flags *rootFlags) *cobra.Command {
	var (
		title          string
		tags           string
		exclude        string
		lyrics         string
		lyricsFile     string
		model          string
		vocal          string
		weirdness      int
		styleInfluence int
		instrumental   bool
		persona        string
		token          string
		tokenSource    captchaTokenSourceFlags
		noCaptcha      bool
		wait           bool
		downloadDir    string
		workspace      string
		variation      string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Generate a custom song from lyrics (captcha-gated)",
		Long: `Generate a custom Suno song from your own lyrics.

Suno's generation endpoint is protected by hCaptcha. Most requests are attempted
without a token, but challenged requests need a fresh token, --wait-for-gate, or
--queue-on-captcha. This command will never launch a browser or solver.

Provide lyrics inline with --lyrics or from a file with --lyrics-file. For a
description-driven (non-custom) generation, use the 'generate describe' command instead.`,
		Example: `  suno-pp-cli generate create --title "Night Drive" --tags "synthwave" --lyrics "Neon lights..." --token <hc>
  printf '%s' "$HCAPTCHA_TOKEN" | suno-pp-cli generate create --title X --lyrics "la la" --token-stdin
  suno-pp-cli generate create --title X --lyrics-file song.txt --model v5 --vocal female --token <hc>
  suno-pp-cli generate create --title X --lyrics "la la" --no-captcha --wait --download ./out`,
		Annotations: map[string]string{"pp:method": "POST", "pp:path": "/api/generate/v2-web/"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}

			// Dry-run short-circuit BEFORE any filesystem read or validation so
			// `generate --dry-run` (incl. with --lyrics-file) is verify-safe.
			if dryRunOK(flags) {
				// Under --json emit a valid JSON dry-run marker (the gate checks
				// combined stdout+stderr for JSON fidelity); in human mode keep
				// the hint on stderr so stdout stays clean.
				if flags.asJSON {
					return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"dry_run": true, "action": "generate"}, flags)
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "would generate a song (dry run)")
				return nil
			}

			resolvedLyrics := lyrics
			if lyricsFile != "" {
				// #nosec G304 -- lyricsFile is the path the CLI user explicitly passed via --lyrics-file; reading it is the command's purpose.
				data, err := os.ReadFile(lyricsFile)
				if err != nil {
					return usageErr(fmt.Errorf("reading --lyrics-file: %w", err))
				}
				resolvedLyrics = string(data)
			}
			if strings.TrimSpace(resolvedLyrics) == "" {
				return usageErr(fmt.Errorf("lyrics are required: pass --lyrics or --lyrics-file (or use the 'describe' command for description-driven generation)"))
			}

			mv, err := resolveModel(model, sunoGenerateModels, sunoGenerateModelOrder)
			if err != nil {
				return err
			}
			if vocal != "" && vocalTag(vocal) == "" {
				return usageErr(fmt.Errorf("--vocal must be male or female (got %q)", vocal))
			}

			// Captcha gate.
			// Optimistic captcha: attempt without a token; runGenerationFlow
			// surfaces captchaRequiredError only if Suno actually challenges it.
			_ = noCaptcha

			finalTags := tags
			if v := vocalTag(vocal); v != "" {
				finalTags = appendTag(finalTags, v)
			}
			_ = exclude // negative_tags is always "" per upstream contract; --exclude is accepted but folded out.

			variationVal, verr := variationPtr(variation)
			if verr != nil {
				return usageErr(verr)
			}
			captchaToken, terr := resolveCaptchaToken(cmd, token, tokenSource)
			if terr != nil {
				return terr
			}
			tokenProvider, perr := resolveCaptchaTokenProvider(tokenSource.tokenProvider, captchaToken != "")
			if perr != nil {
				return perr
			}

			body := buildGenerateBody(generateInput{
				createMode:     "custom",
				mv:             mv,
				title:          title,
				tags:           finalTags,
				prompt:         resolvedLyrics,
				instrumental:   instrumental,
				personaID:      persona,
				token:          captchaToken,
				tokenProvider:  tokenProvider,
				weirdness:      sliderFraction(weirdness, cmd, "weirdness"),
				styleInfluence: sliderFraction(styleInfluence, cmd, "style-influence"),
				variation:      variationVal,
			})
			return runGenerationFlow(cmd, flags, body, wait, downloadDir, workspace)
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Song title")
	cmd.Flags().StringVar(&tags, "tags", "", "Style tags (comma-separated, e.g. \"synthwave, melodic\")")
	cmd.Flags().StringVar(&exclude, "exclude", "", "Styles to exclude (accepted for compatibility; negative_tags is sent empty)")
	cmd.Flags().StringVar(&lyrics, "lyrics", "", "Lyrics for the song")
	cmd.Flags().StringVar(&lyricsFile, "lyrics-file", "", "Read lyrics from this file")
	cmd.Flags().StringVar(&model, "model", defaultGenerateModel, "Model: v5.5, v5, v4.5+, v4.5, v4, v3.5, v3, v2")
	cmd.Flags().StringVar(&vocal, "vocal", "", "Vocal preference: male or female (appended to tags)")
	cmd.Flags().IntVar(&weirdness, "weirdness", 0, "Weirdness/creativity 0-100 (control_sliders.weirdness_constraint)")
	cmd.Flags().IntVar(&styleInfluence, "style-influence", 0, "Style influence 0-100 (control_sliders.style_weight)")
	cmd.Flags().BoolVar(&instrumental, "instrumental", false, "Generate an instrumental (no vocals)")
	cmd.Flags().StringVar(&persona, "persona", "", "Persona ID to apply")
	cmd.Flags().StringVar(&token, "token", "", "Fresh hCaptcha token to attach to the request")
	addCaptchaTokenSourceFlags(cmd, &tokenSource)
	cmd.Flags().BoolVar(&noCaptcha, "no-captcha", false, "Attempt without an hCaptcha token")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace (project) ID to add the generated clip(s) to")
	cmd.Flags().StringVar(&workspace, "project", "", "Alias for --workspace (Suno 'project' ID)")
	cmd.Flags().StringVar(&variation, "variation", "", "Advanced variation preset: high, normal, or subtle (best-effort)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until generation completes")
	cmd.Flags().StringVar(&downloadDir, "download", "", "Download finished clips to this directory (implies --wait)")
	return cmd
}

// sliderFraction converts a 0-100 slider flag to a 0..1 fraction pointer,
// returning nil when the flag was not explicitly set (so control_sliders
// stays nil unless the user opted in). Out-of-range values are clamped.
func sliderFraction(value int, cmd *cobra.Command, flagName string) *float64 {
	if !cmd.Flags().Changed(flagName) {
		return nil
	}
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	f := float64(value) / 100.0
	return &f
}
