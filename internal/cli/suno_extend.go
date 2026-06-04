// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.
//
// pp:data-source live
//
// `extend` — continue an existing clip. POST /api/generate/v2-web/ with
// create_mode "custom", continue_clip_id set, and continue_at offset.
// Captcha-gated like generate.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSunoExtendCmd(flags *rootFlags) *cobra.Command {
	var (
		at          float64
		lyrics      string
		tags        string
		model       string
		token       string
		tokenSource captchaTokenSourceFlags
		noCaptcha   bool
		wait        bool
		downloadDir string
		workspace   string
	)

	cmd := &cobra.Command{
		Use:   "extend <clip_id>",
		Short: "Extend (continue) an existing clip from a time offset (captcha-gated)",
		Long: `Continue an existing clip, generating new audio that picks up from --at seconds.

Captcha-gated: pass --token <hcaptcha-token>, --token-stdin, --token-command,
or --no-captcha. This command never launches a browser or solver.`,
		Example: `  suno-pp-cli generate extend 550e8400-e29b-41d4-a716-446655440000 --at 120 --token <hc>
  printf '%s' "$HCAPTCHA_TOKEN" | suno-pp-cli generate extend <id> --at 90 --token-stdin
  suno-pp-cli generate extend <id> --at 90 --lyrics "next verse..." --no-captcha`,
		Annotations: map[string]string{"pp:method": "POST", "pp:path": "/api/generate/v2-web/"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if len(args) == 0 {
				return usageErr(fmt.Errorf("a clip_id is required"))
			}
			clipID := args[0]

			mv, err := resolveModel(model, sunoGenerateModels, sunoGenerateModelOrder)
			if err != nil {
				return err
			}

			// Optimistic captcha: attempt without a token; runGenerationFlow
			// surfaces captchaRequiredError only if Suno actually challenges it.
			_ = noCaptcha
			if dryRunOK(flags) {
				return nil
			}

			continueAt := at
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
				tags:           tags,
				prompt:         lyrics,
				token:          captchaToken,
				tokenProvider:  tokenProvider,
				continueClipID: clipID,
				continueAt:     &continueAt,
			})
			return runGenerationFlow(cmd, flags, body, wait, downloadDir, workspace)
		},
	}

	cmd.Flags().Float64Var(&at, "at", 0, "Continue from this time offset in seconds")
	cmd.Flags().StringVar(&lyrics, "lyrics", "", "Lyrics for the continuation")
	cmd.Flags().StringVar(&tags, "tags", "", "Style tags (comma-separated)")
	cmd.Flags().StringVar(&model, "model", defaultGenerateModel, "Model: v5.5, v5, v4.5+, v4.5, v4, v3.5, v3, v2")
	cmd.Flags().StringVar(&token, "token", "", "Fresh hCaptcha token to attach to the request")
	addCaptchaTokenSourceFlags(cmd, &tokenSource)
	cmd.Flags().BoolVar(&noCaptcha, "no-captcha", false, "Attempt without an hCaptcha token")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace (project) ID to add the generated clip(s) to")
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until generation completes")
	cmd.Flags().StringVar(&downloadDir, "download", "", "Download finished clips to this directory (implies --wait)")
	return cmd
}
