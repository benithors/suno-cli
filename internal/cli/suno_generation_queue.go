// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/benithors/suno-cli-only/internal/store"
	"github.com/spf13/cobra"
)

const (
	generationQueueResource = "generation_queue"

	generationQueueStatusBlocked   = "blocked_by_hcaptcha"
	generationQueueStatusRetrying  = "retrying"
	generationQueueStatusSubmitted = "submitted"
	generationQueueStatusFailed    = "failed"
)

// generationQueueItem stores a local request that hit Suno's adaptive hCaptcha
// gate. Body.Token is always scrubbed before persistence; a retry may attach a
// token to the in-memory copy for exactly one submit.
type generationQueueItem struct {
	ID            string            `json:"id"`
	Status        string            `json:"status"`
	SourceCommand string            `json:"source_command"`
	Title         string            `json:"title,omitempty"`
	Tags          string            `json:"tags,omitempty"`
	Model         string            `json:"mv,omitempty"`
	CreateMode    string            `json:"create_mode,omitempty"`
	PromptPreview string            `json:"prompt_preview,omitempty"`
	Body          sunoGenerateBody  `json:"body"`
	WorkspaceID   string            `json:"workspace_id,omitempty"`
	Wait          bool              `json:"wait,omitempty"`
	DownloadDir   string            `json:"download_dir,omitempty"`
	Attempts      int               `json:"attempts"`
	LastError     string            `json:"last_error,omitempty"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
	BlockedAt     string            `json:"blocked_at,omitempty"`
	LastTriedAt   string            `json:"last_tried_at,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type generationQueueSummary struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	SourceCommand string `json:"source_command"`
	Title         string `json:"title,omitempty"`
	Tags          string `json:"tags,omitempty"`
	Model         string `json:"mv,omitempty"`
	CreateMode    string `json:"create_mode,omitempty"`
	PromptPreview string `json:"prompt_preview,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	Wait          bool   `json:"wait,omitempty"`
	DownloadDir   string `json:"download_dir,omitempty"`
	Attempts      int    `json:"attempts"`
	LastError     string `json:"last_error,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	BlockedAt     string `json:"blocked_at,omitempty"`
	LastTriedAt   string `json:"last_tried_at,omitempty"`
}

func newSunoGenerationQueueCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "queue",
		Short:       "Manage local generation requests blocked by hCaptcha",
		RunE:        parentNoSubcommandRunE(flags),
		Annotations: map[string]string{"pp:data-source": "local"},
	}
	cmd.AddCommand(newSunoGenerationQueueListCmd(flags))
	cmd.AddCommand(newSunoGenerationQueueGetCmd(flags))
	cmd.AddCommand(newSunoGenerationQueueRetryCmd(flags))
	cmd.AddCommand(newSunoGenerationQueueDeleteCmd(flags))
	return cmd
}

func newSunoGenerationQueueListCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var status string
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List locally queued captcha-blocked generations",
		Example:     "  suno-pp-cli generate queue list\n  suno-pp-cli generate queue list --status blocked_by_hcaptcha --json",
		Annotations: map[string]string{"pp:data-source": "local", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := commandContext(cmd)
			s, err := openExistingStore(ctx)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			if s == nil {
				return printJSONFiltered(cmd.OutOrStdout(), []generationQueueSummary{}, flags)
			}
			defer s.Close()
			items, err := listGenerationQueueItems(ctx, s, status, limit)
			if err != nil {
				return err
			}
			return printJSONFiltered(cmd.OutOrStdout(), items, flags)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum queued generations to show")
	cmd.Flags().StringVar(&status, "status", "", "Filter by queue status")
	return cmd
}

func newSunoGenerationQueueGetCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:         "get <queue_id>",
		Short:       "Show one queued generation request",
		Example:     "  suno-pp-cli generate queue get gq_...",
		Annotations: map[string]string{"pp:data-source": "local", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			item, err := loadGenerationQueueItem(commandContext(cmd), args[0])
			if errors.Is(err, sql.ErrNoRows) {
				return notFoundErr(fmt.Errorf("queued generation %q not found", args[0]))
			}
			if err != nil {
				return err
			}
			return printJSONFiltered(cmd.OutOrStdout(), item, flags)
		},
	}
}

