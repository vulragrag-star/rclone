package pikpak

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenHTTPError checks that a non-2xx download response is retried and
// returned as an error rather than as the file contents.
func TestOpenHTTPError(t *testing.T) {
	const content = "file contents"
	const errorPage = "<html><body>502 Bad Gateway</body></html>"
	var calls, failures atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= failures.Load() {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, errorPage)
			return
		}
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	ctx, ci := fs.AddConfig(context.Background())
	ci.LowLevelRetries = 2
	f := &Fs{
		client: srv.Client(),
		pacer:  fs.NewPacer(ctx, pacer.NewDefault(pacer.MinSleep(minSleep), pacer.MaxSleep(minSleep))),
	}
	o := &Object{fs: f, size: int64(len(content))}

	t.Run("retried", func(t *testing.T) {
		calls.Store(0)
		failures.Store(1)
		in, err := o.open(ctx, srv.URL)
		require.NoError(t, err)
		defer func() { _ = in.Close() }()
		got, err := io.ReadAll(in)
		require.NoError(t, err)
		assert.Equal(t, content, string(got))
		assert.Equal(t, int32(2), calls.Load())
	})

	t.Run("persistent", func(t *testing.T) {
		calls.Store(0)
		failures.Store(100)
		in, err := o.open(ctx, srv.URL)
		if err == nil {
			got, _ := io.ReadAll(in)
			_ = in.Close()
			t.Fatalf("expected an error, got body %q", got)
		}
		assert.Contains(t, err.Error(), "502")
		assert.Equal(t, int32(2), calls.Load())
	})
}
