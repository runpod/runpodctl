package volume

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/clierr"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/runpod/runpodctl/internal/nvtransfer"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const transferLong = `%s

uses the network volume's s3-compatible api, which needs s3 api keys (separate
from your runpod api key): create them at https://console.runpod.io/user/settings
and set RUNPOD_S3_ACCESS_KEY and RUNPOD_S3_SECRET_KEY, or pass the flags.

the volume's data center is looked up with your runpod api key; pass
--data-center-id to skip the lookup. only data centers with the s3 api are
supported: https://docs.runpod.io/storage/s3-api

transfers resume: if one stops (network drop, ctrl-c, crash), run the same
command again. finished files are skipped, a large upload continues from its
uploaded parts, and a large download continues from its finished chunks.`

var uploadCmd = &cobra.Command{
	Use:   "upload <volume-id> <local-path> [remote-path]",
	Short: "upload a file or folder to a network volume",
	Long: fmt.Sprintf(transferLong, `upload a file or folder to a network volume.

remote-path defaults to the local name. a file uploaded to a remote path ending
in "/" keeps its name inside that folder; a folder's contents land under
remote-path. files whose remote copy has the same size and is not older are
skipped.`),
	Example: `  runpodctl network-volume upload abc123 ./model.safetensors models/
  runpodctl network-volume upload abc123 ./dataset datasets/dataset`,
	Args: cobra.RangeArgs(2, 3),
	RunE: runUpload,
}

var downloadCmd = &cobra.Command{
	Use:   "download <volume-id> <remote-path> [local-path]",
	Short: "download a file or folder from a network volume",
	Long: fmt.Sprintf(transferLong, `download a file or folder from a network volume.

remote-path is a folder when it ends in "/" or has files under it. local-path
defaults to the remote name; a file downloaded into an existing local folder
keeps its name. local files with the remote size that are not older than the
remote copy are skipped. use "/" as remote-path to download the whole volume.`),
	Example: `  runpodctl network-volume download abc123 models/model.safetensors
  runpodctl network-volume download abc123 outputs/ ./outputs`,
	Args: cobra.RangeArgs(2, 3),
	RunE: runDownload,
}

var (
	transferDataCenterID string
	transferAccessKey    string
	transferSecretKey    string
	transferEndpointURL  string
	transferConcurrency  int
)

func init() {
	for _, c := range []*cobra.Command{uploadCmd, downloadCmd} {
		c.Flags().StringVar(&transferDataCenterID, "data-center-id", "", "volume data center id, e.g. EU-RO-1 (default: looked up from the volume)")
		c.Flags().StringVar(&transferAccessKey, "s3-access-key", "", "s3 api access key (default: $RUNPOD_S3_ACCESS_KEY)")
		c.Flags().StringVar(&transferSecretKey, "s3-secret-key", "", "s3 api secret key (default: $RUNPOD_S3_SECRET_KEY)")
		c.Flags().IntVar(&transferConcurrency, "concurrency", 4, "parallel requests (1-64)")
		c.Flags().StringVar(&transferEndpointURL, "endpoint-url", "", "s3 endpoint url override")
		c.Flags().MarkHidden("endpoint-url") //nolint:errcheck
	}
}

// transferResult is printed on success.
type transferResult struct {
	VolumeID     string `json:"volumeId"`
	DataCenterID string `json:"dataCenterId,omitempty"`
	Source       string `json:"source"`
	Destination  string `json:"destination"`
	nvtransfer.Summary
}

// incompleteError is a transfer that stopped partway. Rerunning the same
// command resumes it, which is what the code tells an agent.
type incompleteError struct {
	err     error
	summary *nvtransfer.Summary
}

func (e *incompleteError) Error() string {
	msg := e.err.Error()
	if e.summary != nil && e.summary.FilesTransferred > 0 {
		msg = fmt.Sprintf("%s (%d files finished before the failure)", msg, e.summary.FilesTransferred)
	}
	return msg + "; re-run the same command to resume"
}

func (e *incompleteError) Unwrap() error     { return e.err }
func (e *incompleteError) ErrorCode() string { return "transfer_incomplete" }

// missingS3KeysError reuses no_credentials: the fix is the same kind of
// local setup.
type missingS3KeysError struct{}

func (missingS3KeysError) Error() string {
	return "s3 api keys not found. create them at https://console.runpod.io/user/settings then: export RUNPOD_S3_ACCESS_KEY=... RUNPOD_S3_SECRET_KEY=... (or pass --s3-access-key/--s3-secret-key)"
}
func (missingS3KeysError) ErrorCode() string { return "no_credentials" }

func runUpload(cmd *cobra.Command, args []string) error {
	remote := ""
	if len(args) == 3 {
		remote = args[2]
	}
	return runTransfer(cmd, args[0], args[1], remote, true)
}

func runDownload(cmd *cobra.Command, args []string) error {
	local := ""
	if len(args) == 3 {
		local = args[2]
	}
	return runTransfer(cmd, args[0], args[1], local, false)
}

