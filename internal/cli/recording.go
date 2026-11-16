package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"uis-cli/internal/portal"
)

const recordingSource = "private.call_details_data+private.report_constructor.call_records"

func newRecording(g *globalFlags) *cobra.Command {
	root := &cobra.Command{Use: "recording", Short: "Call recordings (private cabinet, read-only)"}
	dl := &cobra.Command{
		Use:   "download <communication_id>",
		Short: "Download a call recording to an explicit path",
		Long: "Resolves the call through private call details (header ID must equal the requested ID), then writes\n" +
			"recording bytes to --output. Several legs with --leg all are written as <name>.legN<ext>.\n" +
			"Prints metadata only (path, bytes, sha256, content type); never the recording locator.\n" +
			"Existing files are not overwritten without --force. No transcription is requested.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := positive(args[0])
			if err != nil {
				return err
			}
			output, _ := cmd.Flags().GetString("output")
			legFlag, _ := cmd.Flags().GetString("leg")
			leg := 0 // 0 = all
			if legFlag != "all" {
				n, e := strconv.Atoi(legFlag)
				if e != nil || n < 1 {
					return exitf(2, "--leg must be all or a positive leg number")
				}
				leg = n
			}
			if output == "" {
				return exitf(2, "--output is required")
			}
			if st, e := os.Stat(filepath.Dir(output)); e != nil || !st.IsDir() {
				return exitf(2, "Output directory does not exist")
			}
			if st, e := os.Stat(output); e == nil && st.IsDir() {
				return exitf(2, "--output must be a file path, not a directory")
			}
			if g.DryRun {
				return emit(g, map[string]interface{}{"communication_id": string(id), "output": output, "leg": legFlag, "source": recordingSource, "network": false})
			}
			var rec portal.CallRecordings
			var client *portal.Client
			if e := privateRead(cmd.Context(), g, func(c *portal.Client) error {
				var re error
				rec, re = c.ResolveRecordings(cmd.Context(), string(id))
				client = c
				return re
			}); e != nil {
				var ee *ExitError
				if errors.As(e, &ee) && ee.Code == 5 && ee.Details["stage"] == "recording_report_row" {
					return notFound("Call "+string(id)+" was not found in the private report around its start time", map[string]interface{}{"reason": "call_not_in_report", "stage": "recording_report_row"})
				}
				return e
			}
			if rec.Legs() == 0 {
				return notFound("Call "+string(id)+" has no downloadable recording (not recorded, not answered, or past retention)", map[string]interface{}{"reason": "no_recording", "communication_id": string(id), "detail_recording_files": rec.DetailFiles})
			}
			legs := []int{}
			if leg == 0 {
				for i := 1; i <= rec.Legs(); i++ {
					legs = append(legs, i)
				}
			} else if leg > rec.Legs() {
				return notFound("Recording leg "+legFlag+" does not exist; call has "+strconv.Itoa(rec.Legs()), map[string]interface{}{"reason": "leg_not_found", "legs_available": rec.Legs()})
			} else {
				legs = []int{leg}
			}
			paths := map[int]string{}
			for _, n := range legs {
				paths[n] = output
				if len(legs) > 1 {
					paths[n] = legPath(output, n)
				}
				if _, e := os.Lstat(paths[n]); e == nil && !g.Force {
					return outputExists(paths[n])
				}
			}
			// Download every leg to private temp files first so a failure leaves
			// no partial set behind; then publish them under the requested names.
			temps := map[int]string{}
			defer func() {
				for _, t := range temps {
					os.Remove(t)
				}
			}()
			files := []map[string]interface{}{}
			for _, n := range legs {
				f, e := os.CreateTemp(filepath.Dir(paths[n]), ".uis-recording-*")
				if e != nil {
					return exitf(1, "Cannot create output file")
				}
				temps[n] = f.Name()
				h := sha256.New()
				ct, size, e := client.DownloadRecording(cmd.Context(), rec, n-1, io.MultiWriter(f, h))
				if ce := f.Close(); e == nil && ce != nil {
					e = exitf(1, "Cannot write output file")
				}
				if e != nil {
					return operationError(e)
				}
				files = append(files, map[string]interface{}{"leg": n, "path": paths[n], "bytes": size, "sha256": hex.EncodeToString(h.Sum(nil)), "content_type": ct})
			}
			for _, n := range legs {
				if g.Force {
					if e := os.Rename(temps[n], paths[n]); e != nil {
						return exitf(1, "Cannot write output file")
					}
				} else if e := os.Link(temps[n], paths[n]); e != nil {
					if os.IsExist(e) {
						return outputExists(paths[n])
					}
					return exitf(1, "Cannot write output file")
				}
				os.Remove(temps[n])
				delete(temps, n)
			}
			return emit(g, map[string]interface{}{"communication_id": string(id), "files": files, "legs_available": rec.Legs(), "detail_recording_files": rec.DetailFiles, "source": recordingSource,
				"limitations": []string{"Recording audio is personal data: keep files private and delete them after use"}})
		},
	}
	dl.Flags().StringP("output", "o", "", "Output file path (required; parent directory must exist)")
	dl.Flags().String("leg", "all", "Recording leg: all or a 1-based leg number")
	root.AddCommand(dl)
	return root
}

// legPath inserts .legN before the extension: /tmp/123.mp3 -> /tmp/123.leg2.mp3.
func legPath(p string, n int) string {
	ext := filepath.Ext(p)
	return strings.TrimSuffix(p, ext) + ".leg" + strconv.Itoa(n) + ext
}

// JSON envelopes replace messages with generic ones, so the reason and hint
// travel in details.
func notFound(msg string, details map[string]interface{}) *ExitError {
	details["hint"] = msg
	return &ExitError{Code: 5, Msg: msg, Details: details}
}

func outputExists(path string) *ExitError {
	msg := "Output file exists; pass --force to overwrite"
	return &ExitError{Code: 10, Msg: msg, Details: map[string]interface{}{"reason": "output_exists", "path": path, "hint": msg}}
}