func newSunoGenerationQueueRetryCmd(flags *rootFlags) *cobra.Command {
	var token string
	var tokenSource captchaTokenSourceFlags
	var wait bool
	var downloadDir string
	cmd := &cobra.Command{
		Use:   "retry <queue_id>",
		Short: "Retry one queued generation request",
		Long: `Retry a generation request that was saved after Suno returned the hCaptcha gate.

If you provide --token, --token-stdin, or --token-command, the token is attached
only to this retry and is never saved. If no token source is provided, retry
submits without a token; this can work when Suno's adaptive gate has cooled down.`,
		Example: "  suno-pp-cli generate queue retry gq_...\n  printf '%s' \"$HCAPTCHA_TOKEN\" | suno-pp-cli generate queue retry gq_... --token-stdin --wait --download ./out",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			ctx := commandContext(cmd)
			s, err := openExistingStore(ctx)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			if s == nil {
				return notFoundErr(fmt.Errorf("queued generation %q not found", args[0]))
			}
			defer s.Close()
			item, err := loadGenerationQueueItemFromStore(s, args[0])
			if errors.Is(err, sql.ErrNoRows) {
				return notFoundErr(fmt.Errorf("queued generation %q not found", args[0]))
			}
			if err != nil {
				return err
			}

			if dryRunOK(flags) {
				item.Body.Token = nil
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
					"dry_run":  true,
					"queue_id": item.ID,
					"body":     item.Body,
				}, flags)
			}

			body := item.Body
			captchaToken, terr := resolveCaptchaToken(cmd, token, tokenSource)
			if terr != nil {
				return terr
			}
			tokenProvider, perr := resolveCaptchaTokenProvider(tokenSource.tokenProvider, captchaToken != "")
			if perr != nil {
				return perr
			}
			body.Token = strPtr(captchaToken)
			body.TokenProvider = tokenProvider
			refreshGenerationRequestIDs(&body)

			retryWait := item.Wait || wait
			retryDownloadDir := item.DownloadDir
			if cmd.Flags().Changed("download") {
				retryDownloadDir = downloadDir
			}

			now := time.Now().UTC().Format(time.RFC3339)
			item.Attempts++
			item.Status = generationQueueStatusRetrying
			item.LastTriedAt = now
			item.UpdatedAt = now
			item.LastError = ""
			if err := saveGenerationQueueItem(s, item); err != nil {
				return fmt.Errorf("marking queued generation %q retrying: %w", item.ID, err)
			}

			err = runGenerationFlow(cmd, flags, body, retryWait, retryDownloadDir, item.WorkspaceID)
			now = time.Now().UTC().Format(time.RFC3339)
			item.UpdatedAt = now
			if err != nil {
				if isCaptchaGateResult(err) {
					item.Status = generationQueueStatusBlocked
					item.BlockedAt = now
				} else {
					item.Status = generationQueueStatusFailed
				}
				item.LastError = safeQueueError(err)
				if serr := saveGenerationQueueItem(s, item); serr != nil {
					return fmt.Errorf("%w\nalso failed to update queue item %q: %v", err, item.ID, serr)
				}
				return err
			}

			item.Status = generationQueueStatusSubmitted
			item.LastError = ""
			if err := saveGenerationQueueItem(s, item); err != nil {
				return fmt.Errorf("marking queued generation %q submitted: %w", item.ID, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "hCaptcha token for this retry only; never saved")
	addCaptchaTokenSourceFlags(cmd, &tokenSource)
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until generation completes")
	cmd.Flags().StringVar(&downloadDir, "download", "", "Download finished clips to this directory (implies --wait)")
	return cmd
}

func newSunoGenerationQueueDeleteCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <queue_id>",
		Short:   "Delete one queued generation request",
		Example: "  suno-pp-cli generate queue delete gq_...",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			ctx := commandContext(cmd)
			s, err := openExistingStore(ctx)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			if s == nil {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"deleted": false, "id": args[0]}, flags)
			}
			defer s.Close()
			res, err := s.DB().ExecContext(ctx, `DELETE FROM resources WHERE resource_type=? AND id=?`, generationQueueResource, args[0])
			if err != nil {
				return fmt.Errorf("deleting queued generation %q: %w", args[0], err)
			}
			n, _ := res.RowsAffected()
			return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"deleted": n > 0, "id": args[0]}, flags)
		},
	}
}

