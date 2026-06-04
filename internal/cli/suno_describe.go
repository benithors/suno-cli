// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.
//
// pp:data-source live
//
// `describe` — description-driven generation. POST /api/generate/v2-web/ with
// create_mode "simple" and gpt_description_prompt = the natural-language
// description, matching the current Suno web app simple-mode body.
// Captcha-gated like generate.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newSunoDescribeCmd(flags *rootFlags) *cobra.Command {
	var (
		prompt       string
		title        string
		tags         string
		model        string
		instrumental bool
		token        string
		tokenSource  captchaTokenSourceFlags
		noCaptcha    bool
		wait         bool
		downloadDir  string
		workspace    string
		variation    string
	)

	cmd := &cobra.Command{
		Use:   "describe [description]",
		Short: "Generate a song from a natural-language description (simple mode, captcha-gated)",
		Long: `Generate a Suno song from a free-text description rather than explicit lyrics.

The description can be given positionally or with --prompt. This uses Suno's
web-app simple create mode; the model writes its own lyrics from your prompt.

Captcha-gated: pass --token <hcaptcha-token>, --token-stdin, --token-command,
or --no-captcha. This command never launches a browser or solver.`,
		Example: `  suno-pp-cli generate describe "an upbeat lo-fi track about rainy mornings" --token <hc>
  printf '%s' "$HCAPTCHA_TOKEN" | suno-pp-cli generate describe "epic orchestral battle theme" --token-stdin
  suno-pp-cli generate describe --prompt "epic orchestral battle theme" --model v5 --no-captcha`,
		Annotations: map[string]string{"pp:method": "POST", "pp:path": "/api/generate/v2-web/"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}

			description := prompt
			if description == "" && len(args) > 0 {
				description = strings.Join(args, " ")
			}
			if strings.TrimSpace(description) == "" {
				return usageErr(fmt.Errorf("a description is required (positional arg or --prompt)"))
			}

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

			variationVal, verr := variationPtr(variation)
			if verr != nil {
				return usageErr(verr)
			}
			simplePrompt := description
			if strings.TrimSpace(tags) != "" {
				simplePrompt = appendTag(simplePrompt, tags)
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
				createMode:           "simple",
				mv:                   mv,
				title:                title,
				tags:                 tags,
				prompt:               "",
				gptDescriptionPrompt: simplePrompt,
				instrumental:         instrumental,
				token:                captchaToken,
				tokenProvider:        tokenProvider,
				variation:            variationVal,
			})
			return runGenerationFlow(cmd, flags, body, wait, downloadDir, workspace)
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Description of the song to generate")
	cmd.Flags().StringVar(&title, "title", "", "Song title")
	cmd.Flags().StringVar(&tags, "tags", "", "Style tags (comma-separated)")
	cmd.Flags().StringVar(&model, "model", defaultGenerateModel, "Model: v5.5, v5, v4.5+, v4.5, v4, v3.5, v3, v2")
	cmd.Flags().BoolVar(&instrumental, "instrumental", false, "Generate an instrumental (no vocals)")
	cmd.Flags().StringVar(&token, "token", "", "Fresh hCaptcha token to attach to the request")
	addCaptchaTokenSourceFlags(cmd, &tokenSource)
	cmd.Flags().BoolVar(&noCaptcha, "no-captcha", false, "Attempt without an hCaptcha token")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace (project) ID to add the generated clip(s) to")
	cmd.Flags().StringVar(&variation, "variation", "", "Advanced variation preset: high, normal, or subtle (best-effort)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until generation completes")
	cmd.Flags().StringVar(&downloadDir, "download", "", "Download finished clips to this directory (implies --wait)")
	return cmd
}