func runTransfer(cmd *cobra.Command, volumeID, src, dst string, upload bool) error {
	if transferConcurrency < 1 || transferConcurrency > 64 {
		return clierr.Usagef("--concurrency must be between 1 and 64")
	}
	accessKey := firstNonEmpty(transferAccessKey, configenv.S3AccessKey())
	secretKey := firstNonEmpty(transferSecretKey, configenv.S3SecretKey())
	if accessKey == "" || secretKey == "" {
		return missingS3KeysError{}
	}
	if upload {
		if _, err := os.Stat(src); err != nil {
			return clierr.Usagef("local path %s: %v", src, errors.Unwrap(err))
		}
	}

	dataCenterID := transferDataCenterID
	endpoint := transferEndpointURL
	if endpoint == "" {
		if dataCenterID == "" {
			client, err := api.NewClient()
			if err != nil {
				return err
			}
			volume, err := client.GetNetworkVolume(volumeID)
			if err != nil {
				return fmt.Errorf("failed to get volume: %w", err)
			}
			dataCenterID = volume.DataCenterID
		}
		endpoint = nvtransfer.EndpointURL(dataCenterID)
	}
	region := dataCenterID
	if region == "" {
		region = "us-east-1"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	progress := newProgress(os.Stderr)
	opts := nvtransfer.Options{
		Client:      nvtransfer.NewS3Client(endpoint, region, accessKey, secretKey),
		Endpoint:    endpoint,
		VolumeID:    volumeID,
		Concurrency: transferConcurrency,
		Reporter:    progress,
	}

	var summary *nvtransfer.Summary
	var err error
	if upload {
		summary, err = nvtransfer.Upload(ctx, opts, src, dst)
	} else {
		summary, err = nvtransfer.Download(ctx, opts, src, dst)
	}
	progress.stop()
	if err != nil {
		return transferError(ctx, err, summary)
	}

	if dst == "" {
		dst = "(default)"
	}
	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(&transferResult{
		VolumeID:     volumeID,
		DataCenterID: dataCenterID,
		Source:       src,
		Destination:  dst,
		Summary:      *summary,
	}, &output.Config{Format: format})
}

// transferError codes a failed transfer. A missing path or a rejected
// credential is final and rerunning will not help; anything else stopped a
// transfer that a rerun resumes.
func transferError(ctx context.Context, err error, summary *nvtransfer.Summary) error {
	if ctx.Err() != nil {
		return &incompleteError{err: errors.New("interrupted"), summary: summary}
	}
	if errors.Is(err, nvtransfer.ErrInvalidPath) {
		return &clierr.UsageError{Err: err}
	}
	if errors.Is(err, nvtransfer.ErrNotFound) {
		return &codedError{err: err, code: "not_found"}
	}
	switch nvtransfer.HTTPStatus(err) {
	case http.StatusUnauthorized:
		return &codedError{err: err, code: "unauthorized"}
	case http.StatusForbidden:
		return &codedError{err: fmt.Errorf("%w (check the s3 api keys and that they belong to the volume's account)", err), code: "forbidden"}
	}
	return &incompleteError{err: err, summary: summary}
}

type codedError struct {
	err  error
	code string
}

func (e *codedError) Error() string     { return e.err.Error() }
func (e *codedError) Unwrap() error     { return e.err }
func (e *codedError) ErrorCode() string { return e.code }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// progress prints transfer progress to stderr: a line redrawn in place on a
// terminal, otherwise a plain line every 30s so logs stay readable.
type progress struct {
	out      *os.File
	tty      bool
	started  time.Time
	files    atomic.Int64
	total    atomic.Int64
	done     atomic.Int64
	finished atomic.Int64

	once sync.Once
	quit chan struct{}
	wg   sync.WaitGroup
}

func newProgress(out *os.File) *progress {
	return &progress{out: out, tty: term.IsTerminal(int(out.Fd())), quit: make(chan struct{})}
}

func (p *progress) Start(files int, bytes int64) {
	p.files.Store(int64(files))
	p.total.Store(bytes)
	p.started = time.Now()
	if files == 0 {
		return
	}
	interval := 30 * time.Second
	if p.tty {
		interval = 500 * time.Millisecond
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.print()
			case <-p.quit:
				return
			}
		}
	}()
}

func (p *progress) Add(bytes int64) { p.done.Add(bytes) }
func (p *progress) FileDone(string) { p.finished.Add(1) }

func (p *progress) print() {
	done, total := p.done.Load(), p.total.Load()
	pct := 100.0
	if total > 0 {
		pct = float64(done) / float64(total) * 100
	}
	rate := float64(done) / time.Since(p.started).Seconds()
	line := fmt.Sprintf("%s / %s (%.1f%%) at %s/s, files %d/%d",
		humanBytes(float64(done)), humanBytes(float64(total)), pct, humanBytes(rate), p.finished.Load(), p.files.Load())
	if p.tty {
		fmt.Fprintf(p.out, "\r\033[K%s", line)
	} else {
		fmt.Fprintln(p.out, line)
	}
}

func (p *progress) stop() {
	p.once.Do(func() {
		close(p.quit)
		p.wg.Wait()
		if p.files.Load() > 0 {
			p.print()
			if p.tty {
				fmt.Fprintln(p.out)
			}
		}
	})
}

func humanBytes(b float64) string {
	units := []string{"b", "kb", "mb", "gb", "tb"}
	i := 0
	for b >= 1000 && i < len(units)-1 {
		b /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", b, units[i])
	}
	return fmt.Sprintf("%.1f %s", b, units[i])
}