func queueGenerationOnCaptcha(cmd *cobra.Command, body sunoGenerateBody, wait bool, downloadDir, workspaceID string, cause error) (*generationQueueItem, error) {
	ctx := commandContext(cmd)
	s, err := openDefaultStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening local database: %w", err)
	}
	defer s.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	item := &generationQueueItem{
		ID:            newGenerationQueueID(),
		Status:        generationQueueStatusBlocked,
		SourceCommand: commandPath(cmd),
		Title:         ptrString(body.Title),
		Tags:          ptrString(body.Tags),
		Model:         body.Mv,
		CreateMode:    body.Metadata.CreateMode,
		PromptPreview: queuePreview(body.Prompt, 160),
		Body:          body,
		WorkspaceID:   workspaceID,
		Wait:          wait,
		DownloadDir:   downloadDir,
		LastError:     safeQueueError(cause),
		CreatedAt:     now,
		UpdatedAt:     now,
		BlockedAt:     now,
	}
	if err := saveGenerationQueueItem(s, item); err != nil {
		return nil, err
	}
	return item, nil
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd == nil {
		return context.Background()
	}
	ctx := cmd.Context()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func commandPath(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	return cmd.CommandPath()
}

func loadGenerationQueueItem(ctx context.Context, id string) (*generationQueueItem, error) {
	s, err := openExistingStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening local database: %w", err)
	}
	if s == nil {
		return nil, sql.ErrNoRows
	}
	defer s.Close()
	return loadGenerationQueueItemFromStore(s, id)
}

func loadGenerationQueueItemFromStore(s *store.Store, id string) (*generationQueueItem, error) {
	raw, err := s.Get(generationQueueResource, id)
	if err != nil {
		return nil, err
	}
	var item generationQueueItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("parsing queued generation %q: %w", id, err)
	}
	item.Body.Token = nil
	return &item, nil
}

func listGenerationQueueItems(ctx context.Context, s *store.Store, status string, limit int) ([]generationQueueSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	raws, err := s.List(generationQueueResource, 1000)
	if err != nil {
		return nil, fmt.Errorf("listing queued generations: %w", err)
	}
	wantStatus := strings.TrimSpace(status)
	items := []generationQueueSummary{}
	for _, raw := range raws {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		var item generationQueueItem
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		item.Body.Token = nil
		if wantStatus != "" && item.Status != wantStatus {
			continue
		}
		items = append(items, generationQueueSummary{
			ID:            item.ID,
			Status:        item.Status,
			SourceCommand: item.SourceCommand,
			Title:         item.Title,
			Tags:          item.Tags,
			Model:         item.Model,
			CreateMode:    item.CreateMode,
			PromptPreview: item.PromptPreview,
			WorkspaceID:   item.WorkspaceID,
			Wait:          item.Wait,
			DownloadDir:   item.DownloadDir,
			Attempts:      item.Attempts,
			LastError:     item.LastError,
			CreatedAt:     item.CreatedAt,
			UpdatedAt:     item.UpdatedAt,
			BlockedAt:     item.BlockedAt,
			LastTriedAt:   item.LastTriedAt,
		})
		if limit > 0 && len(items) >= limit {
			break
		}
	}
	return items, nil
}

func saveGenerationQueueItem(s *store.Store, item *generationQueueItem) error {
	item.Body.Token = nil
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encoding queued generation %q: %w", item.ID, err)
	}
	if err := s.Upsert(generationQueueResource, item.ID, data); err != nil {
		return fmt.Errorf("saving queued generation %q: %w", item.ID, err)
	}
	return nil
}

func newGenerationQueueID() string {
	return "gq_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func refreshGenerationRequestIDs(body *sunoGenerateBody) {
	body.Metadata.CreateSessionToken = uuid.NewString()
	body.TransactionUUID = uuid.NewString()
}

func isCaptchaGateResult(err error) bool {
	if isCaptchaRequired(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "hcaptcha token") || strings.Contains(msg, "captcha_required")
}

func safeQueueError(err error) string {
	if err == nil {
		return ""
	}
	return queuePreview(err.Error(), 500)
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func queuePreview(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if maxRunes <= 0 || s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
