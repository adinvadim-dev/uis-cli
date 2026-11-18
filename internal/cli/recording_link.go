package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/spf13/cobra"
	"uis-cli/internal/portal"
)

func newRecordingLink(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "link <communication_id>", Short: "Verify and disclose one recording link for an approved recipient",
		Long: "Discloses a bearer link to one recording after a complete credential-free audio download.\nRequires --audience-approved after reviewing content and recipient. Anyone holding the link\ncan access it until UIS removes the recording; exact expiry is unknown. No short TTL or revocation is provided.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := positive(args[0])
			if err != nil {
				return err
			}
			leg, _ := cmd.Flags().GetInt("leg")
			if leg < 1 {
				return exitf(2, "--leg must be a positive leg number")
			}
			approved, _ := cmd.Flags().GetBool("audience-approved")
			if g.DryRun {
				return emit(g, map[string]interface{}{"communication_id": string(id), "leg": leg, "network": false, "disclosure": false})
			}
			if !approved {
				return &ExitError{Code: 10, Msg: "Review recording content and approve the intended recipient before --audience-approved", Details: map[string]interface{}{"reason": "audience_approval_required"}}
			}
			var rec portal.CallRecordings
			var client *portal.Client
			if err := privateRead(cmd.Context(), g, func(c *portal.Client) error {
				var e error
				rec, e = c.ResolveRecordings(cmd.Context(), string(id))
				client = c
				return e
			}); err != nil {
				return err
			}
			if rec.Legs() > 1 && !cmd.Flags().Changed("leg") {
				return exitf(2, "Multiple recording legs: select the reviewed leg explicitly with --leg N")
			}
			if leg > rec.Legs() {
				return notFound("Selected recording is unavailable or past retention", map[string]interface{}{"reason": "no_recording", "legs_available": rec.Legs()})
			}
			hash := sha256.New()
			ct, size, err := client.DownloadRecording(cmd.Context(), rec, leg-1, hash)
			if err != nil {
				return operationError(err)
			}
			return emit(g, map[string]interface{}{
				"communication_id": string(id), "leg": leg, "legs_available": rec.Legs(),
				"url": rec.Link(leg - 1), "bytes": size, "sha256": hex.EncodeToString(hash.Sum(nil)), "content_type": ct,
				"verified_at": time.Now().UTC().Format(time.RFC3339), "expires_at": nil,
				"expiry_policy": "UIS recording retention; exact expiry unknown; may stop working earlier",
				"warning":       "Bearer link: forward only to the approved recipient. No CLI revocation or short TTL. Download before retention ends.",
				"source":        recordingSource,
			})
		},
	}
	cmd.Flags().Int("leg", 1, "One 1-based recording leg (no bulk disclosure)")
	cmd.Flags().Bool("audience-approved", false, "Confirm content and intended recipient were reviewed and approved")
	return cmd
}
