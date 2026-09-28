package cloudhypervisor

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVmResizeBalloon(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "ch.sock")
	listener, err := net.Listen("unix", socketPath)
	assert.NoError(t, err)

	var method, path, body string
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	})}
	go server.Serve(listener)
	defer server.Close()

	client := NewClient(socketPath)
	for _, tc := range []struct {
		size int64
		body string
	}{
		{size: 1 << 30, body: `{"desired_balloon":1073741824}`},
		{size: 0, body: `{"desired_balloon":0}`},
	} {
		assert.NoError(t, client.VmResizeBalloon(context.Background(), tc.size))
		assert.Equal(t, "PUT", method)
		assert.Equal(t, "/api/v1/vm.resize", path)
		assert.Equal(t, tc.body, body)
	}
}
